package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruoxizhnya/tellurion-kit/auth"

	"tellurion-strategy/internal/asset"
	"tellurion-strategy/internal/compilerclient"
	"tellurion-strategy/internal/httpapi"
)

// recordingStore 记录最近一次落库的作业：G4 演练要能证明「503 之后那个
// queued 行没有悬空」，而不只是看 HTTP 码。
type recordingStore struct {
	*asset.Memory
	saved asset.BuildJob
}

func (r *recordingStore) SaveJob(ctx context.Context, job asset.BuildJob) error {
	if err := r.Memory.SaveJob(ctx, job); err != nil {
		return err
	}
	r.saved = job
	return nil
}

// recoveringCompiler 是「编译器恢复可用」的替身：派发成功即返回，作业由
// 测试经内部端点显式推终态。
type recoveringCompiler struct{ id string }

func (c *recoveringCompiler) ToolchainID() string { return c.id }

func (c *recoveringCompiler) Compile(context.Context, asset.CompileRequest) (asset.CompileResult, error) {
	return asset.CompileResult{}, errors.New("Compile is disabled in dispatch mode")
}

func (c *recoveringCompiler) DispatchBuild(context.Context, asset.DispatchRequest) error { return nil }

func newDrillServer(t *testing.T, store asset.Store, compiler asset.Compiler) *httptest.Server {
	t.Helper()
	svc, err := asset.New(asset.Options{Store: store, Compiler: compiler})
	if err != nil {
		t.Fatalf("asset.New: %v", err)
	}
	// 无 secret 的 TokenService = 开放访问：内部端点仍经过闸，但就地放行
	// （与 cmd/assetservice 在回环上的启动姿态一致）。
	api, err := httpapi.New(svc,
		httpapi.WithAuthorizer(httpapi.NewAuthorizer(auth.NewTokenService(auth.Config{}))),
		httpapi.WithPromotionLog(asset.NewMemoryPromotionLog()),
	)
	if err != nil {
		t.Fatalf("httpapi.New: %v", err)
	}
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	return srv
}

func drillCall(t *testing.T, srv *httptest.Server, method, path string, body any) (int, map[string]any, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, srv.URL+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var obj map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &obj)
	}
	return resp.StatusCode, obj, raw
}

// 验收 ⑤（G4）：把 -compiler-bin 指向不存在的文件 ⇒ build 端点 503
// compile_unavailable（不静默成功），刚落的 queued 行收口成 failed；把同一个
// store 接上一个可用的派发端口后，同一份源码重新 build ⇒ 202。
func TestG4Drill_MissingCompilerBinFailsClosedThenRecovers(t *testing.T) {
	store := &recordingStore{Memory: asset.NewMemory()}
	missing := filepath.Join(t.TempDir(), "compiler-that-does-not-exist")
	broken, err := compilerclient.New(compilerclient.Config{
		Bin:              missing,
		AssetURL:         "http://127.0.0.1:1",
		ToolchainName:    "tinygo",
		ToolchainVersion: "0.34.0",
		ToolchainPath:    missing,
		ToolchainLock:    missing,
		Timeout:          30 * time.Second,
		MaxAttempts:      2,
		RetryBackoff:     time.Second,
	})
	if err != nil {
		t.Fatalf("compilerclient.New: %v", err)
	}
	srv := newDrillServer(t, store, broken)

	status, obj, raw := drillCall(t, srv, http.MethodPost, "/api/strategies", map[string]any{
		"carrier": "wasm", "abi_version": "on_bar/2", "scope": "market", "entry": "OnBar",
		"capability": map[string]any{
			"frequencies": []string{"1d"}, "markets": []string{"A"}, "features": []string{"momentum"},
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("create status = %d (body: %s)", status, raw)
	}
	id, _ := obj["id"].(string)
	if id == "" {
		t.Fatalf("create returned no id: %s", raw)
	}
	status, obj, raw = drillCall(t, srv, http.MethodPut, "/api/strategies/"+id+"/source",
		map[string]string{"source": "package strategy\n\n//go:wasmexport OnBar\nfunc OnBar() int32 { return 0 }\n"})
	if status != http.StatusOK {
		t.Fatalf("source status = %d (body: %s)", status, raw)
	}
	version, _ := obj["version_hash"].(string)

	// 断腿：二进制不存在 ⇒ 503，不是 202，也不是静默 200。
	status, obj, raw = drillCall(t, srv, http.MethodPost, "/api/strategies/"+id+"/build",
		map[string]string{"version_hash": version, "toolchain": "tinygo 0.34.0"})
	if status != http.StatusServiceUnavailable {
		t.Fatalf("build status = %d, want 503 (body: %s)", status, raw)
	}
	if obj["code"] != "compile_unavailable" {
		t.Fatalf("build error code = %v, want compile_unavailable (body: %s)", obj["code"], raw)
	}
	if store.saved.ID == "" {
		t.Fatal("the queued job row was never persisted")
	}
	stored, err := store.Memory.GetJob(context.Background(), store.saved.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if stored.State != asset.JobFailed || stored.ErrorCode != asset.ErrorCodeCompileUnavailable {
		t.Fatalf("job after dispatch failure = %+v, want failed/compile_unavailable", stored)
	}

	// 恢复：同一个 store 换上可用的编译器端口，同一份源码重新 build ⇒ 202。
	recovered := newDrillServer(t, store, &recoveringCompiler{id: "tinygo 0.34.0"})
	status, obj, raw = drillCall(t, recovered, http.MethodPost, "/api/strategies/"+id+"/build",
		map[string]string{"version_hash": version, "toolchain": "tinygo 0.34.0"})
	if status != http.StatusAccepted {
		t.Fatalf("recovered build status = %d, want 202 (body: %s)", status, raw)
	}
	jobID, _ := obj["job_id"].(string)
	if jobID == "" {
		t.Fatalf("recovered build returned no job_id: %s", raw)
	}
	status, obj, raw = drillCall(t, recovered, http.MethodGet, "/api/strategies/"+id+"/builds/"+jobID, nil)
	if status != http.StatusOK || obj["state"] != asset.JobQueued {
		t.Fatalf("recovered job status = %d state = %v, want 200/queued (body: %s)", status, obj["state"], raw)
	}
}
