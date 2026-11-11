// Command assetservice 是策略资产服务的进程入口。
//
// 生产装配是 PG Store：启动时先连库并执行 strategy schema 的幂等 DDL，再开始
// 监听。DSN 为空时退到内存 Store（只用于本地演示/测试），日志里明确标出——
// 不把「重启即丢」伪装成持久化。编译端口不在此进程内（D 裁决：cmd/compiler
// 独立进程，经内部端点登记）。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ruoxizhnya/tellurion-kit/auth"
	kithttp "github.com/ruoxizhnya/tellurion-kit/auth/httpadapter"

	"tellurion-strategy/internal/asset"
	"tellurion-strategy/internal/compilerclient"
	"tellurion-strategy/internal/httpapi"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "assetservice: %v\n", err)
		os.Exit(1)
	}
}

type repeatableFlag []string

func (f *repeatableFlag) String() string { return strings.Join(*f, " ") }

func (f *repeatableFlag) Set(v string) error {
	*f = append(*f, v)
	return nil
}

func run() error {
	addr := flag.String("addr", "127.0.0.1:8090", "listen address")
	dsn := flag.String("dsn", os.Getenv("STRATEGY_DATABASE_URL"),
		"PostgreSQL DSN; empty selects the in-memory store (non-persistent)")
	cacheDir := flag.String("cache-dir", defaultCacheDir(),
		"local content-addressed cache directory for artifact bytes; empty disables the disk tier")
	cacheMemory := flag.Int64("cache-memory-bytes", asset.DefaultMemoryCacheBytes, "in-memory artifact cache capacity in bytes")
	cacheDisk := flag.Int64("cache-disk-bytes", asset.DefaultDiskCacheBytes, "on-disk artifact cache capacity in bytes")
	latestTTL := flag.Duration("latest-ttl", asset.DefaultLatestTTL, "TTL for resolve?ref=latest results")

	// 外部编译进程（QT-41c）：-compiler-bin 为空 = disabled，build 端点保持
	// 503 compile_unavailable；非空但必填项缺失 = 启动期 fail-loud。
	compilerBin := flag.String("compiler-bin", "", "path to the cmd/compiler executable; empty disables external compilation")
	compilerAssetURL := flag.String("compiler-asset-url", "", "base URL of this asset service as seen by the compiler process (required with -compiler-bin)")
	compilerName := flag.String("compiler-toolchain-name", "", "external toolchain name, e.g. tinygo (required with -compiler-bin)")
	compilerVersion := flag.String("compiler-toolchain-version", "", "pinned external toolchain version (required with -compiler-bin)")
	compilerPath := flag.String("compiler-toolchain-path", "", "absolute path to the external toolchain binary (required with -compiler-bin)")
	compilerLock := flag.String("compiler-toolchain-lock", "", "path to toolchains.lock (required with -compiler-bin)")
	compilerTimeout := flag.Duration("compiler-timeout", 5*time.Minute, "per-attempt compile timeout for the external compiler")
	compilerProcessTimeout := flag.Duration("compiler-process-timeout", compilerclient.DefaultProcessTimeout,
		"hard upper bound on one cmd/compiler child process (SVC-STRAT-G4); over the bound the job is failed with error_code=compile_timeout")
	compilerAttempts := flag.Int("compiler-max-attempts", 3, "attempts per external compile job (1 disables retries)")
	compilerBackoff := flag.Duration("compiler-retry-backoff", 2*time.Second, "delay between attempts of one external compile job")
	var compilerArgs repeatableFlag
	var compilerEnv repeatableFlag
	flag.Var(&compilerArgs, "compiler-arg", "toolchain argument template passed to cmd/compiler (repeatable)")
	flag.Var(&compilerEnv, "compiler-env", "environment variable passed through to the external toolchain (repeatable)")

	// 鉴权（SVC-STRAT-G1）：只有 /promote 与两条 /internal/* 受保护。无 secret
	// 时退到开放访问——启动闸保证它只可能发生在回环或显式 loopback-published。
	jwtSecret := flag.String("jwt-secret", envFirst("JWT_SECRET", "AUTH_JWT_SECRET"),
		"JWT secret shared with core (empty = open access on loopback only)")
	exposure := flag.String("insecure-exposure", os.Getenv("AUTH_INSECURE_EXPOSURE"),
		"explicit exposure declaration for open access on a non-loopback bind (only loopback-published is accepted)")
	flag.Parse()

	startup := kithttp.DecideStartup(*jwtSecret, true, bindHost(*addr), *exposure)
	if startup.Refuse {
		return errors.New(startup.Reason)
	}
	if startup.Insecure {
		slog.Warn("authentication disabled (open access); bind address must be loopback or loopback-published",
			"addr", *addr)
	}
	tokens := auth.NewTokenService(auth.Config{JWTSecret: startup.Secret})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var (
		store        asset.Store
		storage      string
		closeDB      func()
		readiness    func(context.Context) error
		promotionLog asset.PromotionLog
	)
	if *dsn == "" {
		store = asset.NewMemory()
		storage = "memory (non-persistent)"
		promotionLog = asset.NewMemoryPromotionLog()
		slog.Warn("STRATEGY_DATABASE_URL is empty; using the in-memory store")
	} else {
		pg, err := asset.OpenPG(ctx, *dsn)
		if err != nil {
			return err
		}
		if err := pg.Migrate(ctx); err != nil {
			pg.Close()
			return fmt.Errorf("migrate: %w", err)
		}
		store = pg
		storage = "postgres"
		closeDB = pg.Close
		readiness = pg.Ready
		promotionLog = pg
	}
	if closeDB != nil {
		defer closeDB()
	}

	// 缓存是读路径的优化；构造失败（目录不可写等）在启动期就报，不做静默降级。
	cache, err := asset.NewCache(asset.CacheConfig{
		Dir:         *cacheDir,
		MemoryBytes: *cacheMemory,
		DiskBytes:   *cacheDisk,
		LatestTTL:   *latestTTL,
	})
	if err != nil {
		return err
	}

	compiler, err := newExternalCompiler(externalCompilerConfig{
		Bin:              *compilerBin,
		AssetURL:         *compilerAssetURL,
		ToolchainName:    *compilerName,
		ToolchainVersion: *compilerVersion,
		ToolchainPath:    *compilerPath,
		ToolchainLock:    *compilerLock,
		ToolchainArgs:    compilerArgs,
		EnvAllowlist:     compilerEnv,
		Timeout:          *compilerTimeout,
		ProcessTimeout:   *compilerProcessTimeout,
		MaxAttempts:      *compilerAttempts,
		RetryBackoff:     *compilerBackoff,
	})
	if err != nil {
		return fmt.Errorf("configure compiler: %w", err)
	}

	svc, err := asset.New(asset.Options{Store: store, Compiler: compiler, Cache: cache})
	if err != nil {
		return err
	}
	// G 裁决：内置策略保留 core 的 init 注册，这里在启动时同步登记定义
	// （carrier=builtin、无 artifact），消除「引擎自带一套、服务不知道」的
	// 第二真相源。幂等：已存在只核对形状，不覆盖。
	registered, err := svc.RegisterBuiltins(ctx)
	if err != nil {
		return fmt.Errorf("register builtins: %w", err)
	}
	slog.Info("builtin strategies registered", "count", registered)
	api, err := httpapi.New(svc,
		httpapi.WithAuthorizer(httpapi.NewAuthorizer(tokens)),
		httpapi.WithReadiness(readiness),
		httpapi.WithPromotionLog(promotionLog),
	)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	logMode := "disabled"
	if compiler != nil {
		logMode = "external (cmd/compiler)"
	}
	slog.Info("strategy asset service listening", "addr", ln.Addr().String(),
		"storage", storage, "compiler", logMode, "auth", tokens.Enabled(),
		"drain", shutdownDrainTimeout.String())
	return serve(ln, srv, shutdownDrainTimeout)
}

// shutdownDrainTimeout 是有界 drain 窗口：SIGTERM/SIGINT 之后给在途请求的最长
// 宽限时间。与探针/编排侧 10s 级别的超时同口径；超时不无限等待，Close 兜底后
// 以非零码退出。
const shutdownDrainTimeout = 10 * time.Second

// serve 在已绑定的 listener 上跑 HTTP，并在收到 SIGINT/SIGTERM 时排空在途请求。
func serve(ln net.Listener, srv *http.Server, drain time.Duration) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serveWithContext(ctx, ln, srv, drain)
}

// serveWithContext 是 serve 的可测内核：ctx 取消等价于收到终止信号。
// 正常收到信号并排空完成返回 nil（退出码 0）；drain 超时则 Close 后返回错误
// （非零退出），不把「掐断在途请求」伪装成正常退出。
func serveWithContext(ctx context.Context, ln net.Listener, srv *http.Server, drain time.Duration) error {
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	slog.Info("termination signal received; draining in-flight requests", "drain", drain.String())
	drainCtx, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()
	if err := srv.Shutdown(drainCtx); err != nil {
		_ = srv.Close()
		return fmt.Errorf("drain exceeded %s: %w", drain, err)
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// bindHost 从 -addr 取绑定主机；取不到端口时原样当主机比（空 = 全网卡）。
func bindHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

// envFirst 按顺序取第一个非空环境变量（core 历史用法 JWT_SECRET、配置口径 AUTH_JWT_SECRET）。
func envFirst(names ...string) string {
	for _, name := range names {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

// defaultCacheDir 给磁盘二级缓存一个跨重启稳定的位置；拿不到用户缓存目录时
// 退回空串（只开内存一级），而不是写当前工作目录。
func defaultCacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "tellurion-strategy", "artifacts")
}
