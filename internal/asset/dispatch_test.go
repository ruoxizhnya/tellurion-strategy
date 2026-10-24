package asset

import (
	"context"
	"errors"
	"testing"

	contracts "github.com/ruoxizhnya/tellurion-contracts/engine"
)

// fakeDispatcher 同时实现 Compiler 与 BuildDispatcher：派发模式下 Compile
// 不该被调用，compileCall 为 0 就是「没有在资产服务进程内编译」的直接证据。
type fakeDispatcher struct {
	id           string
	err          error
	dispatchCall int
	compileCall  int
	last         DispatchRequest
}

func (f *fakeDispatcher) ToolchainID() string { return f.id }

func (f *fakeDispatcher) Compile(context.Context, CompileRequest) (CompileResult, error) {
	f.compileCall++
	return CompileResult{}, errors.New("Compile must not run in dispatch mode")
}

func (f *fakeDispatcher) DispatchBuild(_ context.Context, req DispatchRequest) error {
	f.dispatchCall++
	f.last = req
	return f.err
}

// recordingStore 记录最近一次落库的作业，让「派发失败后 202 之前的那行」可被检查。
type recordingStore struct {
	*Memory
	savedJob BuildJob
}

func (r *recordingStore) SaveJob(ctx context.Context, job BuildJob) error {
	if err := r.Memory.SaveJob(ctx, job); err != nil {
		return err
	}
	r.savedJob = job
	return nil
}

func TestSubmitBuild_DispatchesWithoutLocalCompile(t *testing.T) {
	ctx := context.Background()
	dispatcher := &fakeDispatcher{id: "tinygo 0.34"}
	svc, _ := newHarness(t, dispatcher)
	def := mustCreateWasm(t, svc, "on_bar/2", contracts.ScopeMarket)
	version := mustPutSource(t, svc, def.ID, "package strategy; func OnBar() {}")

	job, err := svc.SubmitBuild(ctx, def.ID, version.VersionHash, dispatcher.id)
	if err != nil {
		t.Fatalf("SubmitBuild: %v", err)
	}
	if job.State != JobQueued || job.ID == "" || job.Toolchain != dispatcher.id {
		t.Fatalf("job = %+v, want a queued job with the compiler toolchain", job)
	}
	if dispatcher.dispatchCall != 1 {
		t.Fatalf("DispatchBuild calls = %d, want 1", dispatcher.dispatchCall)
	}
	if dispatcher.compileCall != 0 {
		t.Fatalf("Compile calls = %d; dispatch mode must not compile in-process", dispatcher.compileCall)
	}
	if dispatcher.last.JobID != job.ID || dispatcher.last.StrategyID != def.ID ||
		dispatcher.last.VersionHash != version.VersionHash ||
		dispatcher.last.Source != "package strategy; func OnBar() {}" {
		t.Fatalf("dispatch request = %+v", dispatcher.last)
	}
	if dispatcher.last.MemoryLimitBytes != MaxMemoryLimitBytes {
		t.Fatalf("dispatch memory limit = %d, want %d", dispatcher.last.MemoryLimitBytes, MaxMemoryLimitBytes)
	}

	// 派发后作业仍是 queued，等编译进程回写 running / 终态。
	stored, err := svc.Job(ctx, job.ID)
	if err != nil {
		t.Fatalf("Job: %v", err)
	}
	if stored.State != JobQueued {
		t.Fatalf("stored state = %s, want queued", stored.State)
	}
}

func TestSubmitBuild_DispatchFailureMarksJobFailed(t *testing.T) {
	ctx := context.Background()
	dispatcher := &fakeDispatcher{id: "tinygo 0.34", err: errors.New("exec: compiler binary not found")}
	store := &recordingStore{Memory: NewMemory()}
	svc, err := New(Options{Store: store, Compiler: dispatcher, IDGen: seqIDs(), Now: fixedNow})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	def := mustCreateWasm(t, svc, "on_bar/2", contracts.ScopeMarket)
	version := mustPutSource(t, svc, def.ID, "package strategy")

	_, err = svc.SubmitBuild(ctx, def.ID, version.VersionHash, "")
	if !errors.Is(err, ErrCompileUnavailable) {
		t.Fatalf("err = %v, want ErrCompileUnavailable", err)
	}
	if store.savedJob.ID == "" {
		t.Fatal("SubmitBuild must persist the queued job before dispatching")
	}

	// 刚落的 queued 行不能悬空：派发失败必须收口成 failed + compile_unavailable。
	stored, err := svc.Job(ctx, store.savedJob.ID)
	if err != nil {
		t.Fatalf("Job: %v", err)
	}
	if stored.State != JobFailed {
		t.Fatalf("state = %s, want failed", stored.State)
	}
	if stored.ErrorCode != ErrorCodeCompileUnavailable {
		t.Fatalf("error_code = %q, want %q", stored.ErrorCode, ErrorCodeCompileUnavailable)
	}
	if stored.FinishedAt == nil {
		t.Fatalf("failed job has no finished_at: %+v", stored)
	}
	if stored.Error == "" {
		t.Fatalf("failed job has no error message: %+v", stored)
	}
}

func TestAllowedJobTransition(t *testing.T) {
	tests := []struct {
		from, to string
		want     bool
	}{
		{JobQueued, JobRunning, true},
		{JobQueued, JobSucceeded, true},
		{JobQueued, JobFailed, true},
		{JobRunning, JobSucceeded, true},
		{JobRunning, JobFailed, true},
		{JobRunning, JobRunning, true},
		{JobSucceeded, JobRunning, false},
		{JobSucceeded, JobFailed, false},
		{JobFailed, JobRunning, false},
		{JobFailed, JobSucceeded, false},
		{"", JobRunning, false},
	}
	for _, tc := range tests {
		if got := allowedJobTransition(tc.from, tc.to); got != tc.want {
			t.Fatalf("allowedJobTransition(%q, %q) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}
