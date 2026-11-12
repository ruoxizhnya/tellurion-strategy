---
status: active
last-verified: 2026-11-12
verified-by: 2026-11-12 `X-LOW-CLEANUP` strategy 腿——`Dockerfile` 产物名由退役的 `strategy-service` 改为 `assetservice`，镜像实建 + 入口实跑通过；2026-11-12 跨仓伞三腿——`X-DEP-BUMP-CONSUMERS`（kit `v0.2.0 → v0.5.0`、`go` 1.26.11、pgx 随 MVS 到 `v5.11.0`，CI 增 `govulncheck` 门，降版破坏验证复红、还原复绿；同日补 Dockerfile 工具链到 `golang:1.26.11-alpine`）、`X-AGENTS-DEPENDENCY-TABLE`（`AGENTS.md` §1/§4 按实际 `go.mod` 订正 contracts v0.10.0 / kit v0.5.0 / pgx v5.11.0）、`X-CI-RACE-STATICCHECK`（CI 补 `-race` 与 `staticcheck`；存量 1 条 `fixture.register` 测试死字段清理；`-race` 用 Docker golang:1.26 全量跑通 EXIT=0）；2026-11-12 第二轮补鉴权第 3 态（无 secret + 显式 `AUTH_INSECURE_EXPOSURE=loopback-published` ⇒ 任何绑定放开写面，启动 WARN 点名写面无鉴权 + 实际绑定地址；回环未声明仍 503，非回环未声明拒启），ADR-0005 / api-schema / runbook / README / AGENTS 已同步，三态实测矩阵与破坏验证通过；`STRAT-COMPOSE-AUTH-INJECT` 必要性改为条件性（仅统一栈注入 `JWT_SECRET` 时才需要 token）。2026-11-12 收口 `STRAT-WRITE-AUTH-ROLLOUT` 跨仓两步走——agent 侧带 token 能力先发布（`tellurion-agent@a59631e`），随后本仓把 5 条资产写端点并入 `servedPolicy`（`policy.go:20-29`，路由改 `requireAuth`），四判据 + 破坏验证通过；读端点仍公开（`TestReadRoutesRemainPublic` 钉住）；正文与施工记录已移入归档「续档（2026-11-12 收口）」。同时按实测立 `STRAT-COMPOSE-AUTH-INJECT`（⏸ 等 core compose 批次）：统一栈 ai-service 段缺 `STRATEGY_ASSET_URL` / `STRATEGY_ASSET_TOKEN`，strategy 段 `JWT_SECRET` 已就位（`tellurion/docker-compose.yml:474`）——原单「strategy 段补 JWT_SECRET」按实测订正。2026-11-12 裁决 A 第一步——订正「10 条公开端点」措辞为 **5 读 + 5 写**（ADR-0005 / api-schema / README / AGENTS / ARCHITECTURE / doc.go / policy.go 注释），并立跨仓单 `STRAT-WRITE-AUTH-ROLLOUT`。2026-11-12 收口 W7 三单（`STRAT-GRACEFUL-SHUTDOWN` 优雅停机 + 有界 drain；`STRAT-AUTH-ADR` 新增 ADR-0005 并订正「10 条读端点」措辞为 5 读 5 写；`STRAT-READYZ-PROBE` 探针改 HTTP `/readyz`）——正文与施工记录已移入归档「续档（2026-11-12 收口）」。2026-11-11 全面审查 W7 台账立单（A5 授权）——新立 3 条六字段单（`STRAT-GRACEFUL-SHUTDOWN` / `STRAT-AUTH-ADR` / `STRAT-READYZ-PROBE`），证据坐标引 ODR-072 / ODR-069（`tellurion-docs:docs/archive/odr/`）；其中 `STRAT-READYZ-PROBE` 系 W5-F6 归属订正（原 ODR 误记 analytics，实测 `nc -z 8090` 属本仓）；该轮只立单不修复。此前 SVC-STRAT-G3 收口（2026-11-10：主任裁决 A，按已落边界重写验收——内置策略走本地 registry 不联网、非空 `strategy_ref` 走本仓 resolve + 审批闸门、promote 由 agent/操作者直连本仓、旧 DB 注册表已删；原 ①/⑤ 作废，详见 ADR-0004）；此前 SVC-STRAT-G3 复核（2026-11-10：原文 ①/⑤ 与 core 本地 registry 裁决冲突，曾维持 ⛔ 并升级为待主任裁决）；SVC-STRAT-G3-B 读面收口（2026-11-10：`ResolveResult` 增 `status` + `GET /api/strategies/{id}/promotions` 审计读端点；Memory/PG 双腿全绿、两条破坏验证真做真还原）；SVC-STRAT-G4 真回写面停机 + 卡死 job 人工收口演练（2026-11-10，真 PG 16.15 + 真服务 `:18090` + 替身编译进程：非零退出自动 failed / 无终态回写留 running / 重启后仍 running / 内部端点收口 200 且同终态幂等、回退 running 409；库与临时件已清，0 代码改动）；SVC-STRAT-G4 PG 腿（2026-11-10，`tellurion_strategy_test` 全绿含迁移重放指纹 `10f6ee39…2954`）；QT-41c 收口 QT-41b 余项——编译进程状态回写内部端点 + `internal/compilerclient` + `cmd/assetservice` 外部编译器装配；2026-11-12 收口 `STRAT-COMPOSE-AUTH-INJECT`（core `0892bbc`）：统一栈 ai-service 段补齐 `STRATEGY_ASSET_URL` / `STRATEGY_ASSET_TOKEN`（token 为空占位、条件必需），真起 ai-service 已确认读到变量且空 token 显式 warn 401 —— 原文与施工记录移入归档。
---

# TASKS — tellurion-strategy 未完成项

> **归档说明（2026-11-11）**：本文件已瘦身为「只装未做的事」。2026-11-10 及以前已收口（✅）的任务原文、施工记录、验收输出与破坏验证，已逐字移入 [`docs/archive/tasks-archive-2026-11.md`](archive/tasks-archive-2026-11.md)；该文件顶部有「检索索引」表（ID / 标题 / 收口日期 / commit / 证据链接）。检索：`grep -n "<任务ID或commit>" docs/archive/tasks-archive-2026-11.md`。
>
> 状态：`⬜ 待做` / `🔶 进行中` / `⛔ 阻塞（有未解除前置）` / `⏸ 挂起（外部依赖或已裁决暂不实施；**不计入开放项**，重开须另立单并满足该条「重开条件」）` / `✅ 完成（附日期，随即移入归档）`

## 一、未做 / 挂起 / 外部依赖

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
