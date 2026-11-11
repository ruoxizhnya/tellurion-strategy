package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ruoxizhnya/tellurion-kit/auth"

	"tellurion-strategy/internal/asset"
)

// ─── 装配（G1：真鉴权 + 审计 + 可注入就绪）──────────────────────

func newAuthServer(t *testing.T, compiler asset.Compiler, opts ...Option) (*httptest.Server, *asset.Service, *auth.TokenService, *asset.MemoryPromotionLog) {
	t.Helper()
	n := 0
	svc, err := asset.New(asset.Options{
		Store:    asset.NewMemory(),
		Compiler: compiler,
		IDGen: func() string {
			n++
			return fmt.Sprintf("st_g1_%04d", n)
		},
		Now: func() time.Time { return time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("asset.New: %v", err)
	}
	tokens := auth.NewTokenService(auth.Config{JWTSecret: []byte("g1-test-secret")})
	log := asset.NewMemoryPromotionLog()
	all := append([]Option{
		WithAuthorizer(NewAuthorizer(tokens)),
		WithPromotionLog(log),
	}, opts...)
	api, err := New(svc, all...)
	if err != nil {
		t.Fatalf("httpapi.New: %v", err)
	}
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	return srv, svc, tokens, log
}

func serviceToken(t *testing.T, tokens *auth.TokenService) string {
	t.Helper()
	tok, err := tokens.IssueServiceToken("service:core", auth.ServiceRole, time.Hour)
	if err != nil {
		t.Fatalf("IssueServiceToken: %v", err)
	}
	return tok
}

func adminToken(t *testing.T, tokens *auth.TokenService) string {
	t.Helper()
	access, _, err := tokens.IssueTokens(1, "alice", "admin")
	if err != nil {
		t.Fatalf("IssueTokens: %v", err)
	}
	return access
}

func bearer(tok string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + tok}
}

// ─── 策略表与档位 ──────────────────────────────────────────────

func TestServedPolicyIsServiceOnly(t *testing.T) {
	entries := ServedPolicyEntries()
	want := []string{
		"POST /api/strategies",
		"PUT /api/strategies/{id}/source",
		"POST /api/strategies/{id}/build",
		"POST /api/strategies/{id}/versions/{hash}/validate",
		"POST /api/strategies/{id}/versions/{hash}/promote",
		"POST /api/strategies/{id}/versions/{hash}/deprecate",
		"POST /internal/strategies/{id}/artifacts",
		"POST /internal/strategies/{id}/builds/{job_id}",
	}
	if len(entries) != len(want) {
		t.Fatalf("policy has %d entries, want %d: %v", len(entries), len(want), entries)
	}
	for _, key := range want {
		rule, ok := entries[key]
		if !ok {
			t.Fatalf("policy is missing %q (entries: %v)", key, entries)
		}
		if rule.Public || rule.AnyAuthenticated {
			t.Fatalf("%q must be role-gated, got %+v", key, rule)
		}
		if len(rule.Roles) != 1 || rule.Roles[0] != auth.ServiceRole {
			t.Fatalf("%q roles = %v, want [service]", key, rule.Roles)
		}
	}
}

func TestProtectedRoutesRejectMissingToken(t *testing.T) {
	srv, _, _, _ := newAuthServer(t, nil)
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/strategies"},
		{http.MethodPut, "/api/strategies/st_x/source"},
		{http.MethodPost, "/api/strategies/st_x/build"},
		{http.MethodPost, "/api/strategies/st_x/versions/" + strings.Repeat("a", 64) + "/validate"},
		{http.MethodPost, "/api/strategies/st_x/versions/" + strings.Repeat("a", 64) + "/deprecate"},
		{http.MethodPost, "/api/strategies/st_x/versions/" + strings.Repeat("a", 64) + "/promote"},
		{http.MethodPost, "/internal/strategies/st_x/artifacts"},
		{http.MethodPost, "/internal/strategies/st_x/builds/job-1"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			wantErrorCode(t, call(t, srv, tc.method, tc.path, nil, nil),
				http.StatusUnauthorized, "unauthenticated")
		})
	}
}

func TestProtectedRoutesRejectNonServiceRole(t *testing.T) {
	srv, _, tokens, _ := newAuthServer(t, nil)
	h := bearer(adminToken(t, tokens))
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/strategies"},
		{http.MethodPut, "/api/strategies/st_x/source"},
		{http.MethodPost, "/api/strategies/st_x/build"},
		{http.MethodPost, "/api/strategies/st_x/versions/" + strings.Repeat("a", 64) + "/validate"},
		{http.MethodPost, "/api/strategies/st_x/versions/" + strings.Repeat("a", 64) + "/deprecate"},
		{http.MethodPost, "/api/strategies/st_x/versions/" + strings.Repeat("a", 64) + "/promote"},
		{http.MethodPost, "/internal/strategies/st_x/artifacts"},
		{http.MethodPost, "/internal/strategies/st_x/builds/job-1"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			wantErrorCode(t, call(t, srv, tc.method, tc.path, nil, h),
				http.StatusForbidden, "forbidden")
		})
	}
}

func TestProtectedRoutesAcceptServiceToken(t *testing.T) {
	srv, _, tokens, _ := newAuthServer(t, nil)
	h := bearer(serviceToken(t, tokens))
	// 三条都「过了闸」：响应是业务错误（400/404），不是 401/403。
	for _, tc := range []struct{ path string }{
		{"/api/strategies/st_x/versions/" + strings.Repeat("a", 64) + "/promote"},
		{"/internal/strategies/st_x/artifacts"},
		{"/internal/strategies/st_x/builds/job-1"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			resp := call(t, srv, http.MethodPost, tc.path, nil, h)
			if resp.Status == http.StatusUnauthorized || resp.Status == http.StatusForbidden {
				t.Fatalf("service token rejected: %d (body: %s)", resp.Status, resp.Body)
			}
		})
	}
}

// 5 条读端点在**启用鉴权**的进程里仍然不要求 token——它们靠端口隔离做边界。
// 一旦有人「顺手把读也一起闸了」，这条会红：读面不带写入能力，加闸只会让
// core / UI 的读链路无故 401。
func TestReadRoutesRemainPublic(t *testing.T) {
	srv, _, _, _ := newAuthServer(t, nil)
	for _, path := range []string{
		"/api/strategies/st_nope/versions",
		"/api/strategies/st_nope/promotions",
		"/api/strategies/st_nope/builds/job_nope",
		"/api/strategies/st_nope/resolve",
		"/api/strategies/st_nope/versions/" + strings.Repeat("a", 64) + "/artifact",
	} {
		resp := call(t, srv, http.MethodGet, path, nil, nil)
		if resp.Status == http.StatusUnauthorized || resp.Status == http.StatusForbidden {
			t.Fatalf("GET %s must stay public, got %d (body: %s)", path, resp.Status, resp.Body)
		}
	}
	// 并且请求真的进了 handler（业务错误），不是被闸挡下。
	wantErrorCode(t, call(t, srv, http.MethodGet, "/api/strategies/st_nope/versions", nil, nil),
		http.StatusNotFound, "strategy_not_found")
}

// 写侧判据之二：坏 token（不是合法 JWT）在 5 条写端点上一律 401。
func TestWriteRoutesRejectBadToken(t *testing.T) {
	srv, _, _, _ := newAuthServer(t, nil)
	for _, tc := range writeRoutes() {
		t.Run(tc.path, func(t *testing.T) {
			wantErrorCode(t, call(t, srv, tc.method, tc.path, nil,
				map[string]string{"Authorization": "Bearer not-a-jwt"}),
				http.StatusUnauthorized, "unauthenticated")
		})
	}
}

// 写侧判据之三：service token 下 create / source / build / validate / deprecate
// 各一次真成功——闸门只认身份，不改业务语义。
func TestWriteRoutesAcceptServiceToken(t *testing.T) {
	compiler := &fakeCompiler{id: "tinygo-test", bytes: wasmWithImports("env.get_bar")}
	srv, svc, tokens, _ := newAuthServer(t, compiler)
	h := bearer(serviceToken(t, tokens))

	id := createWasm(t, srv, h, "on_bar/2", "market")
	version := putSource(t, srv, h, id, "package strategy")

	resp := call(t, srv, http.MethodPost, "/api/strategies/"+id+"/build",
		map[string]string{"version_hash": version, "toolchain": "tinygo-test"}, h)
	wantStatus(t, resp, http.StatusAccepted)
	jobID, _ := decodeObject(t, resp.Body)["job_id"].(string)
	if jobID == "" {
		t.Fatalf("build returned no job_id: %s", resp.Body)
	}
	if job := waitJob(t, svc, jobID); job.State != asset.JobSucceeded {
		t.Fatalf("build job state = %s (%s)", job.State, job.Error)
	}

	resp = call(t, srv, http.MethodPost, "/api/strategies/"+id+"/versions/"+version+"/validate", nil, h)
	wantStatus(t, resp, http.StatusOK)

	// deprecate 的前置是 promoted（状态机），所以中间补一次 promote。
	promoteH := bearer(serviceToken(t, tokens))
	promoteH["X-Actor"] = "alice"
	resp = call(t, srv, http.MethodPost, "/api/strategies/"+id+"/versions/"+version+"/promote", nil, promoteH)
	wantStatus(t, resp, http.StatusOK)

	resp = call(t, srv, http.MethodPost, "/api/strategies/"+id+"/versions/"+version+"/deprecate", nil, h)
	wantStatus(t, resp, http.StatusOK)
}

// writeRoutes 是 5 条写端点的表驱动清单（判据共用，避免各处漏一条）。
func writeRoutes() []struct{ method, path string } {
	hash := strings.Repeat("a", 64)
	return []struct{ method, path string }{
		{http.MethodPost, "/api/strategies"},
		{http.MethodPut, "/api/strategies/st_x/source"},
		{http.MethodPost, "/api/strategies/st_x/build"},
		{http.MethodPost, "/api/strategies/st_x/versions/" + hash + "/validate"},
		{http.MethodPost, "/api/strategies/st_x/versions/" + hash + "/deprecate"},
	}
}

// ─── promote 的审批人与审计 ─────────────────────────────────────

func TestPromoteRequiresActorAndWritesAudit(t *testing.T) {
	compiler := &fakeCompiler{id: "tinygo-test", bytes: wasmWithImports("env.get_bar")}
	srv, _, tokens, audit := newAuthServer(t, compiler)
	svcHeader := bearer(serviceToken(t, tokens))

	id := createWasm(t, srv, svcHeader, "on_bar/2", "market")
	version := putSource(t, srv, svcHeader, id, "package strategy")
	resp := call(t, srv, http.MethodPost, "/internal/strategies/"+id+"/artifacts",
		registerBody(id, version, compiler.bytes, compiler.imports, compiler.id), svcHeader)
	wantStatus(t, resp, http.StatusCreated)
	artifactHash, _ := decodeObject(t, resp.Body)["artifact_hash"].(string)
	if artifactHash == "" {
		t.Fatalf("artifact registration returned no artifact_hash: %s", resp.Body)
	}
	wantStatus(t, call(t, srv, http.MethodPost,
		"/api/strategies/"+id+"/versions/"+version+"/validate", nil, svcHeader), http.StatusOK)

	promotePath := "/api/strategies/" + id + "/versions/" + version + "/promote"

	// X-Actor 缺失：400 且不写审计行（顺序也在断言里：先闸后审计）。
	wantErrorCode(t, call(t, srv, http.MethodPost, promotePath, nil, svcHeader),
		http.StatusBadRequest, "actor_required")
	if got := len(audit.Events()); got != 0 {
		t.Fatalf("audit rows after actor_required = %d, want 0", got)
	}

	headers := bearer(serviceToken(t, tokens))
	headers["X-Actor"] = "alice"
	resp = call(t, srv, http.MethodPost, promotePath, nil, headers)
	wantStatus(t, resp, http.StatusOK)

	events := audit.Events()
	if len(events) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(events))
	}
	ev := events[0]
	if ev.Actor != "alice" {
		t.Fatalf("audit actor = %q, want alice", ev.Actor)
	}
	if ev.StrategyID != id || ev.VersionHash != version || ev.ArtifactHash != artifactHash {
		t.Fatalf("audit event = %+v, want strategy=%s version=%s artifact=%s", ev, id, version, artifactHash)
	}
	if ev.Outcome != "promoted" {
		t.Fatalf("audit outcome = %q, want promoted", ev.Outcome)
	}
	if ev.DecidedAt.IsZero() {
		t.Fatal("audit decided_at is zero")
	}

	// ── K11 读面：resolve 回写审批态 + 审计可读（谁/何时/结论）──
	resp = call(t, srv, http.MethodGet, "/api/strategies/"+id+"/resolve?ref=latest", nil, nil)
	wantStatus(t, resp, http.StatusOK)
	resolved := decodeObject(t, resp.Body)
	if got, _ := resolved["status"].(string); got != string(asset.StatusPromoted) {
		t.Fatalf("resolve status = %q, want promoted (body: %s)", got, resp.Body)
	}
	if got, _ := resolved["artifact_hash"].(string); got != artifactHash {
		t.Fatalf("resolve artifact_hash = %q, want %s", got, artifactHash)
	}

	resp = call(t, srv, http.MethodGet, "/api/strategies/"+id+"/promotions?version_hash="+version, nil, nil)
	wantStatus(t, resp, http.StatusOK)
	rows := decodeList(t, resp.Body)
	if len(rows) != 1 {
		t.Fatalf("audit read rows = %d, want 1 (body: %s)", len(rows), resp.Body)
	}
	if rows[0]["actor"] != "alice" || rows[0]["outcome"] != "promoted" {
		t.Fatalf("audit read row = %+v, want actor=alice outcome=promoted", rows[0])
	}
	if got, _ := rows[0]["version_hash"].(string); got != version {
		t.Fatalf("audit read version_hash = %q, want %s", got, version)
	}

	// 没有审批行的策略回空切片（不是 null，也不是「猜一个」）。
	resp = call(t, srv, http.MethodGet, "/api/strategies/st_none/promotions", nil, nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeList(t, resp.Body); len(got) != 0 {
		t.Fatalf("audit read for unknown strategy = %v, want []", got)
	}
	// 不匹配的版本 hash 回空切片（精确匹配，不是前缀/模糊匹配）。
	resp = call(t, srv, http.MethodGet,
		"/api/strategies/"+id+"/promotions?version_hash="+strings.Repeat("0", 64), nil, nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeList(t, resp.Body); len(got) != 0 {
		t.Fatalf("audit read for non-matching version = %v, want []", got)
	}
}

// ─── 探针 ──────────────────────────────────────────────────────

// 审计读面未装配时必须 503：读不到 ≠ 没有审批行。
func TestListPromotionsWithoutAuditStoreIsUnavailable(t *testing.T) {
	svc, err := asset.New(asset.Options{
		Store:    asset.NewMemory(),
		Compiler: &fakeCompiler{id: "tinygo-test", bytes: wasmWithImports("env.get_bar")},
		IDGen:    func() string { return "st_no_audit" },
		Now:      func() time.Time { return time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("asset.New: %v", err)
	}
	api, err := New(svc, WithAuthorizer(NewAuthorizer(auth.NewTokenService(auth.Config{}))))
	if err != nil {
		t.Fatalf("httpapi.New: %v", err)
	}
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	wantErrorCode(t, call(t, srv, http.MethodGet, "/api/strategies/st_no_audit/promotions", nil, nil),
		http.StatusServiceUnavailable, "audit_unavailable")
}

func TestProbesArePublicAndReadinessIsReal(t *testing.T) {
	// 启用鉴权的进程里，两个探针也不需要 token。
	srv, _, _, _ := newAuthServer(t, nil)
	resp := call(t, srv, http.MethodGet, "/healthz", nil, nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeObject(t, resp.Body)["status"]; got != "ok" {
		t.Fatalf("/healthz status = %v", got)
	}

	// 没接库 = not_ready（内存 Store 不能算业务就绪）。
	resp = call(t, srv, http.MethodGet, "/readyz", nil, nil)
	wantStatus(t, resp, http.StatusServiceUnavailable)
	if got := decodeObject(t, resp.Body)["store"]; got != "unwired" {
		t.Fatalf("/readyz store = %v, want unwired", got)
	}

	// 注入就绪检查：绿 → 200；红 → 503 且原因为库侧。
	okSrv, _, _, _ := newAuthServer(t, nil, WithReadiness(func(context.Context) error { return nil }))
	resp = call(t, okSrv, http.MethodGet, "/readyz", nil, nil)
	wantStatus(t, resp, http.StatusOK)
	if got := decodeObject(t, resp.Body)["status"]; got != "ready" {
		t.Fatalf("/readyz status = %v", got)
	}

	badSrv, _, _, _ := newAuthServer(t, nil, WithReadiness(func(context.Context) error {
		return errors.New("postgres ping: connection refused")
	}))
	resp = call(t, badSrv, http.MethodGet, "/readyz", nil, nil)
	wantStatus(t, resp, http.StatusServiceUnavailable)
	body := decodeObject(t, resp.Body)
	if body["store"] != "postgres" || !strings.Contains(fmt.Sprint(body["error"]), "postgres ping") {
		t.Fatalf("/readyz body = %v, want postgres probe failure", body)
	}
}

// ─── 限流（401 不被 429 顶替）──────────────────────────────────

func TestRateLimitKeepsAuthFirst(t *testing.T) {
	srv, _, tokens, _ := newAuthServer(t, nil, WithRateLimit(1, 2))
	promotePath := "/api/strategies/st_x/versions/" + strings.Repeat("a", 64) + "/promote"

	// 无 token + 桶已耗尽：仍然必须是 401（鉴权先于限流）。
	h := bearer(serviceToken(t, tokens))
	for i := 0; i < 2; i++ {
		resp := call(t, srv, http.MethodPost, promotePath, nil, h)
		if resp.Status == http.StatusUnauthorized || resp.Status == http.StatusForbidden {
			t.Fatalf("service token rejected: %d", resp.Status)
		}
	}
	wantErrorCode(t, call(t, srv, http.MethodPost, promotePath, nil, nil),
		http.StatusUnauthorized, "unauthenticated")

	// 同一个身份继续打：429 + Retry-After，而不是 401。
	resp := call(t, srv, http.MethodPost, promotePath, nil, h)
	wantErrorCode(t, resp, http.StatusTooManyRequests, "rate_limited")
	if got := resp.Header.Get("Retry-After"); got == "" {
		t.Fatal("429 without Retry-After")
	}
}

// ─── 分页 / build 幂等键 ───────────────────────────────────────

func TestListVersionsPagination(t *testing.T) {
	srv, _, tokens, _ := newAuthServer(t, nil)
	h := bearer(serviceToken(t, tokens))
	id := createWasm(t, srv, h, "on_bar/2", "market")
	for _, src := range []string{"package strategy // one", "package strategy // two", "package strategy // three"} {
		putSource(t, srv, h, id, src)
	}
	base := "/api/strategies/" + id + "/versions"

	if got := len(decodeList(t, call(t, srv, http.MethodGet, base, nil, nil).Body)); got != 3 {
		t.Fatalf("default page size = %d, want 3", got)
	}
	if got := len(decodeList(t, call(t, srv, http.MethodGet, base+"?limit=1", nil, nil).Body)); got != 1 {
		t.Fatalf("limit=1 returned %d rows", got)
	}
	page := decodeList(t, call(t, srv, http.MethodGet, base+"?limit=1&offset=2", nil, nil).Body)
	if len(page) != 1 {
		t.Fatalf("offset=2&limit=1 returned %d rows", len(page))
	}
	if got := len(decodeList(t, call(t, srv, http.MethodGet, base+"?offset=99", nil, nil).Body)); got != 0 {
		t.Fatalf("offset beyond end returned %d rows", got)
	}
	for _, query := range []string{"?limit=0", "?limit=1001", "?limit=x", "?offset=-1"} {
		wantErrorCode(t, call(t, srv, http.MethodGet, base+query, nil, nil),
			http.StatusBadRequest, "invalid_request")
	}
}

// blockingCompiler 让第一个 job 停在 running，用来观察「第二次 build 不排新 job」。
type blockingCompiler struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	calls   int
}

func (c *blockingCompiler) ToolchainID() string { return "tinygo-test" }

func (c *blockingCompiler) Compile(ctx context.Context, req asset.CompileRequest) (asset.CompileResult, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	c.once.Do(func() { close(c.started) })
	select {
	case <-c.release:
	case <-ctx.Done():
		return asset.CompileResult{}, ctx.Err()
	}
	return asset.CompileResult{Bytes: []byte(req.Source), Imports: nil}, nil
}

func (c *blockingCompiler) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func TestSubmitBuildIsIdempotentWhileActive(t *testing.T) {
	compiler := &blockingCompiler{started: make(chan struct{}), release: make(chan struct{})}
	srv, _, tokens, _ := newAuthServer(t, compiler)
	h := bearer(serviceToken(t, tokens))
	id := createWasm(t, srv, h, "on_bar/2", "market")
	version := putSource(t, srv, h, id, "package strategy")

	body := map[string]string{"version_hash": version}
	first := call(t, srv, http.MethodPost, "/api/strategies/"+id+"/build", body, h)
	wantStatus(t, first, http.StatusAccepted)
	firstJob, _ := decodeObject(t, first.Body)["job_id"].(string)
	if firstJob == "" {
		t.Fatalf("first build returned no job_id: %s", first.Body)
	}
	<-compiler.started

	second := call(t, srv, http.MethodPost, "/api/strategies/"+id+"/build", body, h)
	wantStatus(t, second, http.StatusAccepted)
	secondJob, _ := decodeObject(t, second.Body)["job_id"].(string)
	if secondJob != firstJob {
		t.Fatalf("second build job_id = %q, want reuse of %q", secondJob, firstJob)
	}
	if got := compiler.count(); got != 1 {
		t.Fatalf("compiler invoked %d times while a job was active, want 1", got)
	}

	close(compiler.release)
}
