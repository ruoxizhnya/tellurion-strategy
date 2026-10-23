package compile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"tellurion-strategy/internal/asset"
)

// fakeToolchain 是 TestMain 编出来的假工具链路径（testdata/faketc）。
var fakeToolchain string

// TestMain 构建假工具链。它不是 Skip：工具链是 Compile 的被测对象本身，
// 少了它整包都测不了，构建失败必须判红。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "faketc-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "faketc: temp dir: %v\n", err)
		os.Exit(1)
	}
	name := "faketc"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	fakeToolchain = filepath.Join(dir, name)
	build := exec.Command("go", "build", "-o", fakeToolchain, "./testdata/faketc")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		_ = os.RemoveAll(dir)
		fmt.Fprintf(os.Stderr, "faketc: build failed: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func testConfig(allow ...string) Config {
	return Config{
		Name:         "faketc",
		Version:      "0.1.0-test",
		Path:         fakeToolchain,
		Args:         []string{"-target=" + Target, "-o", "{out}", "{src}"},
		EnvAllowlist: append([]string{"SystemRoot"}, allow...),
		Timeout:      5 * time.Second,
	}
}

func newRunner(t *testing.T, cfg Config) *Runner {
	t.Helper()
	runner, err := NewRunner(cfg)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	return runner
}

var _ asset.Compiler = (*Runner)(nil)

func TestConfig_Validate(t *testing.T) {
	base := testConfig("FAKETC_MODE")
	base.Path = ""

	tests := []struct {
		name   string
		mutate func(*Config)
		wantOK bool
	}{
		{"valid", func(c *Config) { c.Path = fakeToolchain }, true},
		{"missing name", func(c *Config) { c.Name = " " }, false},
		{"missing version", func(c *Config) { c.Version = "" }, false},
		{"missing path", func(c *Config) {}, false},
		{"no pinned target", func(c *Config) { c.Args = []string{"-o", "{out}", "{src}"} }, false},
		{"wrong target", func(c *Config) { c.Args = []string{"-target=wasm32", "-o", "{out}", "{src}"} }, false},
		{"near-miss target", func(c *Config) {
			c.Args = []string{"-target=wasm-unknown-extra", "-o", "{out}", "{src}"}
		}, false},
		{"no src placeholder", func(c *Config) { c.Args = []string{"-target=" + Target, "-o", "{out}"} }, false},
		{"no out placeholder", func(c *Config) { c.Args = []string{"-target=" + Target, "{src}"} }, false},
		{"negative max bytes", func(c *Config) { c.MaxBytes = -1 }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			cfg.Args = append([]string(nil), base.Args...)
			tc.mutate(&cfg)
			err := cfg.Validate()
			if tc.wantOK && err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if !tc.wantOK && err == nil {
				t.Fatalf("Validate accepted an invalid config")
			}
		})
	}
}

func TestNewRunner_DefaultsAndID(t *testing.T) {
	runner := newRunner(t, testConfig())
	if got, want := runner.ToolchainID(), "faketc 0.1.0-test"; got != want {
		t.Fatalf("ToolchainID = %q, want %q", got, want)
	}
	if runner.cfg.Timeout <= 0 || runner.cfg.MaxBytes != asset.MaxArtifactBytes {
		t.Fatalf("defaults not applied: timeout=%s max=%d", runner.cfg.Timeout, runner.cfg.MaxBytes)
	}
}

func TestCompile_WritesSourceAndReadsArtifact(t *testing.T) {
	runner := newRunner(t, testConfig("FAKETC_MODE"))

	res, err := runner.Compile(context.Background(), asset.CompileRequest{
		Source: "package strategy\nfunc OnBar() {}\n",
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got, want := string(res.Bytes), "artifact:package strategy\nfunc OnBar() {}\n"; got != want {
		t.Fatalf("artifact = %q, want %q", got, want)
	}
	if res.Log != "" {
		t.Fatalf("log = %q, want empty", res.Log)
	}
}

func TestCompile_RejectsMissingAndOversizeSourceBeforeRunning(t *testing.T) {
	runner := newRunner(t, testConfig("FAKETC_MODE"))

	if _, err := runner.Compile(context.Background(), asset.CompileRequest{Source: "  "}); !errors.Is(err, asset.ErrSourceMissing) {
		t.Fatalf("empty source err = %v, want ErrSourceMissing", err)
	}
	huge := strings.Repeat("x", int(asset.MaxSourceBytes)+1)
	if _, err := runner.Compile(context.Background(), asset.CompileRequest{Source: huge}); !errors.Is(err, asset.ErrSourceTooLarge) {
		t.Fatalf("oversize source err = %v, want ErrSourceTooLarge", err)
	}
}

func TestCompile_FailureCarriesLog(t *testing.T) {
	runner := newRunner(t, testConfig("FAKETC_MODE"))
	t.Setenv("FAKETC_MODE", "fail")

	_, err := runner.Compile(context.Background(), asset.CompileRequest{Source: "package strategy"})
	if !errors.Is(err, asset.ErrCompileFailed) {
		t.Fatalf("err = %v, want ErrCompileFailed", err)
	}
	if !strings.Contains(err.Error(), "deliberate failure") {
		t.Fatalf("err %q must carry the toolchain log", err)
	}
}

func TestCompile_Timeout(t *testing.T) {
	cfg := testConfig("FAKETC_MODE")
	cfg.Timeout = 150 * time.Millisecond
	runner := newRunner(t, cfg)
	t.Setenv("FAKETC_MODE", "sleep")

	_, err := runner.Compile(context.Background(), asset.CompileRequest{Source: "package strategy"})
	if !errors.Is(err, asset.ErrCompileFailed) {
		t.Fatalf("err = %v, want ErrCompileFailed", err)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err %q must mention the timeout", err)
	}
}

func TestCompile_MissingOutput(t *testing.T) {
	runner := newRunner(t, testConfig("FAKETC_MODE"))
	t.Setenv("FAKETC_MODE", "noout")

	if _, err := runner.Compile(context.Background(), asset.CompileRequest{Source: "package strategy"}); !errors.Is(err, asset.ErrCompileFailed) {
		t.Fatalf("err = %v, want ErrCompileFailed", err)
	}
}

func TestCompile_OversizeArtifact(t *testing.T) {
	cfg := testConfig("FAKETC_SIZE")
	cfg.MaxBytes = 16
	runner := newRunner(t, cfg)
	t.Setenv("FAKETC_SIZE", "64")

	if _, err := runner.Compile(context.Background(), asset.CompileRequest{Source: "package strategy"}); !errors.Is(err, asset.ErrArtifactTooLarge) {
		t.Fatalf("err = %v, want ErrArtifactTooLarge", err)
	}
}

// TestCompile_EnvAllowlistIsEnforced 是安全边界测试：服务进程的环境绝不能
// 原样交给编译不可信源码的工具链。
func TestCompile_EnvAllowlistIsEnforced(t *testing.T) {
	runner := newRunner(t, testConfig("FAKETC_MODE"))
	t.Setenv("TELLURION_TEST_SECRET", "s3cr3t")
	t.Setenv("FAKETC_MODE", "")

	res, err := runner.Compile(context.Background(), asset.CompileRequest{Source: "package strategy"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if strings.Contains(res.Log, "s3cr3t") || strings.Contains(res.Log, "leaked secret") {
		t.Fatalf("environment leaked into the toolchain: %q", res.Log)
	}
	if !strings.Contains(string(res.Bytes), "artifact:") {
		t.Fatalf("artifact not produced: %q", res.Bytes)
	}
}

func TestCompile_LogIsTruncated(t *testing.T) {
	runner := newRunner(t, testConfig("FAKETC_LOG"))
	t.Setenv("FAKETC_LOG", "200000")

	res, err := runner.Compile(context.Background(), asset.CompileRequest{Source: "package strategy"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(res.Log) == 0 {
		t.Fatalf("log must not be empty")
	}
	if len(res.Log) > maxLogBytes {
		t.Fatalf("log = %d bytes, want <= %d", len(res.Log), maxLogBytes)
	}
}
