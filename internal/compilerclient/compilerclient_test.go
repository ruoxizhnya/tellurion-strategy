package compilerclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tellurion-strategy/internal/asset"
)

// fakeProcess 记录编译进程的启动参数与环境，并在启动时读取 job 文件内容
// （派发成功后临时文件会被清理，断言必须在启动时抓取）。
type fakeProcess struct {
	bin     string
	args    []string
	env     []string
	jobJSON map[string]any
	startAt int

	startErr error
	waitErr  error
}

func (f *fakeProcess) run(_ context.Context, bin string, args []string, env []string) (func() error, error) {
	f.startAt++
	f.bin, f.args, f.env = bin, append([]string(nil), args...), append([]string(nil), env...)
	for i := 0; i < len(args); i++ {
		if args[i] == "-job" && i+1 < len(args) {
			raw, err := os.ReadFile(args[i+1])
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(raw, &f.jobJSON); err != nil {
				return nil, err
			}
			i++
		}
	}
	if f.startErr != nil {
		return nil, f.startErr
	}
	return func() error { return f.waitErr }, nil
}

func validConfig(srv *httptest.Server, runner ProcessRunner) Config {
	return Config{
		Bin:              "C:/tools/compiler.exe",
		AssetURL:         srv.URL,
		ToolchainName:    "tinygo",
		ToolchainVersion: "0.34.0",
		ToolchainPath:    "C:/tools/tinygo.exe",
		ToolchainLock:    "C:/tools/toolchains.lock",
		ToolchainArgs:    []string{"-target=wasm-unknown", "{out}", "{src}"},
		EnvAllowlist:     []string{"PATH", "TINYGOROOT"},
		MemoryLimitBytes: 4 << 20,
		Timeout:          30 * time.Second,
		MaxAttempts:      2,
		RetryBackoff:     time.Second,
		Runner:           runner,
	}
}

func TestNew_ValidatesRequiredFields(t *testing.T) {
	if _, err := New(Config{Bin: "compiler"}); !errors.Is(err, asset.ErrInvalidParameter) {
		t.Fatalf("err = %v, want ErrInvalidParameter", err)
	}
	cfg := Config{
		Bin: "compiler", AssetURL: "http://127.0.0.1:1", ToolchainName: "tinygo",
		ToolchainVersion: "0.34", ToolchainPath: "tinygo", ToolchainLock: "lock",
	}
	compiler, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if compiler.ToolchainID() != "tinygo 0.34" {
		t.Fatalf("ToolchainID = %q", compiler.ToolchainID())
	}
	if _, err := compiler.Compile(context.Background(), asset.CompileRequest{}); !errors.Is(err, ErrCompileNotDispatched) {
		t.Fatalf("Compile err = %v, want ErrCompileNotDispatched", err)
	}
	if _, err := New(Config{
		Bin: "compiler", AssetURL: "http://127.0.0.1:1", ToolchainName: "tinygo",
		ToolchainVersion: "0.34", ToolchainPath: "tinygo", ToolchainLock: "lock",
		RetryBackoff: -time.Second,
	}); !errors.Is(err, asset.ErrInvalidParameter) {
		t.Fatalf("negative backoff err = %v, want ErrInvalidParameter", err)
	}
}

func TestDispatchBuild_WritesJobFileAndPassesArgs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	proc := &fakeProcess{}
	compiler, err := New(validConfig(srv, proc.run))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := asset.DispatchRequest{
		JobID: "job_42",
		CompileRequest: asset.CompileRequest{
			StrategyID: "st_1", VersionHash: strings.Repeat("a", 64),
			Carrier: asset.CarrierWASM, ABIVersion: "on_bar/2", Entry: "OnBar",
			Source: "package strategy; func OnBar() {}",
		},
	}
	if err := compiler.DispatchBuild(context.Background(), req); err != nil {
		t.Fatalf("DispatchBuild: %v", err)
	}
	if proc.startAt != 1 {
		t.Fatalf("process starts = %d, want 1", proc.startAt)
	}
	if proc.bin != "C:/tools/compiler.exe" {
		t.Fatalf("bin = %q", proc.bin)
	}

	// job 文件：job_id + 源码 + 版本，全部来自 DispatchRequest。
	if proc.jobJSON["job_id"] != "job_42" ||
		proc.jobJSON["strategy_id"] != "st_1" ||
		proc.jobJSON["source"] != "package strategy; func OnBar() {}" ||
		proc.jobJSON["version_hash"] != strings.Repeat("a", 64) {
		t.Fatalf("job json = %+v", proc.jobJSON)
	}

	// 参数：派发所需的全套工具链 / 预算 / 透传。
	want := map[string]string{
		"-asset-url":         srv.URL,
		"-toolchain-name":    "tinygo",
		"-toolchain-version": "0.34.0",
		"-toolchain-path":    "C:/tools/tinygo.exe",
		"-toolchain-lock":    "C:/tools/toolchains.lock",
		"-memory-limit":      "4194304",
		"-timeout":           "30s",
		"-workers":           "1",
		"-max-attempts":      "2",
		"-retry-backoff":     "1s",
	}
	pairs := map[string]string{}
	for i := 0; i < len(proc.args); i++ {
		if strings.HasPrefix(proc.args[i], "-") {
			if i+1 < len(proc.args) && !strings.HasPrefix(proc.args[i+1], "-") {
				pairs[proc.args[i]] = proc.args[i+1]
				i++
			}
		}
	}
	for flag, value := range want {
		if pairs[flag] != value {
			t.Fatalf("arg %s = %q, want %q (args: %v)", flag, pairs[flag], value, proc.args)
		}
	}
	// repeatable -arg / -env 原样透传。
	if !containsPair(proc.args, "-arg", "-target=wasm-unknown") ||
		!containsPair(proc.args, "-arg", "{src}") ||
		!containsPair(proc.args, "-env", "TINYGOROOT") {
		t.Fatalf("repeatable args not passed through: %v", proc.args)
	}
	if _, ok := pairs["-job"]; !ok {
		t.Fatalf("missing -job in args: %v", proc.args)
	}
}

func containsPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func TestDispatchBuild_StartFailureMapsCompileUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	proc := &fakeProcess{startErr: errors.New("exec: file not found")}
	compiler, err := New(validConfig(srv, proc.run))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = compiler.DispatchBuild(context.Background(), asset.DispatchRequest{JobID: "job_1"})
	if !errors.Is(err, asset.ErrCompileUnavailable) {
		t.Fatalf("err = %v, want ErrCompileUnavailable", err)
	}
}

// 进程非零退出且服务侧仍未终态 → 补报 failed（httptest 内部端点）。
func TestDispatchBuild_NonZeroExitBackfillsFailed(t *testing.T) {
	var mu sync.Mutex
	var reports []asset.BuildJobReport
	posted := make(chan asset.BuildJobReport, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/strategies/"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"state":"running"}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/internal/strategies/"):
			var report asset.BuildJobReport
			if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			reports = append(reports, report)
			mu.Unlock()
			posted <- report
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"state":"failed"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	proc := &fakeProcess{waitErr: errors.New("compiler process: exit status 2: boom")}
	compiler, err := New(validConfig(srv, proc.run))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := asset.DispatchRequest{
		JobID: "job_backfill",
		CompileRequest: asset.CompileRequest{
			StrategyID: "st_1", VersionHash: strings.Repeat("a", 64),
			Carrier: asset.CarrierWASM, ABIVersion: "on_bar/2", Entry: "OnBar", Source: "src",
		},
	}
	if err := compiler.DispatchBuild(context.Background(), req); err != nil {
		t.Fatalf("DispatchBuild: %v", err)
	}

	select {
	case report := <-posted:
		if report.State != asset.JobFailed || report.ErrorCode != asset.ErrorCodeCompileFailed {
			t.Fatalf("backfill report = %+v, want failed/compile_failed", report)
		}
		if !strings.Contains(report.Error, "boom") {
			t.Fatalf("backfill error = %q, want the process failure", report.Error)
		}
	case <-time.After(3 * time.Second):
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("no backfill report was posted (reports: %+v)", reports)
	}
}

// 服务侧已终态时不补报（避免把已成功作业打到 409）。
func TestDispatchBuild_SkipsBackfillWhenJobAlreadyTerminal(t *testing.T) {
	var posts int32
	gotGet := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			select {
			case gotGet <- struct{}{}:
			default:
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"state":"succeeded"}`))
			return
		}
		atomic.AddInt32(&posts, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	proc := &fakeProcess{waitErr: errors.New("exit status 1")}
	compiler, err := New(validConfig(srv, proc.run))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := asset.DispatchRequest{JobID: "job_done"}
	if err := compiler.DispatchBuild(context.Background(), req); err != nil {
		t.Fatalf("DispatchBuild: %v", err)
	}

	select {
	case <-gotGet:
	case <-time.After(3 * time.Second):
		t.Fatal("backfill never read the job state")
	}
	if n := atomic.LoadInt32(&posts); n != 0 {
		t.Fatalf("posted %d failed reports for an already-succeeded job", n)
	}
}
