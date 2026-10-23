package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"tellurion-strategy/internal/asset"
)

// ─── 语料与替身 ────────────────────────────────────────────────

// wasmWithImports 造一个最小的合法 wasm：magic + version + import section。
// 每个 import 写成 `module.field`。
func wasmWithImports(imports ...string) []byte {
	var payload []byte
	payload = appendULEB(payload, uint64(len(imports)))
	for _, imp := range imports {
		module, field, ok := strings.Cut(imp, ".")
		if !ok {
			module, field = imp, "fn"
		}
		payload = appendName(payload, module)
		payload = appendName(payload, field)
		payload = append(payload, 0x00)  // kind: func
		payload = appendULEB(payload, 0) // typeidx
	}
	out := []byte("\x00asm")
	var version [4]byte
	binary.LittleEndian.PutUint32(version[:], 1)
	out = append(out, version[:]...)
	out = append(out, 2) // import section
	out = appendULEB(out, uint64(len(payload)))
	return append(out, payload...)
}

func appendULEB(dst []byte, v uint64) []byte {
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		dst = append(dst, b)
		if v == 0 {
			return dst
		}
	}
}

func appendName(dst []byte, s string) []byte {
	dst = appendULEB(dst, uint64(len(s)))
	return append(dst, s...)
}

type fakeCompiler struct {
	id      string
	bytes   []byte
	imports []string
	log     string
	err     error
}

func (f *fakeCompiler) ToolchainID() string { return f.id }

func (f *fakeCompiler) Compile(ctx context.Context, req asset.CompileRequest) (asset.CompileResult, error) {
	if err := ctx.Err(); err != nil {
		return asset.CompileResult{}, err
	}
	if f.err != nil {
		return asset.CompileResult{}, f.err
	}
	return asset.CompileResult{
		Bytes:   append([]byte(nil), f.bytes...),
		Imports: append([]string(nil), f.imports...),
		Log:     f.log,
	}, nil
}

// ─── 装配 ──────────────────────────────────────────────────────

func newServer(t *testing.T, compiler asset.Compiler) (*httptest.Server, *asset.Service) {
	t.Helper()
	n := 0
	svc, err := asset.New(asset.Options{
		Store:    asset.NewMemory(),
		Compiler: compiler,
		IDGen: func() string {
			n++
			return fmt.Sprintf("st_%04d", n)
		},
		Now: func() time.Time { return time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("asset.New: %v", err)
	}
	api, err := New(svc)
	if err != nil {
		t.Fatalf("httpapi.New: %v", err)
	}
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	return srv, svc
}

type response struct {
	Status int
	Header http.Header
	Body   []byte
}

func call(t *testing.T, srv *httptest.Server, method, path string, body any, headers map[string]string) response {
	t.Helper()
	var rdr io.Reader
	switch v := body.(type) {
	case nil:
	case string:
		rdr = strings.NewReader(v)
	case []byte:
		rdr = bytes.NewReader(v)
	default:
		raw, err := json.Marshal(v)
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
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return response{Status: resp.StatusCode, Header: resp.Header.Clone(), Body: data}
}

func wantStatus(t *testing.T, got response, want int) {
	t.Helper()
	if got.Status != want {
		t.Fatalf("status = %d, want %d (body: %s)", got.Status, want, got.Body)
	}
}

func decodeObject(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode object: %v (body: %s)", err, body)
	}
	return out
}

func decodeList(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode list: %v (body: %s)", err, body)
	}
	return out
}

func wantErrorCode(t *testing.T, got response, status int, wantCode string) {
	t.Helper()
	wantStatus(t, got, status)
	body := decodeObject(t, got.Body)
	if body["code"] != wantCode {
		t.Fatalf("code = %v, want %q (body: %s)", body["code"], wantCode, got.Body)
	}
	if msg, _ := body["error"].(string); msg == "" {
		t.Fatalf("error message is empty (body: %s)", got.Body)
	}
}

// createDefinition 走 POST /api/strategies 并返回 id。
func createDefinition(t *testing.T, srv *httptest.Server, body map[string]any) string {
	t.Helper()
	resp := call(t, srv, http.MethodPost, "/api/strategies", body, nil)
	wantStatus(t, resp, http.StatusCreated)
	out := decodeObject(t, resp.Body)
	id, _ := out["id"].(string)
	if id == "" {
		t.Fatalf("create returned no id: %s", resp.Body)
	}
	return id
}

func createWasm(t *testing.T, srv *httptest.Server, abi, scope string) string {
	t.Helper()
	return createDefinition(t, srv, map[string]any{
		"carrier":     "wasm",
		"abi_version": abi,
		"scope":       scope,
		"entry":       "OnBar",
		"capability": map[string]any{
			"frequencies": []string{"1d"},
			"markets":     []string{"A"},
			"features":    []string{"momentum"},
		},
	})
}

func putSource(t *testing.T, srv *httptest.Server, id, source string) string {
	t.Helper()
	resp := call(t, srv, http.MethodPut, "/api/strategies/"+id+"/source",
		map[string]string{"source": source}, nil)
	wantStatus(t, resp, http.StatusOK)
	hash, _ := decodeObject(t, resp.Body)["version_hash"].(string)
	if len(hash) != 64 {
		t.Fatalf("version_hash = %q, want 64 hex", hash)
	}
	return hash
}

func registerBody(id, versionHash string, wasm []byte, imports []string, toolchain string) map[string]any {
	return map[string]any{
		"strategy_id":        id,
		"version_hash":       versionHash,
		"artifact_hash":      asset.ArtifactHash(wasm),
		"carrier":            "wasm",
		"abi_version":        "on_bar/2",
		"toolchain":          toolchain,
		"size_bytes":         len(wasm),
		"memory_limit_bytes": 1 << 20,
		"imports":            imports,
		"bytes_base64":       base64.StdEncoding.EncodeToString(wasm),
	}
}

func waitJob(t *testing.T, svc *asset.Service, jobID string) asset.BuildJob {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := svc.Job(context.Background(), jobID)
		if err != nil {
			t.Fatalf("Job(%s): %v", jobID, err)
		}
		if job.State == asset.JobSucceeded || job.State == asset.JobFailed {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s did not finish", jobID)
	return asset.BuildJob{}
}

// ─── 主链路 ────────────────────────────────────────────────────

func TestAPI_WasmLifecycle(t *testing.T) {
	wasm := wasmWithImports("env.get_bar", "env.emit_target2")
	srv, svc := newServer(t, &fakeCompiler{id: "tinygo-test", bytes: wasm})

	id := createWasm(t, srv, "on_bar/2", "market")
	version := putSource(t, srv, id, "package strategy\nfunc OnBar() {}")

	// 只有源码：W7 给一行 status=source，且空列表必须是 []。
	resp := call(t, srv, http.MethodGet, "/api/strategies/"+id+"/versions", nil, nil)
	wantStatus(t, resp, http.StatusOK)
	rows := decodeList(t, resp.Body)
	if len(rows) != 1 || rows[0]["status"] != "source" || rows[0]["version_hash"] != version {
		t.Fatalf("versions after source = %s", resp.Body)
	}
	if _, ok := rows[0]["artifact_hash"]; ok {
		t.Fatalf("source row must not carry an artifact_hash: %s", resp.Body)
	}

	empty := createWasm(t, srv, "on_bar/2", "symbol")
	resp = call(t, srv, http.MethodGet, "/api/strategies/"+empty+"/versions", nil, nil)
	wantStatus(t, resp, http.StatusOK)
	if got := strings.TrimSpace(string(resp.Body)); got != "[]" {
		t.Fatalf("empty versions = %s, want []", got)
	}

	// build → 异步作业 → built 行。
	resp = call(t, srv, http.MethodPost, "/api/strategies/"+id+"/build",
		map[string]string{"version_hash": version, "toolchain": "tinygo-test"}, nil)
	wantStatus(t, resp, http.StatusAccepted)
	jobID, _ := decodeObject(t, resp.Body)["job_id"].(string)
	job := waitJob(t, svc, jobID)
	if job.State != asset.JobSucceeded {
		t.Fatalf("job state = %s (%s)", job.State, job.Error)
	}
	wantHash := asset.ArtifactHash(wasm)
	if job.ArtifactHash != wantHash {
		t.Fatalf("job artifact_hash = %s, want %s", job.ArtifactHash, wantHash)
	}

	// job_id 查询端点：build 完成后仍可查（落库，不是进程内 registry）。
	resp = call(t, srv, http.MethodGet, "/api/strategies/"+id+"/builds/"+jobID, nil, nil)
	wantStatus(t, resp, http.StatusOK)
	jobBody := decodeObject(t, resp.Body)
	if jobBody["job_id"] != jobID || jobBody["state"] != asset.JobSucceeded ||
		jobBody["artifact_hash"] != wantHash {
		t.Fatalf("job endpoint = %s", resp.Body)
	}
	wantErrorCode(t, call(t, srv, http.MethodGet, "/api/strategies/st_other/builds/"+jobID, nil, nil),
		http.StatusNotFound, "job_not_found")
	wantErrorCode(t, call(t, srv, http.MethodGet, "/api/strategies/"+id+"/builds/st_missing", nil, nil),
		http.StatusNotFound, "job_not_found")

	resp = call(t, srv, http.MethodGet, "/api/strategies/"+id+"/versions", nil, nil)
	wantStatus(t, resp, http.StatusOK)
	rows = decodeList(t, resp.Body)
	if len(rows) != 1 || rows[0]["status"] != "built" ||
		rows[0]["artifact_hash"] != wantHash || rows[0]["toolchain"] != "tinygo-test" {
		t.Fatalf("versions after build = %s", resp.Body)
	}

	// validate → promote。
	resp = call(t, srv, http.MethodPost, "/api/strategies/"+id+"/versions/"+version+"/validate", nil, nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeObject(t, resp.Body)["status"]; got != "validated" {
		t.Fatalf("validate status = %v", got)
	}
	resp = call(t, srv, http.MethodPost, "/api/strategies/"+id+"/versions/"+version+"/promote", nil, nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeObject(t, resp.Body)["status"]; got != "promoted" {
		t.Fatalf("promote status = %v", got)
	}

	// resolve latest：元数据必须带 scope/capability/imports（引擎侧要据此挂载）。
	resp = call(t, srv, http.MethodGet, "/api/strategies/"+id+"/resolve?ref=latest", nil, nil)
	wantStatus(t, resp, http.StatusOK)
	resolved := decodeObject(t, resp.Body)
	for key, want := range map[string]any{
		"strategy_id":   id,
		"version_hash":  version,
		"artifact_hash": wantHash,
		"carrier":       "wasm",
		"abi_version":   "on_bar/2",
		"scope":         "market",
		"size_bytes":    float64(len(wasm)),
		"toolchain":     "tinygo-test",
	} {
		if resolved[key] != want {
			t.Fatalf("resolve.%s = %v, want %v (body: %s)", key, resolved[key], want, resp.Body)
		}
	}
	imports, _ := resolved["imports"].([]any)
	if len(imports) != 2 || imports[0] != "env.emit_target2" || imports[1] != "env.get_bar" {
		t.Fatalf("resolve.imports = %v, want the wasm import section", resolved["imports"])
	}
	capability, _ := resolved["capability"].(map[string]any)
	if freqs, _ := capability["frequencies"].([]any); len(freqs) != 1 || freqs[0] != "1d" {
		t.Fatalf("resolve.capability = %v", resolved["capability"])
	}

	// 下载：字节、类型、X-Artifact-Hash、ETag 与缓存策略。
	resp = call(t, srv, http.MethodGet, "/api/strategies/"+id+"/versions/"+version+"/artifact", nil, nil)
	wantStatus(t, resp, http.StatusOK)
	if !bytes.Equal(resp.Body, wasm) {
		t.Fatalf("artifact bytes differ: got %d bytes, want %d", len(resp.Body), len(wasm))
	}
	if got := resp.Header.Get("Content-Type"); got != "application/wasm" {
		t.Fatalf("Content-Type = %q, want application/wasm", got)
	}
	if got := resp.Header.Get("X-Artifact-Hash"); got != wantHash {
		t.Fatalf("X-Artifact-Hash = %q, want %q", got, wantHash)
	}
	if got := resp.Header.Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("Cache-Control = %q, want immutable", got)
	}
	etag := resp.Header.Get("ETag")
	if etag != `"`+wantHash+`"` {
		t.Fatalf("ETag = %q", etag)
	}

	// 条件请求：命中即 304 且无实体。
	resp = call(t, srv, http.MethodGet, "/api/strategies/"+id+"/versions/"+version+"/artifact",
		nil, map[string]string{"If-None-Match": etag})
	if resp.Status != http.StatusNotModified {
		t.Fatalf("conditional GET status = %d, want 304", resp.Status)
	}
	if len(resp.Body) != 0 {
		t.Fatalf("304 body must be empty, got %d bytes", len(resp.Body))
	}

	// 按 artifact hash / id@version 解析都回同一个 artifact_hash。
	for _, ref := range []string{wantHash, id + "@" + version} {
		resp = call(t, srv, http.MethodGet, "/api/strategies/"+id+"/resolve?ref="+ref, nil, nil)
		wantStatus(t, resp, http.StatusOK)
		if got := decodeObject(t, resp.Body)["artifact_hash"]; got != wantHash {
			t.Fatalf("resolve ref=%s artifact_hash = %v", ref, got)
		}
	}

	// deprecate：latest 断供，历史 hash 仍可解析。
	resp = call(t, srv, http.MethodPost, "/api/strategies/"+id+"/versions/"+version+"/deprecate", nil, nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeObject(t, resp.Body)["status"]; got != "deprecated" {
		t.Fatalf("deprecate status = %v", got)
	}
	wantErrorCode(t, call(t, srv, http.MethodGet, "/api/strategies/"+id+"/resolve", nil, nil),
		http.StatusNotFound, "no_active_version")
	resp = call(t, srv, http.MethodGet, "/api/strategies/"+id+"/resolve?ref="+wantHash, nil, nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeObject(t, resp.Body)["artifact_hash"]; got != wantHash {
		t.Fatalf("deprecated artifact must stay resolvable by hash, got %v", got)
	}
}

// ─── 内部登记端点 ──────────────────────────────────────────────

func TestAPI_RegisterArtifact(t *testing.T) {
	srv, _ := newServer(t, nil)
	id := createWasm(t, srv, "on_bar/2", "market")
	version := putSource(t, srv, id, "package strategy")
	wasm := wasmWithImports("env.get_bar", "env.emit_target2")
	hash := asset.ArtifactHash(wasm)

	// 声明 imports 为空 ⇒ 由字节里的 import section 回填。
	resp := call(t, srv, http.MethodPost, "/internal/strategies/"+id+"/artifacts",
		registerBody(id, version, wasm, nil, "tc-a"), nil)
	wantStatus(t, resp, http.StatusCreated)
	body := decodeObject(t, resp.Body)
	if body["status"] != "built" || body["artifact_hash"] != hash {
		t.Fatalf("register response = %s", resp.Body)
	}
	imports, _ := body["imports"].([]any)
	if len(imports) != 2 || imports[0] != "env.emit_target2" {
		t.Fatalf("derived imports = %v", body["imports"])
	}
	if body["size_bytes"] != float64(len(wasm)) {
		t.Fatalf("size_bytes = %v, want %d", body["size_bytes"], len(wasm))
	}

	// 同字节同工具链幂等。
	resp = call(t, srv, http.MethodPost, "/internal/strategies/"+id+"/artifacts",
		registerBody(id, version, wasm, nil, "tc-a"), nil)
	wantStatus(t, resp, http.StatusCreated)

	// 另一套工具链产出**不同字节** ⇒ 第二行；不带 toolchain 下载即歧义。
	// 同字节不同 toolchain 不会成为两行：主键是 artifact_hash，内容寻址优先。
	wasmB := wasmWithImports("env.get_bar", "env.get_state")
	resp = call(t, srv, http.MethodPost, "/internal/strategies/"+id+"/artifacts",
		registerBody(id, version, wasmB, nil, "tc-b"), nil)
	wantStatus(t, resp, http.StatusCreated)
	wantErrorCode(t, call(t, srv, http.MethodGet,
		"/api/strategies/"+id+"/versions/"+version+"/artifact", nil, nil),
		http.StatusConflict, "ambiguous_artifact")
	resp = call(t, srv, http.MethodGet,
		"/api/strategies/"+id+"/versions/"+version+"/artifact?toolchain=tc-b", nil, nil)
	wantStatus(t, resp, http.StatusOK)
	if !bytes.Equal(resp.Body, wasmB) {
		t.Fatalf("artifact bytes differ")
	}

	// 指定 toolchain 的 validate / promote 只作用于那一行。
	resp = call(t, srv, http.MethodPost, "/api/strategies/"+id+"/versions/"+version+"/validate",
		map[string]string{"toolchain": "tc-a"}, nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeObject(t, resp.Body)["toolchain"]; got != "tc-a" {
		t.Fatalf("validate picked toolchain %v", got)
	}
	resp = call(t, srv, http.MethodPost, "/api/strategies/"+id+"/versions/"+version+"/promote",
		map[string]string{"toolchain": "tc-a"}, nil)
	wantStatus(t, resp, http.StatusOK)
	resp = call(t, srv, http.MethodGet, "/api/strategies/"+id+"/resolve", nil, nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeObject(t, resp.Body)["toolchain"]; got != "tc-a" {
		t.Fatalf("resolve picked toolchain %v, want tc-a", got)
	}
}

func TestAPI_RegisterArtifactFailsClosed(t *testing.T) {
	srv, _ := newServer(t, nil)
	id := createWasm(t, srv, "on_bar/2", "market")
	version := putSource(t, srv, id, "package strategy")
	wasm := wasmWithImports("env.get_bar")

	// path id 与 body id 不一致。
	mismatch := registerBody(id, version, wasm, nil, "tc")
	mismatch["strategy_id"] = "st_other"
	wantErrorCode(t, call(t, srv, http.MethodPost, "/internal/strategies/"+id+"/artifacts",
		mismatch, nil), http.StatusBadRequest, "invalid_request")

	badB64 := registerBody(id, version, wasm, nil, "tc")
	badB64["bytes_base64"] = "!!!not-base64!!!"
	wantErrorCode(t, call(t, srv, http.MethodPost, "/internal/strategies/"+id+"/artifacts",
		badB64, nil), http.StatusBadRequest, "invalid_request")

	// 声明的 artifact_hash 与实际字节不符。
	wrongHash := registerBody(id, version, wasm, nil, "tc")
	wrongHash["artifact_hash"] = strings.Repeat("a", 64)
	wantErrorCode(t, call(t, srv, http.MethodPost, "/internal/strategies/"+id+"/artifacts",
		wrongHash, nil), http.StatusConflict, "artifact_invalid")

	// 载体/abi 与版本行不一致（版本是 on_bar/2）。
	wrongABI := registerBody(id, version, wasm, nil, "tc")
	wrongABI["abi_version"] = "on_bar/1"
	wantErrorCode(t, call(t, srv, http.MethodPost, "/internal/strategies/"+id+"/artifacts",
		wrongABI, nil), http.StatusConflict, "artifact_invalid")

	// import 越界（不在 on_bar/* 冻结函数表里）。
	evil := wasmWithImports("env.os_exec")
	wantErrorCode(t, call(t, srv, http.MethodPost, "/internal/strategies/"+id+"/artifacts",
		registerBody(id, version, evil, nil, "tc"), nil),
		http.StatusConflict, "imports_not_allowed")

	// 元数据 imports 与字节不符（元数据在撒谎）。
	lying := registerBody(id, version, wasm, []string{"env.get_bar", "env.get_state"}, "tc")
	wantErrorCode(t, call(t, srv, http.MethodPost, "/internal/strategies/"+id+"/artifacts",
		lying, nil), http.StatusConflict, "artifact_invalid")

	// 同字节登记到另一个 toolchain：主键是 artifact_hash，不允许同一份字节
	// 被记成两条「不同工具链」的行（那会让「谁编译的」变成猜测）。
	resp := call(t, srv, http.MethodPost, "/internal/strategies/"+id+"/artifacts",
		registerBody(id, version, wasm, nil, "tc-first"), nil)
	wantStatus(t, resp, http.StatusCreated)
	wantErrorCode(t, call(t, srv, http.MethodPost, "/internal/strategies/"+id+"/artifacts",
		registerBody(id, version, wasm, nil, "tc-second"), nil),
		http.StatusConflict, "conflict")

	// 版本不存在。
	unknownVersion := registerBody(id, strings.Repeat("f", 64), wasm, nil, "tc")
	wantErrorCode(t, call(t, srv, http.MethodPost, "/internal/strategies/"+id+"/artifacts",
		unknownVersion, nil), http.StatusNotFound, "version_not_found")
}

// ─── 错误线 ────────────────────────────────────────────────────

func TestAPI_ErrorWireCodes(t *testing.T) {
	compiler := &fakeCompiler{id: "tinygo-test", bytes: wasmWithImports("env.get_bar")}
	srv, _ := newServer(t, compiler)

	wasmID := createWasm(t, srv, "on_bar/2", "market")
	wasmVersion := putSource(t, srv, wasmID, "package strategy")

	builtinID := createDefinition(t, srv, map[string]any{
		"carrier": "builtin", "scope": "symbol", "entry": "OnBar",
	})
	putSource(t, srv, builtinID, "package strategy")

	validRef := "other@" + strings.Repeat("a", 64)

	tests := []struct {
		name   string
		method string
		path   string
		body   any
		status int
		code   string
	}{
		{"unknown strategy versions", http.MethodGet, "/api/strategies/st_nope/versions", nil, 404, "strategy_not_found"},
		{"unknown strategy resolve", http.MethodGet, "/api/strategies/st_nope/resolve", nil, 404, "strategy_not_found"},
		{"no active version", http.MethodGet, "/api/strategies/" + wasmID + "/resolve", nil, 404, "no_active_version"},
		{"malformed ref", http.MethodGet, "/api/strategies/" + wasmID + "/resolve?ref=not-a-ref", nil, 400, "invalid_request"},
		{"ref id mismatch", http.MethodGet, "/api/strategies/" + wasmID + "/resolve?ref=" + validRef, nil, 400, "invalid_request"},
		{"build unknown version", http.MethodPost, "/api/strategies/" + wasmID + "/build",
			map[string]string{"version_hash": strings.Repeat("f", 64)}, 404, "version_not_found"},
		{"build builtin carrier", http.MethodPost, "/api/strategies/" + builtinID + "/build",
			map[string]string{}, 400, "unsupported_carrier"},
		{"build wrong toolchain", http.MethodPost, "/api/strategies/" + wasmID + "/build",
			map[string]string{"version_hash": wasmVersion, "toolchain": "other-tc"}, 400, "unsupported_toolchain"},
		{"validate without artifact", http.MethodPost,
			"/api/strategies/" + wasmID + "/versions/" + wasmVersion + "/validate", nil, 404, "artifact_not_found"},
		{"promote without artifact", http.MethodPost,
			"/api/strategies/" + wasmID + "/versions/" + wasmVersion + "/promote", nil, 404, "artifact_not_found"},
		{"download without artifact", http.MethodGet,
			"/api/strategies/" + wasmID + "/versions/" + wasmVersion + "/artifact", nil, 404, "artifact_not_found"},
		{"unknown field", http.MethodPost, "/api/strategies",
			map[string]any{"carrier": "wasm", "abi_version": "on_bar/2", "scope": "market", "entry": "OnBar", "bogus": 1},
			400, "invalid_request"},
		{"broken json", http.MethodPost, "/api/strategies", "{", 400, "invalid_request"},
		{"two objects", http.MethodPost, "/api/strategies", `{"carrier":"wasm"} {}`, 400, "invalid_request"},
		{"unknown carrier", http.MethodPost, "/api/strategies",
			map[string]any{"carrier": "elf", "scope": "market", "entry": "OnBar"}, 400, "unsupported_carrier"},
		{"unknown abi", http.MethodPost, "/api/strategies",
			map[string]any{"carrier": "wasm", "abi_version": "on_bar/9", "scope": "market", "entry": "OnBar"},
			400, "invalid_abi_version"},
		{"unknown scope", http.MethodPost, "/api/strategies",
			map[string]any{"carrier": "wasm", "abi_version": "on_bar/2", "scope": "galaxy", "entry": "OnBar"},
			400, "invalid_scope"},
		{"missing entry", http.MethodPost, "/api/strategies",
			map[string]any{"carrier": "wasm", "abi_version": "on_bar/2", "scope": "market"},
			400, "invalid_request"},
		{"blank source", http.MethodPut, "/api/strategies/" + wasmID + "/source",
			map[string]string{"source": "  "}, 400, "source_required"},
		{"source over limit", http.MethodPut, "/api/strategies/" + wasmID + "/source",
			map[string]string{"source": strings.Repeat("x", (1<<20)+1)}, 413, "payload_too_large"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantErrorCode(t, call(t, srv, tc.method, tc.path, tc.body, nil), tc.status, tc.code)
		})
	}

	// 路由层：方法不允许由 ServeMux 回 405。
	if resp := call(t, srv, http.MethodDelete, "/api/strategies", nil, nil); resp.Status != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE /api/strategies status = %d, want 405", resp.Status)
	}
}

func TestAPI_BuildWithoutCompilerIsUnavailable(t *testing.T) {
	srv, _ := newServer(t, nil)
	id := createWasm(t, srv, "on_bar/2", "market")
	version := putSource(t, srv, id, "package strategy")

	wantErrorCode(t, call(t, srv, http.MethodPost, "/api/strategies/"+id+"/build",
		map[string]string{"version_hash": version}, nil),
		http.StatusServiceUnavailable, "compile_unavailable")
}
