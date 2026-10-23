// Package httpapi 把 asset.Service 暴露成 HTTP 面。
//
// 只用标准库 net/http：本仓的 wire code（no_active_version / artifact_invalid …）
// 是**领域码**，不是 kit httpserver 那套通用状态码派生（bad_request/not_found），
// 直接写自己的错误出口比在通用出口上再包一层更不容易漂移。
package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	contracts "github.com/ruoxizhnya/tellurion-contracts/engine"

	"tellurion-strategy/internal/asset"
)

// 请求体上限：源码 1 MiB + JSON 转义余量；内部登记含 base64 字节（8 MiB × 4/3）。
const (
	maxJSONBody        = 2 << 20
	maxRegisterBody    = 12 << 20
	codeInvalidRequest = "invalid_request"
)

// API 是 9 个公开端点 + 1 个内部登记端点的路由器。
type API struct {
	svc *asset.Service
	mux *http.ServeMux
}

// New 装配路由。
func New(svc *asset.Service) (*API, error) {
	if svc == nil {
		return nil, errors.New("httpapi: service is required")
	}
	a := &API{svc: svc, mux: http.NewServeMux()}

	a.mux.HandleFunc("POST /api/strategies", a.createDefinition)
	a.mux.HandleFunc("PUT /api/strategies/{id}/source", a.putSource)
	a.mux.HandleFunc("POST /api/strategies/{id}/build", a.submitBuild)
	a.mux.HandleFunc("POST /api/strategies/{id}/versions/{hash}/validate", a.validateVersion)
	a.mux.HandleFunc("POST /api/strategies/{id}/versions/{hash}/promote", a.promote)
	a.mux.HandleFunc("POST /api/strategies/{id}/versions/{hash}/deprecate", a.deprecate)
	a.mux.HandleFunc("GET /api/strategies/{id}/versions", a.listVersions)
	a.mux.HandleFunc("GET /api/strategies/{id}/builds/{job_id}", a.getBuildJob)
	a.mux.HandleFunc("GET /api/strategies/{id}/resolve", a.resolve)
	a.mux.HandleFunc("GET /api/strategies/{id}/versions/{hash}/artifact", a.downloadArtifact)

	// 内部面：编译服务回填 artifact。只登记、不 promote（promote 是人的闸门）。
	a.mux.HandleFunc("POST /internal/strategies/{id}/artifacts", a.registerArtifact)
	return a, nil
}

// Handler 返回可挂到任意 http.Server 的路由。
func (a *API) Handler() http.Handler { return a.mux }

// ─── 写侧 ──────────────────────────────────────────────────────

type createDefinitionRequest struct {
	Carrier    string           `json:"carrier"`
	ABIVersion string           `json:"abi_version"`
	Scope      string           `json:"scope"`
	Capability asset.Capability `json:"capability"`
	Entry      string           `json:"entry"`
}

func (a *API) createDefinition(w http.ResponseWriter, r *http.Request) {
	var req createDefinitionRequest
	if !decodeJSON(w, r, maxJSONBody, &req) {
		return
	}
	def, err := a.svc.CreateDefinition(r.Context(), asset.CreateDefinitionRequest{
		Carrier:    asset.Carrier(req.Carrier),
		ABIVersion: req.ABIVersion,
		Scope:      contracts.StrategyScope(req.Scope),
		Capability: req.Capability,
		Entry:      req.Entry,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": def.ID})
}

type putSourceRequest struct {
	Source string `json:"source"`
}

func (a *API) putSource(w http.ResponseWriter, r *http.Request) {
	var req putSourceRequest
	if !decodeJSON(w, r, maxJSONBody, &req) {
		return
	}
	version, err := a.svc.PutSource(r.Context(), r.PathValue("id"), req.Source)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version_hash": version.VersionHash})
}

type buildRequest struct {
	VersionHash string `json:"version_hash"`
	Toolchain   string `json:"toolchain"`
}

func (a *API) submitBuild(w http.ResponseWriter, r *http.Request) {
	var req buildRequest
	if !decodeJSON(w, r, maxJSONBody, &req) {
		return
	}
	job, err := a.svc.SubmitBuild(r.Context(), r.PathValue("id"), req.VersionHash, req.Toolchain)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": job.ID})
}

type toolchainRequest struct {
	Toolchain string `json:"toolchain"`
}

func (a *API) validateVersion(w http.ResponseWriter, r *http.Request) {
	var req toolchainRequest
	if !decodeOptionalJSON(w, r, maxJSONBody, &req) {
		return
	}
	artifact, err := a.svc.ValidateVersion(r.Context(), r.PathValue("id"), r.PathValue("hash"), req.Toolchain)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, artifactView(artifact))
}

func (a *API) promote(w http.ResponseWriter, r *http.Request) {
	var req toolchainRequest
	if !decodeOptionalJSON(w, r, maxJSONBody, &req) {
		return
	}
	artifact, err := a.svc.Promote(r.Context(), r.PathValue("id"), r.PathValue("hash"), req.Toolchain)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, artifactView(artifact))
}

func (a *API) deprecate(w http.ResponseWriter, r *http.Request) {
	var req toolchainRequest
	if !decodeOptionalJSON(w, r, maxJSONBody, &req) {
		return
	}
	artifact, err := a.svc.Deprecate(r.Context(), r.PathValue("id"), r.PathValue("hash"), req.Toolchain)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, artifactView(artifact))
}

func (a *API) listVersions(w http.ResponseWriter, r *http.Request) {
	views, err := a.svc.ListVersions(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	// 空列表必须是 []：编码成 null 等于契约说谎（DATA-14 的教训）。
	if views == nil {
		views = []asset.VersionView{}
	}
	writeJSON(w, http.StatusOK, views)
}

// getBuildJob 是 build 的轮询端点：202 返回的 job_id 在进程重启后仍可查。
// 路径带 strategy id 且必须与作业归属一致，避免跨策略枚举 job_id。
func (a *API) getBuildJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("job_id")
	job, err := a.svc.Job(r.Context(), jobID)
	if err != nil {
		writeError(w, err)
		return
	}
	if job.StrategyID != r.PathValue("id") {
		writeError(w, fmt.Errorf("%w: %s", asset.ErrJobNotFound, jobID))
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// ─── 读侧 ──────────────────────────────────────────────────────

func (a *API) resolve(w http.ResponseWriter, r *http.Request) {
	result, err := a.svc.Resolve(r.Context(), r.PathValue("id"), r.URL.Query().Get("ref"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) downloadArtifact(w http.ResponseWriter, r *http.Request) {
	artifact, err := a.svc.ArtifactForVersion(r.Context(), r.PathValue("id"), r.PathValue("hash"),
		r.URL.Query().Get("toolchain"))
	if err != nil {
		writeError(w, err)
		return
	}
	etag := `"` + artifact.ArtifactHash + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("X-Artifact-Hash", artifact.ArtifactHash)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if match := r.Header.Get("If-None-Match"); match != "" && etagMatches(match, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	contentType := "application/octet-stream"
	if artifact.Carrier == asset.CarrierWASM {
		contentType = "application/wasm"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(artifact.Bytes)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(artifact.Bytes)
}

// ─── 内部登记 ──────────────────────────────────────────────────

type registerArtifactRequest struct {
	StrategyID       string   `json:"strategy_id"`
	VersionHash      string   `json:"version_hash"`
	ArtifactHash     string   `json:"artifact_hash"`
	Carrier          string   `json:"carrier"`
	ABIVersion       string   `json:"abi_version"`
	Toolchain        string   `json:"toolchain"`
	SizeBytes        int64    `json:"size_bytes"`
	MemoryLimitBytes int64    `json:"memory_limit_bytes"`
	Imports          []string `json:"imports"`
	CompileLog       string   `json:"compile_log"`
	BuiltBy          string   `json:"built_by"`
	BytesBase64      string   `json:"bytes_base64"`
}

func (a *API) registerArtifact(w http.ResponseWriter, r *http.Request) {
	var req registerArtifactRequest
	if !decodeJSON(w, r, maxRegisterBody, &req) {
		return
	}
	if req.StrategyID != r.PathValue("id") {
		writeError(w, fmt.Errorf("%w: strategy_id does not match path id", asset.ErrInvalidParameter))
		return
	}
	bytes, err := base64.StdEncoding.DecodeString(req.BytesBase64)
	if err != nil {
		writeError(w, fmt.Errorf("%w: bytes_base64: %v", asset.ErrInvalidParameter, err))
		return
	}
	artifact, err := a.svc.RegisterArtifact(r.Context(), asset.Artifact{
		StrategyID:       req.StrategyID,
		VersionHash:      req.VersionHash,
		ArtifactHash:     req.ArtifactHash,
		Carrier:          asset.Carrier(req.Carrier),
		ABIVersion:       req.ABIVersion,
		Toolchain:        req.Toolchain,
		SizeBytes:        req.SizeBytes,
		MemoryLimitBytes: req.MemoryLimitBytes,
		Imports:          req.Imports,
		Status:           asset.StatusBuilt,
		Storage:          asset.StorageInline,
		CompileLog:       req.CompileLog,
		BuiltBy:          req.BuiltBy,
		Bytes:            bytes,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, artifactView(artifact))
}

// ─── 编解码与错误出口 ──────────────────────────────────────────

func artifactView(a asset.Artifact) map[string]any {
	return map[string]any{
		"strategy_id":        a.StrategyID,
		"version_hash":       a.VersionHash,
		"artifact_hash":      a.ArtifactHash,
		"carrier":            a.Carrier,
		"abi_version":        a.ABIVersion,
		"toolchain":          a.Toolchain,
		"size_bytes":         a.SizeBytes,
		"memory_limit_bytes": a.MemoryLimitBytes,
		"imports":            a.Imports,
		"status":             a.Status,
	}
}

// decodeJSON 严格解码（未知字段即 400）：契约里没有的字段被静默忽略，等于
// 客户端以为生效了、服务端却没收到。
func decodeJSON(w http.ResponseWriter, r *http.Request, max int64, dst any) bool {
	return decode(w, r, max, dst, false)
}

func decodeOptionalJSON(w http.ResponseWriter, r *http.Request, max int64, dst any) bool {
	return decode(w, r, max, dst, true)
}

func decode(w http.ResponseWriter, r *http.Request, max int64, dst any, optional bool) bool {
	body := io.LimitReader(r.Body, max+1)
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if optional && errors.Is(err, io.EOF) {
			return true
		}
		writeError(w, fmt.Errorf("%w: %v", asset.ErrInvalidParameter, err))
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeError(w, fmt.Errorf("%w: body must contain a single JSON object", asset.ErrInvalidParameter))
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

type errorBody struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// writeError 是唯一的错误出口：4xx 原文可回（调用方要能看懂），5xx 收口成通用文案。
func writeError(w http.ResponseWriter, err error) {
	status, code := classify(err)
	msg := err.Error()
	if status >= http.StatusInternalServerError {
		msg = "服务器内部错误"
	}
	writeJSON(w, status, errorBody{Error: msg, Code: code})
}

func classify(err error) (int, string) {
	switch {
	case errors.Is(err, asset.ErrStrategyNotFound):
		return http.StatusNotFound, "strategy_not_found"
	case errors.Is(err, asset.ErrVersionNotFound):
		return http.StatusNotFound, "version_not_found"
	case errors.Is(err, asset.ErrNoActiveVersion):
		return http.StatusNotFound, "no_active_version"
	case errors.Is(err, asset.ErrArtifactNotFound):
		return http.StatusNotFound, "artifact_not_found"
	case errors.Is(err, asset.ErrJobNotFound):
		return http.StatusNotFound, "job_not_found"

	case errors.Is(err, asset.ErrStrategyExists):
		return http.StatusConflict, "strategy_exists"
	case errors.Is(err, asset.ErrArtifactConflict):
		return http.StatusConflict, "conflict"
	case errors.Is(err, asset.ErrAmbiguousArtifact):
		return http.StatusConflict, "ambiguous_artifact"
	case errors.Is(err, asset.ErrInvalidTransition):
		return http.StatusConflict, "invalid_transition"

	case errors.Is(err, contracts.ErrImportNotWhitelisted):
		return http.StatusConflict, "imports_not_allowed"
	case errors.Is(err, asset.ErrArtifactHashMismatch), errors.Is(err, asset.ErrInvalidArtifact):
		return http.StatusConflict, "artifact_invalid"
	case errors.Is(err, asset.ErrStorageUnsupported):
		return http.StatusConflict, "storage_unsupported"
	case errors.Is(err, asset.ErrCompileFailed):
		return http.StatusConflict, "compile_failed"

	case errors.Is(err, asset.ErrSourceTooLarge), errors.Is(err, asset.ErrArtifactTooLarge):
		return http.StatusRequestEntityTooLarge, "payload_too_large"

	case errors.Is(err, asset.ErrCompileUnavailable):
		return http.StatusServiceUnavailable, "compile_unavailable"

	case errors.Is(err, asset.ErrUnsupportedCarrier):
		return http.StatusBadRequest, "unsupported_carrier"
	case errors.Is(err, asset.ErrInvalidABIVersion):
		return http.StatusBadRequest, "invalid_abi_version"
	case errors.Is(err, asset.ErrInvalidScope):
		return http.StatusBadRequest, "invalid_scope"
	case errors.Is(err, asset.ErrSourceMissing):
		return http.StatusBadRequest, "source_required"
	case errors.Is(err, asset.ErrUnsupportedToolchain):
		return http.StatusBadRequest, "unsupported_toolchain"
	case errors.Is(err, asset.ErrInvalidParameter), errors.Is(err, asset.ErrInvalidDefinition):
		return http.StatusBadRequest, codeInvalidRequest

	default:
		return http.StatusInternalServerError, "internal"
	}
}

func etagMatches(header, etag string) bool {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "*" || part == etag || part == strings.TrimPrefix(etag, `"`) {
			return true
		}
	}
	return false
}
