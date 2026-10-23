package compilerjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"tellurion-strategy/internal/asset"
)

// ─── 替身 ──────────────────────────────────────────────────────

// scriptedRunner 按第 N 次调用决定成功或失败（重试用例的「第一次坏、第二次好」）。
type scriptedRunner struct {
	bytes   []byte
	failFor func(attempt int) error

	mu       sync.Mutex
	attempts int
}

func (r *scriptedRunner) ToolchainID() string { return "faketc 1.0.0" }

func (r *scriptedRunner) Compile(ctx context.Context, req asset.CompileRequest) (asset.CompileResult, error) {
	r.mu.Lock()
	r.attempts++
	attempt := r.attempts
	r.mu.Unlock()
	if r.failFor != nil {
		if err := r.failFor(attempt); err != nil {
			return asset.CompileResult{}, err
		}
	}
	return asset.CompileResult{Bytes: append([]byte(nil), r.bytes...), Log: "ok"}, nil
}

// concurrencyRunner 记录同时在编译的作业数上限。
type concurrencyRunner struct {
	bytes []byte
	delay time.Duration

	mu          sync.Mutex
	inFlight    int
	maxInFlight int
}

func (r *concurrencyRunner) ToolchainID() string { return "faketc 1.0.0" }

func (r *concurrencyRunner) Compile(ctx context.Context, req asset.CompileRequest) (asset.CompileResult, error) {
	r.mu.Lock()
	r.inFlight++
	if r.inFlight > r.maxInFlight {
		r.maxInFlight = r.inFlight
	}
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.inFlight--
		r.mu.Unlock()
	}()
	select {
	case <-time.After(r.delay):
	case <-ctx.Done():
		return asset.CompileResult{}, ctx.Err()
	}
	return asset.CompileResult{Bytes: append([]byte(nil), r.bytes...), Log: "ok"}, nil
}

// echoRegisterServer 只回显登记请求里的元数据（不校验版本行），用于只测队列
// 机械行为的用例：返回的 artifact_hash 与服务端一致，Run 的自证门才走得通。
func echoRegisterServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req registerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"strategy_id":        req.StrategyID,
			"version_hash":       req.VersionHash,
			"artifact_hash":      req.ArtifactHash,
			"carrier":            req.Carrier,
			"abi_version":        req.ABIVersion,
			"toolchain":          req.Toolchain,
			"size_bytes":         req.SizeBytes,
			"memory_limit_bytes": req.MemoryLimitBytes,
			"imports":            req.Imports,
			"status":             "built",
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func compileFailure(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{asset.ErrCompileFailed}, args...)...)
}

// ─── 状态词表 ──────────────────────────────────────────────────

// 队列状态与 strategy.strategy_build_jobs 的 CHECK 必须是同一套词表：
// 换词表等于让「queued/running/终态」的表述在两处各说各话。
func TestQueueStatesMatchBuildJobTable(t *testing.T) {
	want := map[string]string{
		StateQueued:    "queued",
		StateRunning:   "running",
		StateSucceeded: "succeeded",
		StateFailed:    "failed",
	}
	for got, expect := range want {
		if got != expect {
			t.Fatalf("state %q, want %q", got, expect)
		}
	}
	// 同时钉住表侧：DDL 的 CHECK 少一个取值，这里的相等就不再是「同一套」。
	var ddl string
	for _, stmt := range asset.Migrations() {
		if strings.Contains(stmt, "strategy.strategy_build_jobs") {
			ddl = stmt
			break
		}
	}
	if ddl == "" {
		t.Fatal("strategy_build_jobs DDL not found")
	}
	check := "CHECK (state IN ('queued','running','succeeded','failed'))"
	if !strings.Contains(ddl, check) {
		t.Fatalf("build job state CHECK drifted; want %q in:\n%s", check, ddl)
	}
}

// ─── 重试 ──────────────────────────────────────────────────────

// 验收①：第一次失败、第二次成功 —— 终态 succeeded，attempts=2。
func TestQueue_RetrySecondAttemptSucceeds(t *testing.T) {
	fx := newFixture(t)
	wasm := wasmWithImports("env.get_bar")
	runner := &scriptedRunner{
		bytes: wasm,
		failFor: func(attempt int) error {
			if attempt == 1 {
				return compileFailure("toolchain faketc: transient")
			}
			return nil
		},
	}
	queue, err := NewQueue(Config{AssetURL: fx.srv.URL, Runner: runner}, QueueOptions{MaxAttempts: 3})
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}
	id, err := queue.Submit(fx.job())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	queue.Run(context.Background())

	rec, err := queue.Record(id)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if rec.State != StateSucceeded {
		t.Fatalf("state = %s, want succeeded (error: %s)", rec.State, rec.Error)
	}
	if rec.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", rec.Attempts)
	}
	if runner.attempts != 2 {
		t.Fatalf("runner calls = %d, want 2", runner.attempts)
	}
	if rec.ArtifactHash != asset.ArtifactHash(wasm) {
		t.Fatalf("artifact_hash = %s, want %s", rec.ArtifactHash, asset.ArtifactHash(wasm))
	}
	if rec.FinishedAt == nil || rec.StartedAt == nil {
		t.Fatalf("timestamps not recorded: %+v", rec)
	}
}

// 验收②：超过上限 → 终态 failed，attempts=MaxAttempts，error 可检索且不落 artifact。
func TestQueue_ExhaustsAttemptsAndFailsTerminal(t *testing.T) {
	fx := newFixture(t)
	runner := &scriptedRunner{
		bytes:   wasmWithImports("env.get_bar"),
		failFor: func(int) error { return compileFailure("toolchain faketc: still broken") },
	}
	queue, err := NewQueue(Config{AssetURL: fx.srv.URL, Runner: runner}, QueueOptions{MaxAttempts: 2})
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}
	id, err := queue.Submit(fx.job())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	queue.Run(context.Background())

	rec, err := queue.Record(id)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if rec.State != StateFailed {
		t.Fatalf("state = %s, want failed", rec.State)
	}
	if rec.Attempts != 2 || runner.attempts != 2 {
		t.Fatalf("attempts = %d / calls = %d, want 2", rec.Attempts, runner.attempts)
	}
	if !strings.Contains(rec.Error, asset.ErrCompileFailed.Error()) {
		t.Fatalf("error %q must carry the compile sentinel", rec.Error)
	}
	if !strings.Contains(rec.Error, "still broken") {
		t.Fatalf("error %q must carry the toolchain log", rec.Error)
	}
	if rec.ArtifactHash != "" {
		t.Fatalf("failed job recorded artifact_hash %q", rec.ArtifactHash)
	}
	if _, err := fx.svc.ArtifactForVersion(context.Background(), fx.id, fx.version, "faketc 1.0.0"); !errors.Is(err, asset.ErrArtifactNotFound) {
		t.Fatalf("failed job must not register an artifact; err = %v", err)
	}
}

// 不可重试的错误即便还有次数也立即终态：重试不掩盖确定性失败。
func TestQueue_NonRetryableFailsImmediately(t *testing.T) {
	server := echoRegisterServer(t)
	runner := &scriptedRunner{
		bytes:   wasmWithImports("env.get_bar"),
		failFor: func(int) error { return fmt.Errorf("%w: 9 MiB", asset.ErrArtifactTooLarge) },
	}
	queue, err := NewQueue(Config{AssetURL: server.URL, Runner: runner}, QueueOptions{MaxAttempts: 4})
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}
	id, err := queue.Submit(Job{
		StrategyID: "st_1", VersionHash: strings.Repeat("a", 64), Carrier: "wasm",
		ABIVersion: "on_bar/2", Entry: "OnBar", Source: "package strategy",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	queue.Run(context.Background())

	rec, err := queue.Record(id)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if rec.State != StateFailed || rec.Attempts != 1 {
		t.Fatalf("state = %s attempts = %d, want failed/1", rec.State, rec.Attempts)
	}
	if runner.attempts != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.attempts)
	}
}

// ─── 并发 ──────────────────────────────────────────────────────

func TestQueue_BoundsConcurrencyAndKeepsSubmissionOrder(t *testing.T) {
	server := echoRegisterServer(t)
	runner := &concurrencyRunner{bytes: wasmWithImports("env.get_bar"), delay: 30 * time.Millisecond}
	queue, err := NewQueue(Config{AssetURL: server.URL, Runner: runner}, QueueOptions{Workers: 2, MaxAttempts: 1})
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}
	const jobs = 6
	var ids []string
	for i := 0; i < jobs; i++ {
		id, err := queue.Submit(Job{
			JobID:      fmt.Sprintf("job_%d", i),
			StrategyID: fmt.Sprintf("st_%d", i), VersionHash: strings.Repeat("a", 64),
			Carrier: "wasm", ABIVersion: "on_bar/2", Entry: "OnBar", Source: "package strategy",
		})
		if err != nil {
			t.Fatalf("Submit %d: %v", i, err)
		}
		ids = append(ids, id)
	}
	queue.Run(context.Background())

	records := queue.Records()
	if len(records) != jobs {
		t.Fatalf("records = %d, want %d", len(records), jobs)
	}
	for i, rec := range records {
		if rec.JobID != ids[i] {
			t.Fatalf("records[%d] = %s, want %s (submission order)", i, rec.JobID, ids[i])
		}
		if rec.State != StateSucceeded {
			t.Fatalf("job %s state = %s (%s)", rec.JobID, rec.State, rec.Error)
		}
	}
	runner.mu.Lock()
	maxInFlight := runner.maxInFlight
	runner.mu.Unlock()
	if maxInFlight != 2 {
		t.Fatalf("max in flight = %d, want 2 (bounded by Workers)", maxInFlight)
	}
}

// ─── 排队与检索的边界 ──────────────────────────────────────────

func TestQueue_PrefersExternalJobIDAndRejectsDuplicates(t *testing.T) {
	server := echoRegisterServer(t)
	queue, err := NewQueue(Config{AssetURL: server.URL, Runner: &scriptedRunner{bytes: wasmWithImports("env.get_bar")}},
		QueueOptions{MaxAttempts: 1})
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}
	job := Job{
		JobID: "job_from_asset_service", StrategyID: "st_1", VersionHash: strings.Repeat("a", 64),
		Carrier: "wasm", ABIVersion: "on_bar/2", Entry: "OnBar", Source: "package strategy",
	}
	id, err := queue.Submit(job)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if id != "job_from_asset_service" {
		t.Fatalf("id = %q, want the external job id", id)
	}
	if _, err := queue.Submit(job); !errors.Is(err, asset.ErrInvalidParameter) {
		t.Fatalf("duplicate submit err = %v, want ErrInvalidParameter", err)
	}
}

func TestQueue_RejectsInvalidJobAndUnknownRecord(t *testing.T) {
	server := echoRegisterServer(t)
	queue, err := NewQueue(Config{AssetURL: server.URL, Runner: &scriptedRunner{}},
		QueueOptions{MaxAttempts: 1})
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}
	if _, err := queue.Submit(Job{StrategyID: "st_1", VersionHash: strings.Repeat("a", 64),
		Carrier: "wasm", Entry: "OnBar"}); !errors.Is(err, asset.ErrSourceMissing) {
		t.Fatalf("submit invalid job err = %v, want ErrSourceMissing", err)
	}
	if _, err := queue.Record("job_missing"); !errors.Is(err, asset.ErrJobNotFound) {
		t.Fatalf("unknown record err = %v, want ErrJobNotFound", err)
	}
	if records := queue.Records(); len(records) != 0 {
		t.Fatalf("invalid job must not be queued, got %d records", len(records))
	}
}

func TestQueue_SubmitAfterRunIsRejected(t *testing.T) {
	server := echoRegisterServer(t)
	queue, err := NewQueue(Config{AssetURL: server.URL, Runner: &scriptedRunner{bytes: wasmWithImports("env.get_bar")}},
		QueueOptions{MaxAttempts: 1})
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}
	queue.Run(context.Background())
	_, err = queue.Submit(Job{StrategyID: "st_1", VersionHash: strings.Repeat("a", 64),
		Carrier: "wasm", Entry: "OnBar", Source: "package strategy"})
	if !errors.Is(err, ErrQueueClosed) {
		t.Fatalf("err = %v, want ErrQueueClosed", err)
	}
}

func TestNewQueue_RequiresRunner(t *testing.T) {
	if _, err := NewQueue(Config{AssetURL: "http://127.0.0.1:1"}, QueueOptions{}); !errors.Is(err, asset.ErrCompileUnavailable) {
		t.Fatalf("err = %v, want ErrCompileUnavailable", err)
	}
}
