---
status: active
last-verified: 2026-10-22
verified-by: 接口对表时订正两处过时声明（A 已裁 BYTEA；新列 6/7 不一致待裁）
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
| `quant.external_signals` | 外部模型信号注入（`content_hash` 幂等去重，`SignalStrategy` 按 `as_of` 拉取） | **不属本仓** · owner = **core strategy-runtime 模块**（与 H5 裁决的 `quant.strategy_state` 同源，SPEC-R-02）；DDL 在 `tellurion:pkg/storage/postgres.go`（Migration 048），写者 `tellurion:pkg/strategy/signal_store.go` |

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

## 4. 建表与迁移

**现状（必须先知道）**：core 的迁移是「**启动时按序执行一组幂等 DDL**」——唯一真相是 `pkg/storage/postgres.go` 的 `migrationStatements()` 数组（`migrate()` 在服务启动时逐条跑）；**没有版本表、没有 down、没有执行记录**（有意取舍，理由见 core 的 `migrations/README.md`）。`migrations/`（14 个）与 `docs/migrations/`（25 个）里的 `.sql` 是**历史镜像，不执行**——改它们不会改库。

**本仓约定**（从建仓第一天执行）：

- 迁移文件只放 `migrations/`，命名 `<序号>_<动作>.sql`，**本仓内**递增（不与别的仓共享序列，也不复用号）。
- **执行机制随第一个建表单定，二选一（不能"都不选"）**：
  1. **core 式**：Go 里的 `migrationStatements()` 数组 + 启动执行，幂等、无版本表——与 core 一致、运维零新增；
  2. **版本化**：引入迁移库 + `schema_migrations` 表（可 down、有执行记录）——服务变多后更稳，但要一次性 baseline。
  **取向**：X1~X3 期间优先 ①（少一层依赖），服务数 ≥3 或出现"必须先降级"的需求时重估 ②。
- **只前滚**：已合并的迁移不改；修错靠新增一条。
- 每条必须可重入（`IF NOT EXISTS` / `IF EXISTS`），且能在空库上一次跑完。
- 跨 schema 取数只走**只读视图或 API**；出现 `join` 别人的表就是边界腐化的第一信号。
- 结构变更的同步义务：本文件 + `docs/ARCHITECTURE.md` 的「数据归属」节必须同一次提交里更新。

## 5. 待定

- ~~对象存储 vs BYTEA（未决 A）当前按 BYTEA 落地；改判要立 ADR~~ → **A 已裁决（2026-10-21）**：本地栈 PostgreSQL `BYTEA`，**单 blob ≤ 8 MiB 且超限 fail-loud**（不静默截断、不落本地盘）；`storage='url'` **只保留列分支、不实现**（将来的逃逸阀）。源文 `tellurion:docs/design/qt-decoupling/strategy-asset-service-api.md` 的裁决记录。
- ⚠️ **上表 `strategy_definitions` 那行列了 6 个新列，QT-41 ③-① 说的是 7 个**：多出来的是 `active_artifact_hash`，它**只在工单正文里出现，设计文档 §1.1 与本表都没有**。
  两种收口方式（答「当前生效的是哪个字节」）：补列，或让 resolve 经 `strategy_artifacts.status='promoted'` 反查。
  **这是待裁决，不是笔误**——登记与后果见 `docs/api-schema.md` §6.5 偏差 #1。
