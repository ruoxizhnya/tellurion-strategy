package httpapi

import kithttp "github.com/ruoxizhnya/tellurion-kit/auth/httpadapter"

// servedPolicy 是本仓**加了鉴权闸**的路由表（SVC-STRAT-G1，裁决 ⑫）。
//
// 只开三条：`/promote` 与两条 `/internal/*`。它们的调用方是 agent/操作者（直连本仓
// 的 promote，ADR-0004 裁决 A）与编译进程，所以档位只认 `service`——service token 是
// 长期 access token 上的角色串，kit 只按 kind=access + 角色 any-of 判定。
//
// 其余 10 条公开端点**刻意不在表里**：G1 的姿势是端口隔离（`127.0.0.1:8090`，
// 只在集群内/回环），不是给每个端点拍一次角色。表外的端点不挂鉴权闸；一旦把
// 某条公开端点挪进本表，`TestServedPolicyIsServiceOnly` 与守卫测试会一起红，
// 那是**预期**——新增受保护端点必须同时更新文档与用例，不许悄悄放开。
var servedPolicy = kithttp.MustNewPolicy(map[string]kithttp.Rule{
	"POST /api/strategies/{id}/versions/{hash}/promote": {Roles: []string{"service"}},
	"POST /internal/strategies/{id}/artifacts":          {Roles: []string{"service"}},
	"POST /internal/strategies/{id}/builds/{job_id}":    {Roles: []string{"service"}},
})

// ServedPolicyEntries 返回实际生效的策略表副本（守卫测试用）。
func ServedPolicyEntries() map[string]kithttp.Rule { return servedPolicy.Entries() }
