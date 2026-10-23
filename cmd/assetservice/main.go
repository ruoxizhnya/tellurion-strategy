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
	"net/http"
	"os"
	"path/filepath"
	"time"

	"tellurion-strategy/internal/asset"
	"tellurion-strategy/internal/httpapi"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "assetservice: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	addr := flag.String("addr", "127.0.0.1:8085", "listen address")
	dsn := flag.String("dsn", os.Getenv("STRATEGY_DATABASE_URL"),
		"PostgreSQL DSN; empty selects the in-memory store (non-persistent)")
	cacheDir := flag.String("cache-dir", defaultCacheDir(),
		"local content-addressed cache directory for artifact bytes; empty disables the disk tier")
	cacheMemory := flag.Int64("cache-memory-bytes", asset.DefaultMemoryCacheBytes, "in-memory artifact cache capacity in bytes")
	cacheDisk := flag.Int64("cache-disk-bytes", asset.DefaultDiskCacheBytes, "on-disk artifact cache capacity in bytes")
	latestTTL := flag.Duration("latest-ttl", asset.DefaultLatestTTL, "TTL for resolve?ref=latest results")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var (
		store   asset.Store
		storage string
		closeDB func()
	)
	if *dsn == "" {
		store = asset.NewMemory()
		storage = "memory (non-persistent)"
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

	svc, err := asset.New(asset.Options{Store: store, Cache: cache})
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
	api, err := httpapi.New(svc)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	slog.Info("strategy asset service listening", "addr", *addr,
		"storage", storage, "compiler", "external (cmd/compiler)")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
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
