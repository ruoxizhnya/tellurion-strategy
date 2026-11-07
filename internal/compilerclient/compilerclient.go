// Package compilerclient 是资产服务侧的编译端口实现：把一次 build 作业派发给
// 独立的 cmd/compiler 进程，由对方经内部端点回写 running / 终态。
//
// 它实现 asset.Compiler（ToolchainID）与 asset.BuildDispatcher（DispatchBuild）；
// Compile 不会被调用（派发模式没有进程内编译），调到时返回明确错误。
//
// 依赖方向：cmd/assetservice → internal/compilerclient → internal/asset；
// compilerclient 不 import internal/compile，也不直连 DB。
package compilerclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"tellurion-strategy/internal/asset"
)

// ErrProcessStart 是编译进程没能启动（二进制不存在 / 无法 fork）。
var ErrProcessStart = errors.New("compilerclient: failed to start compiler process")

// ErrProcessTimeout 是 cmd/compiler 子进程超过了硬上界（见 DefaultProcessTimeout）。
//
// 它是**进程级**超时：子进程被 CommandContext 杀掉后，DispatchBuild 的等待方
// 据此把作业收口成 failed + compile_timeout，而不是留在 running。
var ErrProcessTimeout = errors.New("compilerclient: compiler process exceeded its deadline")

// ErrCompileNotDispatched 说明 Compile 不该被调用：本实现只做跨进程派发。
var ErrCompileNotDispatched = errors.New("compilerclient: Compile is not available; builds are dispatched to cmd/compiler")

const (
	// DefaultProcessTimeout 是 cmd/compiler 子进程的默认硬上界。
	//
	// 批次预算 maxAttempts×(timeout+backoff)+1min（默认 3×(5m+2s)+1m≈16m）低于
	// 它时以批次预算为准；高于它时一律在此截断——子进程不能因为配置了多尝试 /
	// 长超时而无限存活。core 侧 AUD-56 的同型坑：没有 CommandContext 的
	// `go build` 挂 601s 不返回，进程级上界是最后一道闸。
	DefaultProcessTimeout = 20 * time.Minute

	// MaxProcessTimeout 是允许配置的进程上界最大值（超过它等于没有上界）。
	MaxProcessTimeout = time.Hour
)

// ProcessRunner 启动编译器进程并返回等待函数。
//
// 返回 err 表示**启动失败**（映射 ErrCompileUnavailable，build 端点 503）；
// 返回的 wait 在进程退出后给出结果，非 nil 表示非零退出。
// 单独拆出 wait 是为了让 DispatchBuild 能立刻返回 202、同时把等待放到 goroutine。
type ProcessRunner func(ctx context.Context, bin string, args []string, env []string) (wait func() error, err error)

// Config 是 compilerclient 的装配面。
type Config struct {
	Bin              string
	AssetURL         string
	ToolchainName    string
	ToolchainVersion string
	ToolchainPath    string
	ToolchainLock    string
	ToolchainArgs    []string
	EnvAllowlist     []string
	MemoryLimitBytes int64
	Timeout          time.Duration
	// ProcessTimeout 是 cmd/compiler 子进程的硬上界（0 = DefaultProcessTimeout）。
	// 取值必须落在 (0, MaxProcessTimeout]；实际生效值取它与批次预算的较小者。
	ProcessTimeout time.Duration
	MaxAttempts    int
	RetryBackoff   time.Duration
	HTTPClient     *http.Client
	Runner         ProcessRunner
}

// Compiler 实现 asset.Compiler + asset.BuildDispatcher。
type Compiler struct {
	cfg Config
}

// New 校验必填项并填默认值；缺项在启动期 fail-loud，不留到第一次 build。
func New(cfg Config) (*Compiler, error) {
	for _, required := range []struct {
		name  string
		value string
	}{
		{"compiler-bin", cfg.Bin},
		{"compiler-asset-url", cfg.AssetURL},
		{"compiler-toolchain-name", cfg.ToolchainName},
		{"compiler-toolchain-version", cfg.ToolchainVersion},
		{"compiler-toolchain-path", cfg.ToolchainPath},
		{"compiler-toolchain-lock", cfg.ToolchainLock},
	} {
		if strings.TrimSpace(required.value) == "" {
			return nil, fmt.Errorf("%w: -%s is required", asset.ErrInvalidParameter, required.name)
		}
	}
	if cfg.MemoryLimitBytes == 0 {
		cfg.MemoryLimitBytes = asset.MaxMemoryLimitBytes
	}
	if cfg.MemoryLimitBytes < 0 || cfg.MemoryLimitBytes > asset.MaxMemoryLimitBytes {
		return nil, fmt.Errorf("%w: memory limit %d out of (0, %d]",
			asset.ErrInvalidParameter, cfg.MemoryLimitBytes, asset.MaxMemoryLimitBytes)
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Minute
	}
	if cfg.ProcessTimeout == 0 {
		cfg.ProcessTimeout = DefaultProcessTimeout
	}
	if cfg.ProcessTimeout < 0 || cfg.ProcessTimeout > MaxProcessTimeout {
		return nil, fmt.Errorf("%w: process timeout %s out of (0, %s]",
			asset.ErrInvalidParameter, cfg.ProcessTimeout, MaxProcessTimeout)
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.RetryBackoff < 0 {
		return nil, fmt.Errorf("%w: retry backoff must not be negative", asset.ErrInvalidParameter)
	}
	if cfg.Runner == nil {
		cfg.Runner = execRunner
	}
	return &Compiler{cfg: cfg}, nil
}

// ToolchainID 是作业与 artifact 行的工具链标识，与 cmd/compiler 的
// `-toolchain-name/-toolchain-version` 同一套 `<name> <version>`。
func (c *Compiler) ToolchainID() string {
	return c.cfg.ToolchainName + " " + c.cfg.ToolchainVersion
}

// Compile 永不成功：派发模式没有进程内编译。
func (c *Compiler) Compile(context.Context, asset.CompileRequest) (asset.CompileResult, error) {
	return asset.CompileResult{}, ErrCompileNotDispatched
}

// DispatchBuild 把作业写成临时 job JSON，启动 cmd/compiler，并立刻返回。
//
// 进程在 goroutine 里等待：非零退出时先查服务侧作业是否已终态，未终态才补报
// failed（服务端的终态幂等/不可回退保证重复上报不会改判）。
func (c *Compiler) DispatchBuild(_ context.Context, req asset.DispatchRequest) error {
	job := jobFile{
		JobID:       req.JobID,
		StrategyID:  req.StrategyID,
		VersionHash: req.VersionHash,
		Carrier:     string(req.Carrier),
		ABIVersion:  req.ABIVersion,
		Entry:       req.Entry,
		Source:      req.Source,
		BuiltBy:     "compilerclient " + c.ToolchainID(),
	}
	raw, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("%w: encode job: %v", asset.ErrCompileUnavailable, err)
	}
	tmp, err := os.CreateTemp("", "strategy-build-*.json")
	if err != nil {
		return fmt.Errorf("%w: create job file: %v", asset.ErrCompileUnavailable, err)
	}
	path := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(path)
		return fmt.Errorf("%w: write job file: %v", asset.ErrCompileUnavailable, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("%w: close job file: %v", asset.ErrCompileUnavailable, err)
	}

	// 请求 ctx 随 build handler 返回而取消，不能拿它管进程生命周期。
	// 子进程存活时间 = min(批次预算, ProcessTimeout)：前者按重试配置算，
	// 后者是**绝对上界**，防止「多尝试 × 长超时」把子进程留在 running 一侧。
	deadline := c.processDeadline()
	runCtx, cancel := context.WithTimeout(context.Background(), deadline)
	wait, err := c.cfg.Runner(runCtx, c.cfg.Bin, c.args(path), os.Environ())
	if err != nil {
		cancel()
		_ = os.Remove(path)
		return fmt.Errorf("%w: %v", asset.ErrCompileUnavailable, err)
	}
	go func() {
		defer cancel()
		defer func() { _ = os.Remove(path) }()
		waitErr := wait()
		if waitErr == nil {
			return
		}
		cause := waitErr
		// 到上界被 CommandContext 杀掉：归因成进程超时，回写 failed +
		// compile_timeout。没有这一步，job 会停在 running（AUD-56 的同型坑）。
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			cause = fmt.Errorf("%w after %s: %v", ErrProcessTimeout, deadline, waitErr)
		}
		c.backfillFailed(req.StrategyID, req.JobID, cause)
	}()
	return nil
}

func (c *Compiler) budget() time.Duration {
	// 单作业：maxAttempts × (timeout + backoff) + 1min（与 cmd/compiler 的批次预算同口径）。
	return time.Duration(c.cfg.MaxAttempts)*(c.cfg.Timeout+c.cfg.RetryBackoff) + time.Minute
}

// processDeadline 是实际生效的子进程上界：批次预算与配置上界的较小者。
func (c *Compiler) processDeadline() time.Duration {
	deadline := c.budget()
	if c.cfg.ProcessTimeout > 0 && c.cfg.ProcessTimeout < deadline {
		deadline = c.cfg.ProcessTimeout
	}
	return deadline
}

func (c *Compiler) args(jobPath string) []string {
	args := []string{
		"-job", jobPath,
		"-asset-url", c.cfg.AssetURL,
		"-toolchain-name", c.cfg.ToolchainName,
		"-toolchain-version", c.cfg.ToolchainVersion,
		"-toolchain-path", c.cfg.ToolchainPath,
		"-toolchain-lock", c.cfg.ToolchainLock,
		"-memory-limit", strconv.FormatInt(c.cfg.MemoryLimitBytes, 10),
		"-timeout", c.cfg.Timeout.String(),
		"-workers", "1",
		"-max-attempts", strconv.Itoa(c.cfg.MaxAttempts),
		"-retry-backoff", c.cfg.RetryBackoff.String(),
	}
	for _, arg := range c.cfg.ToolchainArgs {
		args = append(args, "-arg", arg)
	}
	for _, env := range c.cfg.EnvAllowlist {
		args = append(args, "-env", env)
	}
	return args
}

// backfillFailed 在进程非零退出且服务侧作业仍未终态时补报 failed。
//
// 「先查再报」不是必需（服务端会拒绝终态回退），但它避免把一次已经成功的
// 作业无谓地打到 409，日志里也更容易判断。
func (c *Compiler) backfillFailed(strategyID, jobID string, cause error) {
	if strings.TrimSpace(jobID) == "" {
		return
	}
	state, err := c.jobState(strategyID, jobID)
	if err != nil {
		slog.Warn("compilerclient: read build job state", "job_id", jobID, "error", err)
	}
	if state == asset.JobSucceeded || state == asset.JobFailed {
		return
	}
	code := asset.ErrorCodeCompileFailed
	if errors.Is(cause, ErrProcessTimeout) {
		code = asset.ErrorCodeCompileTimeout
	}
	report := asset.BuildJobReport{
		State:     asset.JobFailed,
		ErrorCode: code,
		Error:     cause.Error(),
	}
	if err := c.postReport(strategyID, jobID, report); err != nil {
		slog.Warn("compilerclient: backfill failed state", "job_id", jobID, "error", err)
	}
}

func (c *Compiler) jobState(strategyID, jobID string) (string, error) {
	endpoint := strings.TrimRight(c.cfg.AssetURL, "/") + "/api/strategies/" +
		url.PathEscape(strategyID) + "/builds/" + url.PathEscape(jobID)
	client := c.cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Get(endpoint)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var body struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	return body.State, nil
}

func (c *Compiler) postReport(strategyID, jobID string, report asset.BuildJobReport) error {
	payload, err := json.Marshal(report)
	if err != nil {
		return err
	}
	endpoint := strings.TrimRight(c.cfg.AssetURL, "/") + "/internal/strategies/" +
		url.PathEscape(strategyID) + "/builds/" + url.PathEscape(jobID)
	client := c.cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Post(endpoint, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// execRunner 是默认 ProcessRunner：Start 失败即启动失败，Wait 放到调用方。
//
// 必须走 exec.CommandContext：没有它的 `go build` 能在联网解析时挂 601s
// （core 侧 AUD-56 的实测）。ctx 到点时 CommandContext 杀掉子进程，Wait
// 返回错误，DispatchBuild 据此把作业收口成 failed + error_code。
func execRunner(ctx context.Context, bin string, args []string, env []string) (func() error, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return func() error {
		if err := cmd.Wait(); err != nil {
			return fmt.Errorf("compiler process: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil
	}, nil
}

// jobFile 是写给 cmd/compiler 的 job JSON（字段名与 compilerjob.Job 一致，
// DisallowUnknownFields 下多一个字段就会整批拒）。
type jobFile struct {
	JobID       string `json:"job_id,omitempty"`
	StrategyID  string `json:"strategy_id"`
	VersionHash string `json:"version_hash"`
	Carrier     string `json:"carrier"`
	ABIVersion  string `json:"abi_version"`
	Entry       string `json:"entry"`
	Source      string `json:"source"`
	BuiltBy     string `json:"built_by"`
}
