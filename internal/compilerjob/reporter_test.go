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

	"tellurion-strategy/internal/asset"
)

// ─── StatusReporter：内部端点的 wire 形状 ──────────────────────

type recordedReport struct {
	Path string
	Body asset.BuildJobReport
}

func TestStatusReporter_SkipsJobWithoutJobID(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	reporter := &StatusReporter{AssetURL: srv.URL}
	job := Job{StrategyID: "st_1", VersionHash: strings.Repeat("a", 64), Source: "src"}
	if err := reporter.Report(context.Background(), job, StateRunning, "", "", ""); err != nil {
		t.Fatalf("Report with empty job id: %v", err)
	}
	if requests != 0 {
		t.Fatalf("reporter sent %d requests for a job without job_id; want 0", requests)
	}
}

func TestStatusReporter_PostsInternalEndpoint(t *testing.T) {
	var got recordedReport
	var method string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		got.Path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&got.Body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"state":"running"}`))
	}))
	defer srv.Close()

	reporter := &StatusReporter{AssetURL: srv.URL + "/"}
	job := Job{JobID: "job_1", StrategyID: "st_1", VersionHash: strings.Repeat("a", 64), Source: "src"}
	err := reporter.Report(context.Background(), job, StateFailed, "", asset.ErrorCodePayloadTooLarge, "too big")
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if method != http.MethodPost {
		t.Fatalf("method = %s, want POST", method)
	}
	if got.Path != "/internal/strategies/st_1/builds/job_1" {
		t.Fatalf("path = %q", got.Path)
	}
	if got.Body.State != StateFailed || got.Body.ErrorCode != asset.ErrorCodePayloadTooLarge ||
		got.Body.Error != "too big" {
		t.Fatalf("report body = %+v", got.Body)
	}
}

func TestStatusReporter_Non2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "conflict", http.StatusConflict)
	}))
	defer srv.Close()
	reporter := &StatusReporter{AssetURL: srv.URL}
	job := Job{JobID: "job_1", StrategyID: "st_1"}
	if err := reporter.Report(context.Background(), job, StateRunning, "", "", ""); err == nil {
		t.Fatal("non-2xx response must be an error")
	}
}

func TestErrorCodeFor(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"compile failed", fmt.Errorf("%w: boom", asset.ErrCompileFailed), asset.ErrorCodeCompileFailed},
		{"compile unavailable", asset.ErrCompileUnavailable, asset.ErrorCodeCompileUnavailable},
		{"source too large", asset.ErrSourceTooLarge, asset.ErrorCodePayloadTooLarge},
		{"artifact too large", asset.ErrArtifactTooLarge, asset.ErrorCodePayloadTooLarge},
		{"hash mismatch", asset.ErrArtifactHashMismatch, asset.ErrorCodeArtifactInvalid},
		{"invalid artifact", asset.ErrInvalidArtifact, asset.ErrorCodeArtifactInvalid},
		{"unknown", errors.New("something else"), asset.ErrorCodeCompileFailed},
	}
	for _, tc := range tests {
		if got := ErrorCodeFor(tc.err); got != tc.want {
			t.Fatalf("%s: ErrorCodeFor = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// ─── Queue 的状态回写 ─────────────────────────────────────────

type reportCall struct {
	JobID        string
	State        string
	ArtifactHash string
	ErrorCode    string
	Message      string
}

type fakeReporter struct {
	mu    sync.Mutex
	calls []reportCall
	// failOn 是 1-based 的调用序号，命中即返回错误（测「running 上报失败按可重试」）。
	failOn map[int]error
	count  int
}

func (f *fakeReporter) Report(_ context.Context, job Job, state, artifactHash, errorCode, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count++
	if err, ok := f.failOn[f.count]; ok {
		return err
	}
	f.calls = append(f.calls, reportCall{
		JobID: job.JobID, State: state, ArtifactHash: artifactHash,
		ErrorCode: errorCode, Message: message,
	})
	return nil
}

func (f *fakeReporter) snapshot() []reportCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]reportCall(nil), f.calls...)
}

func TestQueue_ReportsRunningAndTerminal(t *testing.T) {
	fx := newFixture(t)
	wasm := wasmWithImports("env.get_bar")
	reporter := &fakeReporter{}
	job := fx.job()
	job.JobID = "job_reported"
	queue, err := NewQueue(Config{AssetURL: fx.srv.URL, Runner: &scriptedRunner{bytes: wasm}, Reporter: reporter},
		QueueOptions{MaxAttempts: 1})
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}
	if _, err := queue.Submit(job); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	queue.Run(context.Background())

	calls := reporter.snapshot()
	if len(calls) != 2 {
		t.Fatalf("reports = %+v, want running + succeeded", calls)
	}
	if calls[0].State != StateRunning || calls[0].JobID != "job_reported" {
		t.Fatalf("first report = %+v, want running", calls[0])
	}
	if calls[1].State != StateSucceeded || calls[1].ArtifactHash != asset.ArtifactHash(wasm) {
		t.Fatalf("second report = %+v, want succeeded with artifact_hash", calls[1])
	}
	if calls[1].ErrorCode != "" {
		t.Fatalf("succeeded report carries error_code %q", calls[1].ErrorCode)
	}
}

func TestQueue_ReportsTerminalFailureCode(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode string
	}{
		{"compile failed", fmt.Errorf("%w: boom", asset.ErrCompileFailed), asset.ErrorCodeCompileFailed},
		{"compile unavailable", fmt.Errorf("%w: no toolchain", asset.ErrCompileUnavailable), asset.ErrorCodeCompileUnavailable},
		{"payload too large", asset.ErrSourceTooLarge, asset.ErrorCodePayloadTooLarge},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fx := newFixture(t)
			reporter := &fakeReporter{}
			job := fx.job()
			job.JobID = "job_fail"
			queue, err := NewQueue(Config{
				AssetURL: fx.srv.URL,
				Runner:   &scriptedRunner{failFor: func(int) error { return tc.err }},
				Reporter: reporter,
			}, QueueOptions{MaxAttempts: 1})
			if err != nil {
				t.Fatalf("NewQueue: %v", err)
			}
			if _, err := queue.Submit(job); err != nil {
				t.Fatalf("Submit: %v", err)
			}
			queue.Run(context.Background())

			calls := reporter.snapshot()
			if len(calls) != 2 {
				t.Fatalf("reports = %+v, want running + failed", calls)
			}
			if calls[0].State != StateRunning {
				t.Fatalf("first report = %+v, want running", calls[0])
			}
			if calls[1].State != StateFailed || calls[1].ErrorCode != tc.wantCode {
				t.Fatalf("terminal report = %+v, want failed/%s", calls[1], tc.wantCode)
			}
			if calls[1].Message == "" {
				t.Fatalf("terminal report must carry the error message: %+v", calls[1])
			}
		})
	}
}

// running 上报失败按可重试错误处理：下一次尝试重报 running，编译仍只跑一次。
func TestQueue_RetriesRunningReport(t *testing.T) {
	fx := newFixture(t)
	wasm := wasmWithImports("env.get_bar")
	reporter := &fakeReporter{failOn: map[int]error{1: errors.New("asset service down")}}
	job := fx.job()
	job.JobID = "job_retry_report"
	queue, err := NewQueue(Config{
		AssetURL: fx.srv.URL,
		Runner:   &scriptedRunner{bytes: wasm},
		Reporter: reporter,
	}, QueueOptions{MaxAttempts: 2})
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}
	if _, err := queue.Submit(job); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	queue.Run(context.Background())

	calls := reporter.snapshot()
	if len(calls) != 2 {
		t.Fatalf("reports = %+v, want running then succeeded", calls)
	}
	if calls[0].State != StateRunning || calls[1].State != StateSucceeded {
		t.Fatalf("report states = %s/%s", calls[0].State, calls[1].State)
	}
	rec, err := queue.Record("job_retry_report")
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if rec.State != StateSucceeded || rec.Attempts != 1 {
		t.Fatalf("record = %+v; report retry must not consume a compile attempt", rec)
	}
}

// 终态上报失败不能把已经成功的作业改判：进程内记录仍是 succeeded。
func TestQueue_TerminalReportFailureDoesNotFlipSuccess(t *testing.T) {
	fx := newFixture(t)
	wasm := wasmWithImports("env.get_bar")
	// 第 2 次（终态）上报失败。
	reporter := &fakeReporter{failOn: map[int]error{2: errors.New("asset service down")}}
	job := fx.job()
	job.JobID = "job_terminal_report"
	queue, err := NewQueue(Config{
		AssetURL: fx.srv.URL,
		Runner:   &scriptedRunner{bytes: wasm},
		Reporter: reporter,
	}, QueueOptions{MaxAttempts: 1})
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}
	if _, err := queue.Submit(job); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	queue.Run(context.Background())

	rec, err := queue.Record("job_terminal_report")
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if rec.State != StateSucceeded || rec.ArtifactHash != asset.ArtifactHash(wasm) {
		t.Fatalf("record = %+v; a failed report must not change the in-process verdict", rec)
	}
}

// JobID 为空时不上报（兼容现有一批 job 文件用法）。
func TestQueue_DoesNotReportJobsWithoutJobID(t *testing.T) {
	fx := newFixture(t)
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	queue, err := NewQueue(Config{
		AssetURL: fx.srv.URL,
		Runner:   &scriptedRunner{bytes: wasmWithImports("env.get_bar")},
		Reporter: &StatusReporter{AssetURL: srv.URL},
	}, QueueOptions{MaxAttempts: 1})
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}
	job := fx.job()
	job.JobID = ""
	if _, err := queue.Submit(job); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	queue.Run(context.Background())

	if requests != 0 {
		t.Fatalf("reporter sent %d requests; jobs without an asset-service job_id must not be reported", requests)
	}
}
