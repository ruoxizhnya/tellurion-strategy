---
status: active
last-verified: 2026-11-12
verified-by: 2026-11-12 跨仓伞三腿——`X-DEP-BUMP-CONSUMERS`（kit `v0.2.0 → v0.5.0`、`go` 1.26.11、pgx 随 MVS 到 `v5.11.0`，CI 增 `govulncheck` 门，降版破坏验证复红、还原复绿）、`X-AGENTS-DEPENDENCY-TABLE`（`AGENTS.md` §1/§4 按实际 `go.mod` 订正 contracts v0.10.0 / kit v0.5.0 / pgx v5.11.0）、`X-CI-RACE-STATICCHECK`（CI 补 `-race` 与 `staticcheck`；存量 1 条 `fixture.register` 测试死字段清理；`-race` 用 Docker golang:1.26 全量跑通 EXIT=0）；2026-11-12 第二轮补鉴权第 3 态（无 secret + 显式 `AUTH_INSECURE_EXPOSURE=loopback-published` ⇒ 任何绑定放开写面，启动 WARN 点名写面无鉴权 + 实际绑定地址；回环未声明仍 503，非回环未声明拒启），ADR-0005 / api-schema / runbook / README / AGENTS 已同步，三态实测矩阵与破坏验证通过；`STRAT-COMPOSE-AUTH-INJECT` 必要性改为条件性（仅统一栈注入 `JWT_SECRET` 时才需要 token）。2026-11-12 收口 `STRAT-WRITE-AUTH-ROLLOUT` 跨仓两步走——agent 侧带 token 能力先发布（`tellurion-agent@a59631e`），随后本仓把 5 条资产写端点并入 `servedPolicy`（`policy.go:20-29`，路由改 `requireAuth`），四判据 + 破坏验证通过；读端点仍公开（`TestReadRoutesRemainPublic` 钉住）；正文与施工记录已移入归档「续档（2026-11-12 收口）」。同时按实测立 `STRAT-COMPOSE-AUTH-INJECT`（⏸ 等 core compose 批次）：统一栈 ai-service 段缺 `STRATEGY_ASSET_URL` / `STRATEGY_ASSET_TOKEN`，strategy 段 `JWT_SECRET` 已就位（`tellurion/docker-compose.yml:474`）——原单「strategy 段补 JWT_SECRET」按实测订正。2026-11-12 裁决 A 第一步——订正「10 条公开端点」措辞为 **5 读 + 5 写**（ADR-0005 / api-schema / README / AGENTS / ARCHITECTURE / doc.go / policy.go 注释），并立跨仓单 `STRAT-WRITE-AUTH-ROLLOUT`。2026-11-12 收口 W7 三单（`STRAT-GRACEFUL-SHUTDOWN` 优雅停机 + 有界 drain；`STRAT-AUTH-ADR` 新增 ADR-0005 并订正「10 条读端点」措辞为 5 读 5 写；`STRAT-READYZ-PROBE` 探针改 HTTP `/readyz`）——正文与施工记录已移入归档「续档（2026-11-12 收口）」。2026-11-11 全面审查 W7 台账立单（A5 授权）——新立 3 条六字段单（`STRAT-GRACEFUL-SHUTDOWN` / `STRAT-AUTH-ADR` / `STRAT-READYZ-PROBE`），证据坐标引 ODR-072 / ODR-069（`tellurion-docs:docs/archive/odr/`）；其中 `STRAT-READYZ-PROBE` 系 W5-F6 归属订正（原 ODR 误记 analytics，实测 `nc -z 8090` 属本仓）；该轮只立单不修复。此前 SVC-STRAT-G3 收口（2026-11-10：主任裁决 A，按已落边界重写验收——内置策略走本地 registry 不联网、非空 `strategy_ref` 走本仓 resolve + 审批闸门、promote 由 agent/操作者直连本仓、旧 DB 注册表已删；原 ①/⑤ 作废，详见 ADR-0004）；此前 SVC-STRAT-G3 复核（2026-11-10：原文 ①/⑤ 与 core 本地 registry 裁决冲突，曾维持 ⛔ 并升级为待主任裁决）；SVC-STRAT-G3-B 读面收口（2026-11-10：`ResolveResult` 增 `status` + `GET /api/strategies/{id}/promotions` 审计读端点；Memory/PG 双腿全绿、两条破坏验证真做真还原）；SVC-STRAT-G4 真回写面停机 + 卡死 job 人工收口演练（2026-11-10，真 PG 16.15 + 真服务 `:18090` + 替身编译进程：非零退出自动 failed / 无终态回写留 running / 重启后仍 running / 内部端点收口 200 且同终态幂等、回退 running 409；库与临时件已清，0 代码改动）；SVC-STRAT-G4 PG 腿（2026-11-10，`tellurion_strategy_test` 全绿含迁移重放指纹 `10f6ee39…2954`）；QT-41c 收口 QT-41b 余项——编译进程状态回写内部端点 + `internal/compilerclient` + `cmd/assetservice` 外部编译器装配
---

# TASKS — tellurion-strategy 未完成项

> **归档说明（2026-11-11）**：本文件已瘦身为「只装未做的事」。2026-11-10 及以前已收口（✅）的任务原文、施工记录、验收输出与破坏验证，已逐字移入 [`docs/archive/tasks-archive-2026-11.md`](archive/tasks-archive-2026-11.md)；该文件顶部有「检索索引」表（ID / 标题 / 收口日期 / commit / 证据链接）。检索：`grep -n "<任务ID或commit>" docs/archive/tasks-archive-2026-11.md`。
>
> 状态：`⬜ 待做` / `🔶 进行中` / `⛔ 阻塞（有未解除前置）` / `⏸ 挂起（外部依赖或已裁决暂不实施；**不计入开放项**，重开须另立单并满足该条「重开条件」）` / `✅ 完成（附日期，随即移入归档）`

## 一、未做 / 挂起 / 外部依赖

### STRAT-COMPOSE-AUTH-INJECT · 统一栈 ai-service 段补资产登记地址与 service token（等 core compose 批次）[P2] ⏸

> **状态说明**：⏸ 挂起——**等 core compose 批次一起落**（`tellurion/docker-compose.yml` 后面有 core 批次要改 compose 参数，两个执行者同时改会撞）。本单不可单独派发。**必要性已改为条件性（2026-11-12 第二轮三态补全后）**：统一栈默认无 `JWT_SECRET` 且 strategy 段声明 `AUTH_INSECURE_EXPOSURE=loopback-published`，命中第 3 态——写面无鉴权但可用，agent 登记链在注入前**不会** 401（此前「注入前会 401」的口径已作废）。只有当统一栈改为注入 `JWT_SECRET`（生产姿态）时，写面才回到全闸，本单的 `STRATEGY_ASSET_TOKEN` 注入才成为必需；届时按本单 ③ 同批落。

- **① 目标**：把 service token 与资产服务地址注入统一栈的 ai-service 段，让 agent 的 copilot 登记链在统一栈里真的能写策略资产；两段注入同批落地。
- **② 上下文（2026-11-12 实测）**：`tellurion/docker-compose.yml:303-336` 的 ai-service 段注入了 `DATA_SERVICE_URL` / `AI_SERVICE_FACTOR_API_URL` / `JWT_SECRET` 等，但**既无 `STRATEGY_ASSET_URL` 也无 `STRATEGY_ASSET_TOKEN`**——agent 侧 `config/ai-service.yaml` 的默认值 `http://127.0.0.1:8090` 在容器内指向自己，登记链必然失败。strategy 段 `:472-477` 已有 `JWT_SECRET: ${JWT_SECRET:-}` 与 `AUTH_INSECURE_EXPOSURE`，**无需再补**（原协同单「strategy 段补 JWT_SECRET」已不成立，按实测订正）。agent 自证 `deploy/compose.yaml` 已注入 `STRATEGY_ASSET_URL` + `STRATEGY_ASSET_TOKEN`（`tellurion-agent@a59631e`）。
- **③ 要求**：① ai-service 段补 `STRATEGY_ASSET_URL: ${STRATEGY_ASSET_URL:-http://host.docker.internal:8090}`（与 `STRATEGY_ASSET_RESOLVE_URL` 同口径，`:240`）与 `STRATEGY_ASSET_TOKEN: ${STRATEGY_ASSET_TOKEN:-}`；② token 与 strategy 段 `JWT_SECRET` **同源**：同一份 secret 签发/验签，compose 里不得出现第二个 secret；③ 注释写明「注入 `JWT_SECRET` 时空 token = 登记链 401，不是静默降级；无 secret + 第 3 态下则免 token」；④ 与 core compose 批次同批提交，避免与之冲突。
- **④ 约束与非目标**：不改 strategy 段（JWT_SECRET 已就位）；不改端点 / 角色 / 请求体；不在 compose 里塞真 token（只留 `${VAR:-}` 占位）；不动 `tools/issue_service_token.go`。
- **⑤ 验收**：`docker compose config -q` 通过；`docker compose --profile full config` 的 ai-service 段出现 `STRATEGY_ASSET_URL` / `STRATEGY_ASSET_TOKEN` 且 token 与 strategy 段 `JWT_SECRET` 同源；起栈后用 `tools/issue_service_token.go` 签发的 token 走一次 create → source（2xx），把 ai-service 段 token 清空后同一请求 401（真做真还原）。
- **⑥ 边界**：`tellurion/docker-compose.yml` 的 ai-service 段（`:303-336`）；跨仓归属 core compose 批次执行者。
- **owner**：待定（core compose 批次）

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
