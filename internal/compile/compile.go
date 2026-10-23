// Package compile 是 QT-41b 的编译执行器：固定 toolchain + env 白名单 + 体积上限。
//
// 本包只负责「把源码变成字节」，不落库、不 promote；产物的登记走
// asset.RegisterArtifact（由 cmd/compiler 调内部端点完成）。
package compile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"tellurion-strategy/internal/asset"
)

// Target 是唯一允许的编译目标（D 裁决：产物 -target=wasm-unknown）。
const Target = "wasm-unknown"

// maxLogBytes 是编译日志上限：日志是给人看的，不能反过来把磁盘 / 内存打满。
const maxLogBytes = 64 << 10

// Config 是固定 toolchain 的描述。
//
// 纪律：Name/Version 进 toolchain 标识（artifact 行按它区分产物）；Path 是绝对
// 路径（不靠 PATH 查找，避免「机器上换了版本但服务不知道」）；Args 必须含
// `-target=wasm-unknown` 且含 {src} / {out} 两个占位符。
type Config struct {
	Name         string
	Version      string
	Path         string
	Args         []string
	EnvAllowlist []string
	Timeout      time.Duration
	MaxBytes     int64
}

// ToolchainID 是写进 artifact 行的稳定标识。
func (c Config) ToolchainID() string { return c.Name + " " + c.Version }

// Validate 在构造期 fail-closed（服务启动即发现配置错误，而不是第一次编译时）。
func (c Config) Validate() error {
	if strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.Version) == "" {
		return errors.New("compile: toolchain name and version are required")
	}
	if strings.TrimSpace(c.Path) == "" {
		return errors.New("compile: toolchain path is required")
	}
	joined := strings.Join(c.Args, " ")
	if !hasPinnedTarget(c.Args) {
		return fmt.Errorf("compile: toolchain args must pin -target=%s", Target)
	}
	if !strings.Contains(joined, "{src}") || !strings.Contains(joined, "{out}") {
		return errors.New("compile: toolchain args must contain {src} and {out} placeholders")
	}
	// TinyGo 默认把源文件的绝对路径写进 DWARF：Runner 每次在随机临时目录里落
	// src，同源码两次编译就会得到不同字节、不同 artifact_hash。`-no-debug`
	// 是唯一让 tinygo 产物与路径无关的开关（它没有 -trimpath）——缺了它，
	// 「同源码同工具链可复现」这条验收直接不成立。
	if c.Name == "tinygo" && !hasArg(c.Args, "-no-debug") {
		return errors.New("compile: tinygo args must include -no-debug (debug info embeds the source path and breaks artifact reproducibility)")
	}
	if c.MaxBytes < 0 {
		return errors.New("compile: max bytes must be non-negative")
	}
	return nil
}

// hasArg 判断参数里是否有**恰好等于** name 的一项（不是子串匹配）。
func hasArg(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

// hasPinnedTarget 要求参数里存在**恰好等于** `-target=wasm-unknown` 的一项，
// 而不是「包含该子串」：`-target=wasm-unknown-extra` 会被子串匹配放过，
// 那样「锁定目标」这句话就不成立了。
func hasPinnedTarget(args []string) bool {
	pinned := "-target=" + Target
	for _, a := range args {
		if a == pinned {
			return true
		}
	}
	return false
}

// Result 是一次编译的产出。
type Result struct {
	Bytes []byte
	Log   string
}

// Runner 是编译执行端口。生产实现是 ExecRunner；测试可注入 fake。
type Runner struct {
	cfg Config
}

// NewRunner 校验配置并返回执行器。
func NewRunner(cfg Config) (*Runner, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Minute
	}
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = asset.MaxArtifactBytes
	}
	return &Runner{cfg: cfg}, nil
}

// ToolchainID 返回配置的 toolchain 标识。
func (r *Runner) ToolchainID() string { return r.cfg.ToolchainID() }

// Compile 把源码写进临时目录、执行固定 toolchain、读回产物。
//
// 子进程环境只保留 EnvAllowlist 里的变量：把服务进程的整个环境（含 DB 连接串、
// token）交给一个编译不可信源码的工具链，是最容易被忽略的越权通道。
func (r *Runner) Compile(ctx context.Context, req asset.CompileRequest) (asset.CompileResult, error) {
	if strings.TrimSpace(req.Source) == "" {
		return asset.CompileResult{}, asset.ErrSourceMissing
	}
	if int64(len(req.Source)) > asset.MaxSourceBytes {
		return asset.CompileResult{}, fmt.Errorf("%w: %d bytes", asset.ErrSourceTooLarge, len(req.Source))
	}

	dir, err := os.MkdirTemp("", "tellurion-compile-*")
	if err != nil {
		return asset.CompileResult{}, fmt.Errorf("compile: temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(req.Source), 0o600); err != nil {
		return asset.CompileResult{}, fmt.Errorf("compile: write source: %w", err)
	}
	outPath := filepath.Join(dir, "artifact.wasm")

	args := make([]string, len(r.cfg.Args))
	for i, a := range r.cfg.Args {
		a = strings.ReplaceAll(a, "{src}", srcPath)
		a = strings.ReplaceAll(a, "{out}", outPath)
		args[i] = a
	}

	runCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, r.cfg.Path, args...)
	cmd.Dir = dir
	cmd.Env = r.filteredEnv()
	var logBuf bytes.Buffer
	cmd.Stdout = &limitedWriter{buf: &logBuf, limit: maxLogBytes}
	cmd.Stderr = &limitedWriter{buf: &logBuf, limit: maxLogBytes}

	runErr := cmd.Run()
	if runErr != nil {
		log := strings.TrimSpace(logBuf.String())
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return asset.CompileResult{}, fmt.Errorf("%w: toolchain %s timed out after %s; log: %s",
				asset.ErrCompileFailed, r.cfg.ToolchainID(), r.cfg.Timeout, log)
		}
		return asset.CompileResult{}, fmt.Errorf("%w: toolchain %s: %v; log: %s",
			asset.ErrCompileFailed, r.cfg.ToolchainID(), runErr, log)
	}

	info, err := os.Stat(outPath)
	if err != nil {
		return asset.CompileResult{}, fmt.Errorf("%w: artifact was not produced at {out}: %v",
			asset.ErrCompileFailed, err)
	}
	if info.Size() > r.cfg.MaxBytes {
		return asset.CompileResult{}, fmt.Errorf("%w: %d bytes > %d",
			asset.ErrArtifactTooLarge, info.Size(), r.cfg.MaxBytes)
	}
	bytesOut, err := os.ReadFile(outPath)
	if err != nil {
		return asset.CompileResult{}, fmt.Errorf("%w: read artifact: %v", asset.ErrCompileFailed, err)
	}
	return asset.CompileResult{Bytes: bytesOut, Log: strings.TrimSpace(logBuf.String())}, nil
}

// filteredEnv 只保留白名单里的变量（空白名单 = 空环境）。
func (r *Runner) filteredEnv() []string {
	var env []string
	for _, name := range r.cfg.EnvAllowlist {
		if name == "" {
			continue
		}
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}

// limitedWriter 截断超限输出（写满后继续接受但丢弃，避免管道堵塞子进程）。
type limitedWriter struct {
	buf   *bytes.Buffer
	limit int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.buf.Len() >= w.limit {
		return len(p), nil
	}
	remain := w.limit - w.buf.Len()
	if len(p) > remain {
		_, _ = w.buf.Write(p[:remain])
		return len(p), nil
	}
	return w.buf.Write(p)
}
