---
status: active
last-verified: 2026-11-12
verified-by: 2026-11-12 裁决 A 第一步——订正「10 条公开端点」措辞为 **5 读 + 5 写**（ADR-0005 / api-schema / README / AGENTS / ARCHITECTURE / doc.go / policy.go 注释），并立跨仓单 `STRAT-WRITE-AUTH-ROLLOUT`（⛔ 等 agent 仓空出，不可派发；`servedPolicy` / handler / agent 仓均未动）。2026-11-12 收口 W7 三单（`STRAT-GRACEFUL-SHUTDOWN` 优雅停机 + 有界 drain；`STRAT-AUTH-ADR` 新增 ADR-0005 并订正「10 条读端点」措辞为 5 读 5 写；`STRAT-READYZ-PROBE` 探针改 HTTP `/readyz`）——正文与施工记录已移入归档「续档（2026-11-12 收口）」。2026-11-11 全面审查 W7 台账立单（A5 授权）——新立 3 条六字段单（`STRAT-GRACEFUL-SHUTDOWN` / `STRAT-AUTH-ADR` / `STRAT-READYZ-PROBE`），证据坐标引 ODR-072 / ODR-069（`tellurion-docs:docs/archive/odr/`）；其中 `STRAT-READYZ-PROBE` 系 W5-F6 归属订正（原 ODR 误记 analytics，实测 `nc -z 8090` 属本仓）；该轮只立单不修复。此前 SVC-STRAT-G3 收口（2026-11-10：主任裁决 A，按已落边界重写验收——内置策略走本地 registry 不联网、非空 `strategy_ref` 走本仓 resolve + 审批闸门、promote 由 agent/操作者直连本仓、旧 DB 注册表已删；原 ①/⑤ 作废，详见 ADR-0004）；此前 SVC-STRAT-G3 复核（2026-11-10：原文 ①/⑤ 与 core 本地 registry 裁决冲突，曾维持 ⛔ 并升级为待主任裁决）；SVC-STRAT-G3-B 读面收口（2026-11-10：`ResolveResult` 增 `status` + `GET /api/strategies/{id}/promotions` 审计读端点；Memory/PG 双腿全绿、两条破坏验证真做真还原）；SVC-STRAT-G4 真回写面停机 + 卡死 job 人工收口演练（2026-11-10，真 PG 16.15 + 真服务 `:18090` + 替身编译进程：非零退出自动 failed / 无终态回写留 running / 重启后仍 running / 内部端点收口 200 且同终态幂等、回退 running 409；库与临时件已清，0 代码改动）；SVC-STRAT-G4 PG 腿（2026-11-10，`tellurion_strategy_test` 全绿含迁移重放指纹 `10f6ee39…2954`）；QT-41c 收口 QT-41b 余项——编译进程状态回写内部端点 + `internal/compilerclient` + `cmd/assetservice` 外部编译器装配
---

# TASKS — tellurion-strategy 未完成项

> **归档说明（2026-11-11）**：本文件已瘦身为「只装未做的事」。2026-11-10 及以前已收口（✅）的任务原文、施工记录、验收输出与破坏验证，已逐字移入 [`docs/archive/tasks-archive-2026-11.md`](archive/tasks-archive-2026-11.md)；该文件顶部有「检索索引」表（ID / 标题 / 收口日期 / commit / 证据链接）。检索：`grep -n "<任务ID或commit>" docs/archive/tasks-archive-2026-11.md`。
>
> 状态：`⬜ 待做` / `🔶 进行中` / `⛔ 阻塞（有未解除前置）` / `⏸ 挂起（外部依赖或已裁决暂不实施；**不计入开放项**，重开须另立单并满足该条「重开条件」）` / `✅ 完成（附日期，随即移入归档）`

## 一、未做 / 挂起 / 外部依赖

### STRAT-WRITE-AUTH-ROLLOUT · 5 条公开写端点收紧为 service token（跨仓两步走）[P1] ⛔

> **状态说明**：⛔ 阻塞——**等 agent 仓空出后再执行**（当前该仓有执行者在改 `cmd/ai/handlers_pipeline.go`、`pkg/aistore/adoption*` 等，跨仓窗口未开）。本单**不可立即派发**；本轮只完成第一步（文档口径订正 + 立本单），`servedPolicy` / handler / agent 仓一律未动。

- **① 目标**：把策略资产的 5 条写端点（create / source / build / validate / deprecate）从「公开、靠端口隔离」收紧为与本仓 `promote` / `/internal/*` 同一套 `role=service` 鉴权闸；跨仓两步走，中间不设断链窗口。
- **② 上下文（2026-11-12 裁决 A + 全仓实测测绘）**：
  - 本仓现状：`internal/httpapi/policy.go:19-23` 只挂 3 条；`internal/httpapi/api.go:112-133` 的 10 条公开端点里 **5 读 + 5 写**（写侧：`POST /api/strategies`、`PUT .../source`、`POST .../build`、`POST .../validate`、`POST .../deprecate`）。闸门语义：无 authorizer 时受保护路由一律 503 `auth_unavailable`（`internal/httpapi/auth.go:87-92`），不存在「没配 secret 就放行」。
  - 唯一生产调用方：agent `pkg/ai/client/strategy_asset_client.go`——`Register → createDefinition:166`、`putSource:185-189`、`submitBuild:205-209`，运行期服务间调用；`call():263-302` **只设 `Content-Type`，零 `Authorization`**，`StrategyAssetConfig:51-61` 无 token 字段。装配：`config/ai-service.yaml:89` 默认 `http://127.0.0.1:8090`，agent 自证 `deploy/compose.yaml:56` 注入 `STRATEGY_ASSET_URL`；统一栈 `tellurion/docker-compose.yml:311-336` 的 ai-service **连 `STRATEGY_ASSET_URL` 都没注入**，strategy 段在 `:463-486`——两段都无 token。
  - 零生产调用方：`validate` 仅 core opt-in 用例 `cmd/analysis/wiring/strategy_asset_live_test.go:203`；`deprecate` 仅本仓 `internal/httpapi/api_test.go:417`。UI 不直连 8090（`web/src/api/strategy.ts` 走 core 的 `VITE_API_BASE`），无前端改造。
  - 因此本单**不能在本仓单独完成**：一旦把 5 条并入策略表，agent 的 copilot 登记链会立刻 401/503（连「无 secret + 回环」的本地开发也断，因为受保护路由是 503 而不是放行）。
- **③ 要求**：
  1. **agent 侧（跨仓，先在 agent 仓执行）**：`StrategyAssetConfig` 增 token 字段；`call()` 在有 token 时发 `Authorization: Bearer <token>`，无 token 而 `BaseURL` 非空时 fail-closed（拒绝调用并报错，不得静默不带 token）；`STRATEGY_ASSET_TOKEN` 进 `config/ai-service.yaml` 与 agent 自证 `deploy/compose.yaml`；补客户端测试（有/无/错 token 三条）。
  2. **统一栈（core 仓）**：`docker-compose.yml` 的 **ai-service 段**补 `STRATEGY_ASSET_URL` + `STRATEGY_ASSET_TOKEN`，**strategy 段**补 `JWT_SECRET`（与 token 同源）——两段必须同批注入，不能只改一侧。
  3. **本仓（第二步）**：把 5 条写端点并入 `servedPolicy`，角色 `service`；不改 handler、不新增角色、不改路径。
  4. **发布顺序硬约束**：**agent 具备带 token 能力并发布在前，strategy 收紧在后**；中间不设断链窗口（agent 先上线时 token 可被公开期忽略，收紧后立即生效）。
  5. **本地直跑口径**：复用 kit `DecideStartup` 的回环 / `AUTH_INSECURE_EXPOSURE=loopback-published` 豁免——但它**只免除启动拒绝**，不会让受保护路由放行。收紧后本地起服务必须先 `go run tools/issue_service_token.go`（core 仓）签发 token，并注入 `JWT_SECRET` + `STRATEGY_ASSET_TOKEN`。**禁止**新增「没配 secret 就放行写端点」的后门。
- **④ 约束与非目标**：5 条读端点保持公开不动；不改端点路径 / 请求体 / 响应体；不新增角色；不在本单改 agent 仓代码（只作为前置依赖）；不把 `validate` / `deprecate` 拆出来单独先收紧（5 条同批，避免姿态割裂）；不做 per-endpoint RBAC。
- **⑤ 验收（四种判据 + 破坏验证）**：
  - **无 token** → 有 `JWT_SECRET` 时 401 `unauthenticated`；无 authorizer 时 503 `auth_unavailable`；**不得 2xx**。
  - **错 token**（坏签名 / refresh token / `role=user`）→ 401；角色不符 → 403。
  - **对 token**（`role=service`）→ 与今天等价的成功（create / source / build / validate / deprecate 各一条真调用）。
  - **破坏验证**：把 5 条从 `servedPolicy` 删除（回到公开）⇒ 上述「无 token 不得 2xx」判据必须变红；逐字节还原后复绿。
  - 另需：启动闸在无 secret + 非回环时仍拒起；`policy_test.go` 守卫用例同步更新（新增/删除条目必须改文档的断言）；`go build ./... && go vet ./... && go test ./... -count=1` 全绿；agent 侧客户端测试与统一栈 `docker compose config` 通过。
- **⑥ 边界**：本仓 `internal/httpapi/{policy.go,api.go,policy_test.go,g1_test.go}` 与 docs；agent 仓 `pkg/ai/client/strategy_asset_client*.go` + `config/ai-service.yaml` + `deploy/compose.yaml`；core 仓 `docker-compose.yml` 的 strategy / ai-service 两段与 `tools/issue_service_token.go`。**执行前置：agent 仓工作树干净、无其他执行者占用**；未满足不得开工。
- **owner**：待定（跨仓：strategy + agent + core）

## 二、跨仓指针（不在本仓，勿双写）

| 事项 | 去哪 |
|---|---|
| 执行侧闸门与 `strategy_ref` 消费 | `tellurion:docs/TASKS.md` |
| 版本输入与 hash 契约 | `tellurion-contracts:docs/TASKS.md` |
| 编排侧提交 | `tellurion-orchestration:docs/TASKS.md` |

## 维护本文件

- **新增单**：写全六字段；六字段里必须显式引用**冻结契约的路径与节号**（契约仓的包路径、设计文档的节号），不许写"参考相关实现"。
- **派工前先证伪前提**：读码核对工单写的前提；不符就停下回报 + 给拆法，不硬做、不扩范围。
- **跨仓引用写 `repo:path`**（如 `tellurion:docs/TASKS.md`），不做可点击相对链接。
- **顺序纪律**：同一波次内撞同一批文件的单按单号串行，一单一 commit。
