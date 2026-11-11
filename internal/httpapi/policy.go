package httpapi

import kithttp "github.com/ruoxizhnya/tellurion-kit/auth/httpadapter"

// servedPolicy 是本仓**加了鉴权闸**的路由表（SVC-STRAT-G1 裁决 ⑫；写侧收紧
// 见 STRAT-WRITE-AUTH-ROLLOUT，2026-11-12 裁决 A）。
//
// 八条全部只认 `service`：`/promote` 的调用方是 agent / 操作者（ADR-0004 裁决
// A），两条 `/internal/*` 是编译进程，5 条写端点（create / source / build /
// validate / deprecate）是 agent 的 copilot 登记链与 core 的 opt-in 用例。
// service token 是长期 access token 上的角色串，kit 只按 kind=access + 角色
// any-of 判定。
//
// 表外只剩 **5 条读端点**（versions / promotions / builds/{job} / resolve /
// artifact）：它们保持公开，以端口隔离（`127.0.0.1:8090`，只在集群内/回环）
// 为边界。读面不带写入能力，给读加闸只会让 core / UI 的读链路无故要 token。
//
// 新增/删除受保护端点必须同一批改文档与用例：`TestServedPolicyIsServiceOnly`
// 与 `TestReadRoutesRemainPublic` 是两侧的钉子，不许悄悄放开或悄悄收紧。
var servedPolicy = kithttp.MustNewPolicy(map[string]kithttp.Rule{
	"POST /api/strategies":                                {Roles: []string{"service"}},
	"PUT /api/strategies/{id}/source":                     {Roles: []string{"service"}},
	"POST /api/strategies/{id}/build":                     {Roles: []string{"service"}},
	"POST /api/strategies/{id}/versions/{hash}/validate":  {Roles: []string{"service"}},
	"POST /api/strategies/{id}/versions/{hash}/promote":   {Roles: []string{"service"}},
	"POST /api/strategies/{id}/versions/{hash}/deprecate": {Roles: []string{"service"}},
	"POST /internal/strategies/{id}/artifacts":            {Roles: []string{"service"}},
	"POST /internal/strategies/{id}/builds/{job_id}":      {Roles: []string{"service"}},
})

// ServedPolicyEntries 返回实际生效的策略表副本（守卫测试用）。
func ServedPolicyEntries() map[string]kithttp.Rule { return servedPolicy.Entries() }
