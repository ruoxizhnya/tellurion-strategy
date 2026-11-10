// Package httpapi 把 asset.Service 暴露成 HTTP 面。
//
// 只用标准库 net/http：本仓的 wire code（no_active_version / artifact_invalid …）
// 是**领域码**，不是 kit httpserver 那套通用状态码派生（bad_request/not_found），
// 直接写自己的错误出口比在通用出口上再包一层更不容易漂移。
package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	contracts "github.com/ruoxizhnya/tellurion-contracts/engine"

	"tellurion-strategy/internal/asset"
)

// 请求体上限：源码 1 MiB + JSON 转义余量；内部登记含 base64 字节（8 MiB × 4/3）。
const (
	maxJSONBody        = 2 << 20
	maxRegisterBody    = 12 << 20
	codeInvalidRequest = "invalid_request"

	// actorHeader 是 core 鉴权后转发的受信头（裁决 ⑫ 面 B1）。本仓只校验非空，
	// 审批人身份的真伪由 core 的入口闸保证。
	actorHeader = "X-Actor"

	// readyzTimeout 是就绪检查上界：探针是 1s 级的东西，库卡住必须明确报
	// not_ready，而不是把调用方挂在这里。
	readyzTimeout = 3 * time.Second

	// 版本列表分页（G1 ⑥）：默认 200、上界 1000。整段返回无上界等于让一个
	// 策略的版本数决定响应体大小。
	defaultVersionLimit = 200
	maxVersionLimit     = 1000
)

// HTTP 层新增的哨兵（不是领域码：它们描述的是闸门/传输，不是资产语义）。
var (
	errAuthUnavailable = errors.New("auth gate is not configured")
	errRateLimited     = errors.New("request rate exceeded")
	errActorRequired   = errors.New("actor is required")
	errAuditFailed     = errors.New("promotion audit failed")
	// errAuditUnavailable 表示审计读面没装配存储：读不到 ≠ 没有审批行，
	// 必须 503 而不是回一份空列表。
	errAuditUnavailable = errors.New("promotion audit is not configured")
)

// API 是 10 个公开端点 + 2 个内部端点 + 2 个探针的路由器。
type API struct {
	svc          *asset.Service
	mux          *http.ServeMux
	authorizer   Authorizer
	limiter      *rateLimiter
	readiness    func(context.Context) error
	promotionLog asset.PromotionLog
}

// Option 按需装配鉴权闸、限流、就绪检查与 promote 审计。
type Option func(*API)

// WithReadiness 注入就绪检查（G1 ③）：`/readyz` = PG 池可连 + 本仓迁移已执行。
// 不注入 ⇒ 503（内存 Store 的进程就是没接库，不应被读成就绪）。
func WithReadiness(check func(context.Context) error) Option {
	return func(a *API) { a.readiness = check }
}

// WithPromotionLog 注入 promote 审计。`cmd/assetservice` 两种存储形态都装配；
// 未装配时（单仓测试）promote 不写审计行。
func WithPromotionLog(l asset.PromotionLog) Option {
	return func(a *API) { a.promotionLog = l }
}

// WithRateLimit 覆盖默认限流（每秒 perSecond 个令牌、突发 burst）。
// perSecond <= 0 或 burst <= 0 ⇒ 关闭限流（仅测试用）。
func WithRateLimit(perSecond float64, burst int) Option {
	return func(a *API) {
		if perSecond <= 0 || burst <= 0 {
			a.limiter = nil
			return
		}
		a.limiter = newRateLimiter(perSecond, float64(burst))
	}
}

// New 装配路由。
func New(svc *asset.Service, opts ...Option) (*API, error) {
	if svc == nil {
		return nil, errors.New("httpapi: service is required")
	}
	a := &API{
		svc:     svc,
		mux:     http.NewServeMux(),
		limiter: newRateLimiter(defaultRatePerSecond, defaultRateBurst),
	}
	for _, opt := range opts {
		opt(a)
	}
	a.register()
	return a, nil
}

// register 挂路由。探针公开且不限流；`policy.go` 的三条走 requireAuth
// （鉴权先于限流）；其余公开端点只限流（端口隔离是它们的边界，见 policy.go）。
func (a *API) register() {
	// 公开探针（G1 ③）：探针不鉴权、不限流——被限流会被调用方读成未登录。
	a.mux.HandleFunc("GET /healthz", a.healthz)
	a.mux.HandleFunc("GET /readyz", a.readyz)

	// 受保护的三条（裁决 ⑫）。
	a.mux.HandleFunc("POST /api/strategies/{id}/versions/{hash}/promote", a.requireAuth(a.promote))
	a.mux.HandleFunc("POST /internal/strategies/{id}/artifacts", a.requireAuth(a.registerArtifact))
	a.mux.HandleFunc("POST /internal/strategies/{id}/builds/{job_id}", a.requireAuth(a.reportBuildJob))

	// 其余公开端点：靠端口隔离，不在策略表里。
	a.mux.HandleFunc("POST /api/strategies", a.openRoute(a.createDefinition))
	a.mux.HandleFunc("PUT /api/strategies/{id}/source", a.openRoute(a.putSource))
	a.mux.HandleFunc("POST /api/strategies/{id}/build", a.openRoute(a.submitBuild))
	a.mux.HandleFunc("POST /api/strategies/{id}/versions/{hash}/validate", a.openRoute(a.validateVersion))
	a.mux.HandleFunc("POST /api/strategies/{id}/versions/{hash}/deprecate", a.openRoute(a.deprecate))
	a.mux.HandleFunc("GET /api/strategies/{id}/versions", a.openRoute(a.listVersions))
	a.mux.HandleFunc("GET /api/strategies/{id}/promotions", a.openRoute(a.listPromotions))
	a.mux.HandleFunc("GET /api/strategies/{id}/builds/{job_id}", a.openRoute(a.getBuildJob))
	a.mux.HandleFunc("GET /api/strategies/{id}/resolve", a.openRoute(a.resolve))
	a.mux.HandleFunc("GET /api/strategies/{id}/versions/{hash}/artifact", a.openRoute(a.downloadArtifact))
}

// Handler 返回可挂到任意 http.Server 的路由。
func (a *API) Handler() http.Handler { return a.mux }

// ─── 探针 ──────────────────────────────────────────────────────

func (a *API) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyBody 是 `/readyz` 的报文：`store` 说明检查的是哪一档
// （`postgres` = 真库检查已注入；`unwired` = 没接库）。
type readyBody struct {
	Status string `json:"status"`
	Store  string `json:"store"`
	Error  string `json:"error,omitempty"`
}

// readyz 是就绪探针（G1 ③）：PG 池可连 + 本仓迁移已执行。
// 与 `/healthz` 的分工：后者只回答「进程活着」，库挂了它也 200。
func (a *API) readyz(w http.ResponseWriter, r *http.Request) {
	if a.readiness == nil {
		writeJSON(w, http.StatusServiceUnavailable, readyBody{
			Status: "not_ready",
			Store:  "unwired",
			Error:  "STRATEGY_DATABASE_URL is empty: the in-memory store is not ready for business",
		})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), readyzTimeout)
	defer cancel()
	if err := a.readiness(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, readyBody{
			Status: "not_ready",
			Store:  "postgres",
			Error:  err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, readyBody{Status: "ready", Store: "postgres"})
}

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

// promote 是唯一带审计的写端点（G1 ②）：审批人取 core 鉴权后转发的受信
// `X-Actor`，缺失即 400 actor_required 且**不写审计行**。
//
// 审计在 promote 成功之后追加（人 / 时间 / 结论）；写审计失败回 500
// `audit_failed`——promote 可能已经生效，重试是安全的，但「悄悄丢审计」
// 不是可接受的降级。
func (a *API) promote(w http.ResponseWriter, r *http.Request) {
	actor := strings.TrimSpace(r.Header.Get(actorHeader))
	if actor == "" {
		writeError(w, fmt.Errorf("%w: %s header is required", errActorRequired, actorHeader))
		return
	}
	var req toolchainRequest
	if !decodeOptionalJSON(w, r, maxJSONBody, &req) {
		return
	}
	artifact, err := a.svc.Promote(r.Context(), r.PathValue("id"), r.PathValue("hash"), req.Toolchain)
	if err != nil {
		writeError(w, err)
		return
	}
	if a.promotionLog != nil {
		ctx, cancel := context.WithTimeout(r.Context(), readyzTimeout)
		defer cancel()
		err := a.promotionLog.RecordPromotion(ctx, asset.PromotionEvent{
			StrategyID:   artifact.StrategyID,
			VersionHash:  artifact.VersionHash,
			ArtifactHash: artifact.ArtifactHash,
			Actor:        actor,
			Outcome:      string(artifact.Status),
			DecidedAt:    time.Now().UTC(),
		})
		if err != nil {
			writeError(w, fmt.Errorf("%w: %v", errAuditFailed, err))
			return
		}
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

// listVersions 是 W7：无版本返回空切片（不是 null），支持 limit / offset
// 分页（G1 ⑥）。上界见 maxVersionLimit。
func (a *API) listVersions(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parsePage(r)
	if err != nil {
		writeError(w, err)
		return
	}
	views, err := a.svc.ListVersions(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	// 空列表必须是 []：编码成 null 等于契约说谎（DATA-14 的教训）。
	if views == nil {
		views = []asset.VersionView{}
	}
	if offset > len(views) {
		offset = len(views)
	}
	end := offset + limit
	if end > len(views) {
		end = len(views)
	}
	writeJSON(w, http.StatusOK, views[offset:end])
}

// listPromotions 是 promote 审计的读面（K11 前置）：core 回测入口要能证明
// 「这个版本是谁批的、什么时候、结论是什么」，而不是只看一个布尔位。
//
// 按既有口径保持端口隔离（不引 service token，见 api-schema §6.1）；
// `?version_hash=` 是精确匹配（与写入侧同口径）。
func (a *API) listPromotions(w http.ResponseWriter, r *http.Request) {
	if a.promotionLog == nil {
		writeError(w, fmt.Errorf("%w: promotion audit is not configured", errAuditUnavailable))
		return
	}
	limit, offset, err := parsePage(r)
	if err != nil {
		writeError(w, err)
		return
	}
	events, err := a.promotionLog.ListPromotions(r.Context(), asset.PromotionFilter{
		StrategyID:  r.PathValue("id"),
		VersionHash: strings.TrimSpace(r.URL.Query().Get("version_hash")),
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	// 空列表必须是 []：编码成 null 会让调用方把「没有审批」读成「读面缺席」。
	if events == nil {
		events = []asset.PromotionEvent{}
	}
	writeJSON(w, http.StatusOK, events)
}

// parsePage 解析 `?limit=&offset=`：缺省 200 / 0，上界 1000；非法值 400。
func parsePage(r *http.Request) (int, int, error) {
	limit := defaultVersionLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > maxVersionLimit {
			return 0, 0, fmt.Errorf("%w: limit must be an integer in 1..%d", asset.ErrInvalidParameter, maxVersionLimit)
		}
		limit = n
	}
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return 0, 0, fmt.Errorf("%w: offset must be a non-negative integer", asset.ErrInvalidParameter)
		}
		offset = n
	}
	return limit, offset, nil
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

// reportBuildJob 是编译进程的状态回写端点：queued → running → succeeded | failed。
//
// 归属不符一律 404 job_not_found（不跨策略枚举）；非法转移 409 invalid_transition；
// succeeded 必须指向同一 (strategy, version, toolchain) 已登记的 artifact。
func (a *API) reportBuildJob(w http.ResponseWriter, r *http.Request) {
	var report asset.BuildJobReport
	if !decodeJSON(w, r, maxJSONBody, &report) {
		return
	}
	job, err := a.svc.ReportBuildJob(r.Context(), r.PathValue("id"), r.PathValue("job_id"), report)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
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
	// ── 闸门 / 传输（G1，不是资产语义）──
	case errors.Is(err, ErrUnauthenticated):
		return http.StatusUnauthorized, "unauthenticated"
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden, "forbidden"
	case errors.Is(err, errActorRequired):
		return http.StatusBadRequest, "actor_required"
	case errors.Is(err, errRateLimited):
		return http.StatusTooManyRequests, "rate_limited"
	case errors.Is(err, errAuthUnavailable):
		return http.StatusServiceUnavailable, "auth_unavailable"
	case errors.Is(err, errAuditFailed):
		return http.StatusInternalServerError, "audit_failed"
	case errors.Is(err, errAuditUnavailable):
		return http.StatusServiceUnavailable, "audit_unavailable"

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
