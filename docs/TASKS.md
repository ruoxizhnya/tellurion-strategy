---
status: active
last-verified: 2026-11-11
verified-by: 2026-11-11 全面审查 W7 台账立单（A5 授权）——新立 3 条六字段单（`STRAT-GRACEFUL-SHUTDOWN` / `STRAT-AUTH-ADR` / `STRAT-READYZ-PROBE`），证据坐标引 ODR-072 / ODR-069（`tellurion-docs:docs/archive/odr/`）；其中 `STRAT-READYZ-PROBE` 系 W5-F6 归属订正（原 ODR 误记 analytics，实测 `nc -z 8090` 属本仓）；本轮只立单不修复。此前 SVC-STRAT-G3 收口（2026-11-10：主任裁决 A，按已落边界重写验收——内置策略走本地 registry 不联网、非空 `strategy_ref` 走本仓 resolve + 审批闸门、promote 由 agent/操作者直连本仓、旧 DB 注册表已删；原 ①/⑤ 作废，详见 ADR-0004）；此前 SVC-STRAT-G3 复核（2026-11-10：原文 ①/⑤ 与 core 本地 registry 裁决冲突，曾维持 ⛔ 并升级为待主任裁决）；SVC-STRAT-G3-B 读面收口（2026-11-10：`ResolveResult` 增 `status` + `GET /api/strategies/{id}/promotions` 审计读端点；Memory/PG 双腿全绿、两条破坏验证真做真还原）；SVC-STRAT-G4 真回写面停机 + 卡死 job 人工收口演练（2026-11-10，真 PG 16.15 + 真服务 `:18090` + 替身编译进程：非零退出自动 failed / 无终态回写留 running / 重启后仍 running / 内部端点收口 200 且同终态幂等、回退 running 409；库与临时件已清，0 代码改动）；SVC-STRAT-G4 PG 腿（2026-11-10，`tellurion_strategy_test` 全绿含迁移重放指纹 `10f6ee39…2954`）；QT-41c 收口 QT-41b 余项——编译进程状态回写内部端点 + `internal/compilerclient` + `cmd/assetservice` 外部编译器装配
---

# TASKS — tellurion-strategy 未完成项

> **归档说明（2026-11-11）**：本文件已瘦身为「只装未做的事」。2026-11-10 及以前已收口（✅）的任务原文、施工记录、验收输出与破坏验证，已逐字移入 [`docs/archive/tasks-archive-2026-11.md`](archive/tasks-archive-2026-11.md)；该文件顶部有「检索索引」表（ID / 标题 / 收口日期 / commit / 证据链接）。检索：`grep -n "<任务ID或commit>" docs/archive/tasks-archive-2026-11.md`。
>
> 状态：`⬜ 待做` / `🔶 进行中` / `⛔ 阻塞（有未解除前置）` / `⏸ 挂起（外部依赖或已裁决暂不实施；**不计入开放项**，重开须另立单并满足该条「重开条件」）` / `✅ 完成（附日期，随即移入归档）`

## 一、未做 / 挂起 / 外部依赖

### STRAT-AUTH-ADR · 鉴权姿态无 ADR，10 条公开端点的边界决定只散在 api-schema [P2] ⬜

- **① 目标**：给本仓的鉴权姿态补一条 ADR，把「10 条读端点公开 + 端口隔离、3 条写端点 service-token、`DecideStartup` 启动闸、promote 额外 `X-Actor`」这套**决定**固化成可追溯的架构决策，而不是只留在接口文档的行为描述里。
- **② 上下文（2026-11-11 全面审查 W2-F4，证据 ODR-069 `tellurion-docs:docs/archive/odr/odr-069-audit-wave2-core-architecture.md`）**：`docs/api-schema.md:78-95` 已完整描述鉴权行为（service token = kit `IssueServiceToken`、无/坏 token→401、角色不对→403、10 条公开端点靠 `127.0.0.1:8090` 端口隔离、`DecideStartup` 拒「漏配 secret 又非回环」、限流 50rps/突发200、promote 要 `X-Actor` 否则 400 `actor_required`）。但 `docs/adr/` 只有 `0001`~`0004`，**无一条是鉴权姿态 ADR**（`0004-core消费边界与promote归属` 只裁 promote 归属，不覆盖整体鉴权边界）。对照 risk/data/analytics 均有鉴权 ADR（ADR-0005/0006 族），本仓缺这一条。
- **③ 要求**：① 新增 `docs/adr/0005-鉴权姿态与端口隔离边界.md`（编号续 0004），记录：为何读端点公开（单人自托管 + 回环/集群内端口隔离）、为何写端点只认 `service`、`DecideStartup` 的 fail-closed 判据、promote 的 `X-Actor` 与审计；② 显式 cross-ref `ADR-0004`（promote 归属）与 kit `auth/httpadapter`；③ `docs/api-schema.md` 鉴权节加一行指回该 ADR；④ `docs/adr/README.md` 索引登记。
- **④ 约束与非目标**：**不改任何鉴权代码与策略表**（本单纯文档/决策固化）；不新增端点；不放宽现有 fail-closed；不把 api-schema 的行为表整段抄进 ADR（ADR 写「为什么这么定」，行为表留在 api-schema）。
- **⑤ 验收**：ADR-0005 存在且 Status=Accepted、含 Context/Decision/Consequences；api-schema 鉴权节有指回链接；`docs/adr/README.md` 索引含 0005；`python ../tellurion-docs/tools/check_doc_links.py`（或本仓等价链接巡检）活跃层 0 断链。
- **⑥ 边界**：本仓 `docs/adr/` 与 `docs/api-schema.md` 鉴权节；鉴权实现归 kit，跨仓服务间角色口径归 core/各服务 ADR。
- **owner**：待定

### STRAT-READYZ-PROBE · 健康探针是 TCP `nc -z`，端口开着就算健康（活≠就绪）[P3] ⬜

- **① 目标**：把本仓的健康探针从「TCP 端口可连」升级为「HTTP 端点真应答」，让容器在 HTTP 服务未起 / 迁移未完成时不被误判为健康。
- **② 上下文（2026-11-11 全面审查 W5-F6，证据 ODR-072 `tellurion-docs:docs/archive/odr/odr-072-audit-wave5-security-reliability-deps.md`；**归属订正**：ODR-072 W5-F6 原把此探针误记为 analytics，实测 `nc -z 8090` 属本仓 strategy，analytics compose 已是 HTTP `/healthz`，详见该 ODR 末尾订正注记）**：本仓 `Dockerfile:40-41` `HEALTHCHECK ... CMD nc -z 127.0.0.1 8090 || exit 1`、`deploy/compose.yaml:28-29` `test: ["CMD","nc","-z","127.0.0.1","8090"]` 都是**纯 TCP 探测**——端口一开就 healthy，即使 HTTP 路由未注册或 PG/迁移未就绪。本仓已有 `GET /healthz`（进程存活）与 `GET /readyz`（PG + 迁移，SVC-STRAT-G1）两条 HTTP 探针可用。core 统一 `docker-compose.yml:480-481` 镜像了同一 `nc -z` 定义（跨仓指针见下）。
- **③ 要求**：① `Dockerfile` 与 `deploy/compose.yaml` 的 healthcheck 改为 HTTP 探测（基线 `curl -fsS http://127.0.0.1:8090/healthz`，与 risk/analytics/orchestration/broker 同口径；若镜像无 curl 用 wget 等价）；② 是否进一步用 `/readyz`（含 PG/迁移）由施工者按「PG  down 时是否希望容器被重启」权衡并在单内记录决定；③ 确认运行镜像含探测所需二进制（curl/wget），缺则在 Dockerfile 补。
- **④ 约束与非目标**：不改 `/healthz` `/readyz` 端点本身的语义；不改端口；不引入新依赖库；core 统一 compose 的同步行归 core（跨仓指针），本单只改本仓两份自证资产。
- **⑤ 验收**：`docker build` 后起容器，HTTP 服务未就绪时 healthcheck 不报 healthy（破坏验证 = 把探针改回 `nc -z` ⇒ 端口开即 healthy 的假绿复现，还原复绿）；`grep -n "nc -z" Dockerfile deploy/compose.yaml` 归零。
- **⑥ 边界**：本仓 `Dockerfile` + `deploy/compose.yaml`；core 统一 `docker-compose.yml:480-481` 的 strategy healthcheck 同步归 `tellurion:docs/TASKS.md`（另立或并入 compose 巡检单）。
- **owner**：待定

**其余无未做 / 挂起 / 外部依赖项**（截至 2026-11-11 台账重整 + 全面审查 W7 立单）。已收口单的正文与施工记录全部在归档文件里；新增工作一律另立单。

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
