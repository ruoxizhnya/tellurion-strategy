---
status: active
last-verified: 2026-10-20
verified-by: 功能边界固化（三份契约：db / api / dataflow）
---

# DB Schema — tellurion-strategy

> **所有权声明**：本仓**拥有 `strategy` schema 的 DDL**：策略定义、版本、artifact 元数据与字节码。是这三张表的唯一写者。
> 上级依据：`tellurion:docs/design/qt-decoupling/data-service-contract-and-ownership.md` §1 —— **DDL 只有一个 owner**，同一个 PG 实例、每服务一个 schema，将来拆库是"按 schema 搬走"而不是重写。
> 本文件是本仓表结构的**唯一真相源**：代码里的建表语句与它不一致时，**改代码**；要改表结构，先改本文件（结构级变更还要立 ADR）。

## 1. 归属表

| 表 | 内容 | 现状 |
|---|---|---|
| `strategy.strategy_definitions` | 定义（原 `strategies` 升级）：`carrier` / `abi_version` / `scope` / `capability` / `entry` / `active_version_hash` | 改造 |
| `strategy.strategy_artifacts` | artifact 元数据：`version_hash` + `artifact_hash` / `size_bytes` / `memory_limit_bytes` / `imports` / `toolchain` / `status` / `storage` | 新建 |
| `strategy.strategy_artifact_blobs` | 字节码本体（本地栈 BYTEA，8 MiB 上限；`storage='url'` 时可为空） | 新建 |

约束：`UNIQUE(strategy_id, version_hash)`；`version_hash` 由**源码 canonical 化后 sha256**派生，内容改必须改 hash。

## 2. 只读输入（不属于本仓，禁止写入）

| 数据 | 真实 owner |
|---|---|
| 回测结果、指标 | core / `run` |
| 行情与因子值 | `tellurion-data` |
| 规则快照 | `tellurion-risk` |

本仓**不写**这些表；解析只产出"引擎要加载什么"，不产出信号。

## 3. 红线

1. **不算信号**：本仓只回答"这份 artifact 是什么"，执行与出信号在 core。
2. **promote 不覆盖 artifact 行**：历史回测必须能按旧 hash 复现。
3. **无 active 版本 = fail-closed**：解析失败绝不回退到"本地内置同名策略"。
4. **`scope` 必须参与 `version_hash`**：同源码不同 scope 行为不同。
5. 字节码不可信：体积 / 内存 / imports 三项校验不过 → 不 promote。

## 4. 迁移策略

- 迁移脚本落在 `migrations/`，命名 `<序号>_<动作>.sql`（全仓一个递增序列，不复用号）。
- **只前滚**：已合并的迁移文件不改；修错靠新增一条。
- 每条迁移必须可重入（`IF NOT EXISTS` / `IF EXISTS`），且能在空库上一次跑完。
- 跨 schema 取数只走**只读视图或 API**；出现 `join` 别人的表就是边界腐化的第一信号。
- 表结构变更的同步义务：本文件 + `docs/ARCHITECTURE.md` 的「数据归属」节必须同一次提交里更新。

## 5. 待定

- 对象存储 vs BYTEA（未决 A）当前按 BYTEA 落地；改判要立 ADR。
