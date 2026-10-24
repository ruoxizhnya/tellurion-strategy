package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"tellurion-strategy/internal/asset"
)

// dispatchCompiler 是派发模式的替身：SubmitBuild 只落 queued 作业并调
// DispatchBuild，作业状态由测试显式经内部端点回写。
type dispatchCompiler struct {
	id string
}

func (d *dispatchCompiler) ToolchainID() string { return d.id }

func (d *dispatchCompiler) Compile(context.Context, asset.CompileRequest) (asset.CompileResult, error) {
	return asset.CompileResult{}, errors.New("Compile must not run in dispatch mode")
}

func (d *dispatchCompiler) DispatchBuild(context.Context, asset.DispatchRequest) error {
	return nil
}

func reportBody(state, artifactHash, errorCode, message string) map[string]any {
	body := map[string]any{"state": state}
	if artifactHash != "" {
		body["artifact_hash"] = artifactHash
	}
	if errorCode != "" {
		body["error_code"] = errorCode
	}
	if message != "" {
		body["error"] = message
	}
	return body
}

// queuedJob 走 build 端点拿到一个 queued 作业并返回 id / version。
func queuedJob(t *testing.T, srv *httptest.Server, id, version, toolchain string) string {
	t.Helper()
	resp := call(t, srv, http.MethodPost, "/api/strategies/"+id+"/build",
		map[string]string{"version_hash": version, "toolchain": toolchain}, nil)
	wantStatus(t, resp, http.StatusAccepted)
	jobID, _ := decodeObject(t, resp.Body)["job_id"].(string)
	if jobID == "" {
		t.Fatalf("build returned no job_id: %s", resp.Body)
	}
	return jobID
}

// 验收：queued → running → succeeded 全路径，且有 artifact 才能成功。
func TestAPI_InternalReportBuildJob_SucceedsWithArtifact(t *testing.T) {
	wasm := wasmWithImports("env.get_bar", "env.emit_target2")
	srv, _ := newServer(t, &dispatchCompiler{id: "tinygo-test"})
	id := createWasm(t, srv, "on_bar/2", "market")
	version := putSource(t, srv, id, "package strategy")

	jobID := queuedJob(t, srv, id, version, "tinygo-test")
	resp := call(t, srv, http.MethodGet, "/api/strategies/"+id+"/builds/"+jobID, nil, nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeObject(t, resp.Body)["state"]; got != "queued" {
		t.Fatalf("state right after build = %v, want queued", got)
	}

	// queued → running。
	path := "/internal/strategies/" + id + "/builds/" + jobID
	resp = call(t, srv, http.MethodPost, path, reportBody("running", "", "", ""), nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeObject(t, resp.Body)["state"]; got != "running" {
		t.Fatalf("state after running report = %v", got)
	}

	// succeeded 缺 artifact → 409 artifact_invalid。
	resp = call(t, srv, http.MethodPost, path,
		reportBody("succeeded", asset.ArtifactHash(wasm), "", ""), nil)
	wantErrorCode(t, resp, http.StatusConflict, "artifact_invalid")

	// 登记 artifact 后重报 succeeded。
	resp = call(t, srv, http.MethodPost, "/internal/strategies/"+id+"/artifacts",
		registerBody(id, version, wasm, []string{"env.get_bar", "env.emit_target2"}, "tinygo-test"), nil)
	wantStatus(t, resp, http.StatusCreated)
	resp = call(t, srv, http.MethodPost, path,
		reportBody("succeeded", asset.ArtifactHash(wasm), "", ""), nil)
	wantStatus(t, resp, http.StatusOK)
	body := decodeObject(t, resp.Body)
	if body["state"] != "succeeded" || body["artifact_hash"] != asset.ArtifactHash(wasm) {
		t.Fatalf("succeeded report body = %s", resp.Body)
	}
	if _, ok := body["finished_at"]; !ok {
		t.Fatalf("succeeded report must carry finished_at: %s", resp.Body)
	}

	// 同终态重复上报幂等。
	resp = call(t, srv, http.MethodPost, path,
		reportBody("succeeded", asset.ArtifactHash(wasm), "", ""), nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeObject(t, resp.Body)["state"]; got != "succeeded" {
		t.Fatalf("idempotent report state = %v", got)
	}

	// 终态不可回退。
	resp = call(t, srv, http.MethodPost, path, reportBody("running", "", "", ""), nil)
	wantErrorCode(t, resp, http.StatusConflict, "invalid_transition")

	// 公开轮询端点看到同一终态。
	resp = call(t, srv, http.MethodGet, "/api/strategies/"+id+"/builds/"+jobID, nil, nil)
	wantStatus(t, resp, http.StatusOK)
	body = decodeObject(t, resp.Body)
	if body["state"] != "succeeded" || body["artifact_hash"] != asset.ArtifactHash(wasm) {
		t.Fatalf("public job endpoint = %s", resp.Body)
	}
}

// 验收：queued 直达 failed，带 error_code；缺省归 compile_failed。
func TestAPI_InternalReportBuildJob_FailedWithErrorCode(t *testing.T) {
	srv, _ := newServer(t, &dispatchCompiler{id: "tinygo-test"})
	id := createWasm(t, srv, "on_bar/2", "market")
	version := putSource(t, srv, id, "package strategy")
	path := "/internal/strategies/" + id + "/builds/"

	jobID := queuedJob(t, srv, id, version, "tinygo-test")
	resp := call(t, srv, http.MethodPost, path+jobID,
		reportBody("failed", "", asset.ErrorCodePayloadTooLarge, "source exceeds the limit"), nil)
	wantStatus(t, resp, http.StatusOK)
	body := decodeObject(t, resp.Body)
	if body["state"] != "failed" || body["error_code"] != asset.ErrorCodePayloadTooLarge {
		t.Fatalf("failed report body = %s", resp.Body)
	}
	if body["error"] != "source exceeds the limit" {
		t.Fatalf("failed report error = %v", body["error"])
	}

	// 缺省 error_code → compile_failed。
	other := queuedJob(t, srv, id, version, "tinygo-test")
	resp = call(t, srv, http.MethodPost, path+other, reportBody("failed", "", "", "boom"), nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeObject(t, resp.Body)["error_code"]; got != asset.ErrorCodeCompileFailed {
		t.Fatalf("default error_code = %v, want %s", got, asset.ErrorCodeCompileFailed)
	}

	// 终态重复上报幂等（同 failed）。
	resp = call(t, srv, http.MethodPost, path+other, reportBody("failed", "", "", "boom"), nil)
	wantStatus(t, resp, http.StatusOK)
}

// 验收：跨策略与不存在的 job 一律 404 job_not_found（不跨策略枚举）。
func TestAPI_InternalReportBuildJob_NotFound(t *testing.T) {
	srv, _ := newServer(t, &dispatchCompiler{id: "tinygo-test"})
	id := createWasm(t, srv, "on_bar/2", "market")
	version := putSource(t, srv, id, "package strategy")
	jobID := queuedJob(t, srv, id, version, "tinygo-test")

	wantErrorCode(t, call(t, srv, http.MethodPost,
		"/internal/strategies/st_other/builds/"+jobID, reportBody("running", "", "", ""), nil),
		http.StatusNotFound, "job_not_found")
	wantErrorCode(t, call(t, srv, http.MethodPost,
		"/internal/strategies/"+id+"/builds/st_missing", reportBody("running", "", "", ""), nil),
		http.StatusNotFound, "job_not_found")
}

// 验收：非法状态 / 非法转移 / succeeded 缺 artifact / 坏 hash 的错误码。
func TestAPI_InternalReportBuildJob_RejectsInvalidTransitions(t *testing.T) {
	srv, _ := newServer(t, &dispatchCompiler{id: "tinygo-test"})
	id := createWasm(t, srv, "on_bar/2", "market")
	version := putSource(t, srv, id, "package strategy")
	prefix := "/internal/strategies/" + id + "/builds/"

	jobID := queuedJob(t, srv, id, version, "tinygo-test")
	wantErrorCode(t, call(t, srv, http.MethodPost, prefix+jobID,
		reportBody("exploded", "", "", ""), nil),
		http.StatusBadRequest, "invalid_request")

	// queued 直接报 failed 合法；之后报 running 非法。
	resp := call(t, srv, http.MethodPost, prefix+jobID, reportBody("failed", "", "", "boom"), nil)
	wantStatus(t, resp, http.StatusOK)
	wantErrorCode(t, call(t, srv, http.MethodPost, prefix+jobID,
		reportBody("running", "", "", ""), nil),
		http.StatusConflict, "invalid_transition")

	// succeeded 的 artifact_hash 不合形状 → 409 artifact_invalid。
	other := queuedJob(t, srv, id, version, "tinygo-test")
	wantErrorCode(t, call(t, srv, http.MethodPost, prefix+other,
		reportBody("succeeded", "not-a-hash", "", ""), nil),
		http.StatusConflict, "artifact_invalid")
}
