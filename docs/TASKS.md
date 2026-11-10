---
status: active
last-verified: 2026-11-10
verified-by: SVC-STRAT-G3 收口（2026-11-10：主任裁决 A，按已落边界重写验收——内置策略走本地 registry 不联网、非空 `strategy_ref` 走本仓 resolve + 审批闸门、promote 由 agent/操作者直连本仓、旧 DB 注册表已删；原 ①/⑤ 作废，详见 ADR-0004）；此前 SVC-STRAT-G3 复核（2026-11-10：原文 ①/⑤ 与 core 本地 registry 裁决冲突，曾维持 ⛔ 并升级为待主任裁决）；SVC-STRAT-G3-B 读面收口（2026-11-10：`ResolveResult` 增 `status` + `GET /api/strategies/{id}/promotions` 审计读端点；Memory/PG 双腿全绿、两条破坏验证真做真还原）；SVC-STRAT-G4 真回写面停机 + 卡死 job 人工收口演练（2026-11-10，真 PG 16.15 + 真服务 `:18090` + 替身编译进程：非零退出自动 failed / 无终态回写留 running / 重启后仍 running / 内部端点收口 200 且同终态幂等、回退 running 409；库与临时件已清，0 代码改动）；SVC-STRAT-G4 PG 腿（2026-11-10，`tellurion_strategy_test` 全绿含迁移重放指纹 `10f6ee39…2954`）；QT-41c 收口 QT-41b 余项——编译进程状态回写内部端点 + `internal/compilerclient` + `cmd/assetservice` 外部编译器装配
---

# TASKS — tellurion-strategy 未完成项

> **归档说明（2026-11-11）**：本文件已瘦身为「只装未做的事」。2026-11-10 及以前已收口（✅）的任务原文、施工记录、验收输出与破坏验证，已逐字移入 [`docs/archive/tasks-archive-2026-11.md`](archive/tasks-archive-2026-11.md)；该文件顶部有「检索索引」表（ID / 标题 / 收口日期 / commit / 证据链接）。检索：`grep -n "<任务ID或commit>" docs/archive/tasks-archive-2026-11.md`。
>
> 状态：`⬜ 待做` / `🔶 进行中` / `⛔ 阻塞（有未解除前置）` / `⏸ 挂起（外部依赖或已裁决暂不实施；**不计入开放项**，重开须另立单并满足该条「重开条件」）` / `✅ 完成（附日期，随即移入归档）`

## 一、未做 / 挂起 / 外部依赖

**本仓当前无未做 / 挂起 / 外部依赖项**（截至 2026-11-11 台账重整）。已收口单的正文与施工记录全部在归档文件里。

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
