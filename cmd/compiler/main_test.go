package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"tellurion-strategy/internal/compile"
	"tellurion-strategy/internal/compilerjob"
)

// 无锁不编译：缺 --toolchain-lock 直接拒，job 都不读。
func TestRunRequiresToolchainLock(t *testing.T) {
	err := run([]string{
		"-job", "whatever.json",
		"-asset-url", "http://127.0.0.1:1",
		"-toolchain-name", "tinygo",
		"-toolchain-version", "0.33.0",
		"-toolchain-path", "tinygo",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "-toolchain-lock is required")
}

// caller 报的版本不在锁清单里 ⇒ ErrToolchainNotPinned（锁是部署物，
// 不是调用参数）。
func TestRunRejectsUnpinnedVersion(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "toolchains.lock")
	sum := sha256.Sum256([]byte("x"))
	lockJSON := `{"toolchains":[{"name":"tinygo","version":"0.33.0","sha256":"` + hex.EncodeToString(sum[:]) + `"}]}`
	require.NoError(t, os.WriteFile(lockPath, []byte(lockJSON), 0o600))

	err := run([]string{
		"-job", "whatever.json",
		"-asset-url", "http://127.0.0.1:1",
		"-toolchain-name", "tinygo",
		"-toolchain-version", "0.99.0",
		"-toolchain-path", "tinygo",
		"-toolchain-lock", lockPath,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, compile.ErrToolchainNotPinned)
}

// 版本在清单但二进制对不上 ⇒ ErrToolchainBinaryMismatch。
func TestRunRejectsBinaryNotMatchingPin(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "tinygo")
	require.NoError(t, os.WriteFile(bin, []byte("real pinned binary"), 0o700))
	lockPath := filepath.Join(dir, "toolchains.lock")
	sum := sha256.Sum256([]byte("real pinned binary"))
	lockJSON := `{"toolchains":[{"name":"tinygo","version":"0.33.0","sha256":"` + hex.EncodeToString(sum[:]) + `"}]}`
	require.NoError(t, os.WriteFile(lockPath, []byte(lockJSON), 0o600))
	require.NoError(t, os.WriteFile(bin, []byte("tampered"), 0o700))

	err := run([]string{
		"-job", "whatever.json",
		"-asset-url", "http://127.0.0.1:1",
		"-toolchain-name", "tinygo",
		"-toolchain-version", "0.33.0",
		"-toolchain-path", bin,
		"-toolchain-lock", lockPath,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, compile.ErrToolchainBinaryMismatch))
}

// ─── 批次 / 重试 / 报告 ────────────────────────────────────────

const fakeToolchainSource = `package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	var src, out string
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		if args[i] == "-o" && i+1 < len(args) {
			out = args[i+1]
			i++
			continue
		}
		if !strings.HasPrefix(args[i], "-") {
			src = args[i]
		}
	}
	b, err := os.ReadFile(src)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(4)
	}
	if strings.Contains(string(b), "boom") {
		fmt.Fprintln(os.Stderr, "faketc: deliberate failure")
		os.Exit(2)
	}
	if err := os.WriteFile(out, append([]byte("artifact:"), b...), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(5)
	}
}
`

func buildFakeToolchain(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(src, []byte(fakeToolchainSource), 0o600))
	name := "faketc"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(dir, name)
	out, err := exec.Command("go", "build", "-o", bin, src).CombinedOutput()
	require.NoErrorf(t, err, "build fake toolchain: %s", out)
	return bin
}

// compilerHarness 是一批 job 需要的输入物：锁文件 / job 文件 / 资产服务替身。
type compilerHarness struct {
	dir       string
	bin       string
	lockPath  string
	assetURL  string
	jobPaths  []string
	reportOut string
}

func newCompilerHarness(t *testing.T) *compilerHarness {
	t.Helper()
	dir := t.TempDir()
	bin := buildFakeToolchain(t)

	sum := sha256.Sum256(mustReadFile(t, bin))
	lockPath := filepath.Join(dir, "toolchains.lock")
	lockJSON := `{"toolchains":[{"name":"faketc","version":"0.1.0-test","sha256":"` + hex.EncodeToString(sum[:]) + `"}]}`
	require.NoError(t, os.WriteFile(lockPath, []byte(lockJSON), 0o600))

	// 登记端点只回显元数据；artifact_hash 自证由 Run 与服务端一致即可。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			StrategyID       string   `json:"strategy_id"`
			VersionHash      string   `json:"version_hash"`
			ArtifactHash     string   `json:"artifact_hash"`
			Carrier          string   `json:"carrier"`
			ABIVersion       string   `json:"abi_version"`
			Toolchain        string   `json:"toolchain"`
			SizeBytes        int64    `json:"size_bytes"`
			MemoryLimitBytes int64    `json:"memory_limit_bytes"`
			Imports          []string `json:"imports"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"strategy_id": req.StrategyID, "version_hash": req.VersionHash,
			"artifact_hash": req.ArtifactHash, "carrier": req.Carrier,
			"abi_version": req.ABIVersion, "toolchain": req.Toolchain,
			"size_bytes": req.SizeBytes, "memory_limit_bytes": req.MemoryLimitBytes,
			"imports": req.Imports, "status": "built",
		})
	}))
	t.Cleanup(srv.Close)

	return &compilerHarness{dir: dir, bin: bin, lockPath: lockPath, assetURL: srv.URL,
		reportOut: filepath.Join(dir, "report.json")}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return b
}

// addJob 写一个 job JSON 文件并登记路径。
func (h *compilerHarness) addJob(t *testing.T, jobID, strategyID, source string) {
	t.Helper()
	job := map[string]string{
		"strategy_id": strategyID, "version_hash": strings.Repeat("a", 64),
		"carrier": "wasm", "abi_version": "on_bar/2", "entry": "OnBar", "source": source,
	}
	if jobID != "" {
		job["job_id"] = jobID
	}
	raw, err := json.Marshal(job)
	require.NoError(t, err)
	path := filepath.Join(h.dir, strategyID+".json")
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	h.jobPaths = append(h.jobPaths, path)
}

func (h *compilerHarness) args(extra ...string) []string {
	args := []string{
		"-asset-url", h.assetURL,
		"-toolchain-name", "faketc",
		"-toolchain-version", "0.1.0-test",
		"-toolchain-path", h.bin,
		"-toolchain-lock", h.lockPath,
		"-arg", "-target=wasm-unknown",
		"-arg", "-o",
		"-arg", "{out}",
		"-arg", "{src}",
		"-timeout", "30s",
		"-retry-backoff", "0s",
		"-report", h.reportOut,
	}
	for _, path := range h.jobPaths {
		args = append(args, "-job", path)
	}
	return append(args, extra...)
}

func (h *compilerHarness) readReport(t *testing.T) report {
	t.Helper()
	raw, err := os.ReadFile(h.reportOut)
	require.NoError(t, err)
	var got report
	require.NoError(t, json.Unmarshal(raw, &got))
	return got
}

// 一批多个 job：限并发执行，全部成功时退出码为 0，报告按提交顺序可检索。
func TestRunProcessesBatch(t *testing.T) {
	h := newCompilerHarness(t)
	h.addJob(t, "job_a", "st_a", "package a")
	h.addJob(t, "job_b", "st_b", "package b")

	require.NoError(t, run(h.args("-workers", "2", "-max-attempts", "2")))
	got := h.readReport(t)
	assert.Equal(t, 2, got.Succeeded)
	assert.Equal(t, 0, got.Failed)
	require.Len(t, got.Jobs, 2)
	assert.Equal(t, "job_a", got.Jobs[0].JobID)
	assert.Equal(t, "job_b", got.Jobs[1].JobID)
	for _, rec := range got.Jobs {
		assert.Equal(t, compilerjob.StateSucceeded, rec.State)
		assert.Equal(t, 1, rec.Attempts)
		assert.NotEmpty(t, rec.ArtifactHash)
	}
}

// 一个 job 失败不带走整批：失败可检索（报告里带 job_id + attempts + error），
// 整批退出码非 0，但成功的那个照样登记。
func TestRunKeepsBatchAliveWhenOneJobFails(t *testing.T) {
	h := newCompilerHarness(t)
	h.addJob(t, "job_ok", "st_ok", "package ok")
	h.addJob(t, "job_bad", "st_bad", "package boom")

	err := run(h.args("-max-attempts", "2"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 of 2 build jobs failed")

	got := h.readReport(t)
	assert.Equal(t, 1, got.Succeeded)
	assert.Equal(t, 1, got.Failed)
	require.Len(t, got.Jobs, 2)

	bad := got.Jobs[1]
	assert.Equal(t, "job_bad", bad.JobID)
	assert.Equal(t, compilerjob.StateFailed, bad.State)
	assert.Equal(t, 2, bad.Attempts, "retries must be honored from the CLI")
	assert.Contains(t, bad.Error, "compile failed")
	assert.Contains(t, bad.Error, "deliberate failure")
	assert.Empty(t, bad.ArtifactHash)

	assert.Equal(t, compilerjob.StateSucceeded, got.Jobs[0].State)
}

// 关掉重试（-max-attempts 1）后失败作业只跑一次；这是「关掉重试 → 重试用例变红」
// 在 CLI 侧的对应口径。
func TestRunDisablesRetriesWithOneAttempt(t *testing.T) {
	h := newCompilerHarness(t)
	h.addJob(t, "job_bad", "st_bad", "package boom")

	require.Error(t, run(h.args("-max-attempts", "1")))
	got := h.readReport(t)
	require.Len(t, got.Jobs, 1)
	assert.Equal(t, 1, got.Jobs[0].Attempts)
	assert.Equal(t, compilerjob.StateFailed, got.Jobs[0].State)
}
