---
status: active
last-verified: 2026-10-23
verified-by: 两处主键/列裁决落地（docs/adr/0002）+ DDL 与代码逐列对表（internal/asset/ddl.go）
---

# DB Schema — tellurion-strategy

> **所有权声明**：本仓**拥有 `strategy` schema 的 DDL**：策略定义、版本、artifact 元数据与字节码。是这三张表的唯一写者。
> 上级依据：`tellurion:docs/design/qt-decoupling/data-service-contract-and-ownership.md` §1 —— **DDL 只有一个 owner**，同一个 PG 实例、每服务一个 schema，将来拆库是"按 schema 搬走"而不是重写。
> 本文件是本仓表结构的**唯一真相源**：代码里的建表语句与它不一致时，**改代码**；要改表结构，先改本文件（结构级变更还要立 ADR）。

## 1. 归属表

| 表 | 内容 | 现状 |
|---|---|---|
| `strategy.strategy_definitions` | 定义（原 `strategies` 升级）：`carrier` / `abi_version` / `scope` / `capability` / `entry` / `active_version_hash` / **`active_artifact_hash`** | 改造（7 新列，见 §5 裁决） |
| `strategy.strategy_versions` | 源码版本：`version_hash`（内容寻址）/ `source` / `carrier` / `abi_version` / `scope` / `entry` | 新建 |
| `strategy.strategy_artifacts` | artifact 元数据：`version_hash` + `artifact_hash` / `size_bytes` / `memory_limit_bytes` / `imports` / `toolchain` / `status` / `storage` / `compile_log` / `built_by` | 新建（主键 `(strategy_id, artifact_hash)`，见 §5 裁决） |
| `strategy.strategy_artifact_blobs` | 字节码本体（本地栈 BYTEA，8 MiB 上限；`storage='url'` 未实现） | 新建 |
| `strategy.strategy_build_jobs` | 编译作业：`job_id` / `version_hash` / `toolchain` / `state` / `artifact_hash` / `error` / `finished_at` | 新建（2026-10-24；job 跨重启可查） |
| `quant.external_signals` | 外部模型信号注入（`content_hash` 幂等去重，`SignalStrategy` 按 `as_of` 拉取） | **不属本仓** · owner = **core strategy-runtime 模块**（与 H5 裁决的 `quant.strategy_state` 同源，SPEC-R-02）；DDL 在 `tellurion:pkg/storage/postgres.go`（Migration 048），写者 `tellurion:pkg/strategy/signal_store.go` |

约束：
- `strategy_versions`：`PRIMARY KEY (strategy_id, version_hash)`。
- `strategy_artifacts`：`PRIMARY KEY (strategy_id, artifact_hash)` + `UNIQUE (strategy_id, version_hash, toolchain)`；
  字节相同即同一 artifact（内容寻址优先），同版本同工具链不允许第二份字节。
- `strategy_artifact_blobs`：`PRIMARY KEY (strategy_id, artifact_hash)` + `octet_length(bytes) <= 8388608`。
- `strategy_build_jobs`：`PRIMARY KEY (id)` + `state IN ('queued','running','succeeded','failed')`；
  `(strategy_id, version_hash)` 外键指向版本行；`finished_at` 只在终态写入。
- `version_hash` 由**源码 canonical 化后 sha256**派生，内容改必须改 hash。

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

- **执行机制已定（取向 ①，2026-10-23）**：Go 数组 + 启动执行、幂等、无版本表 —— 语句本体在
  `internal/asset/ddl.go` 的 `migrationStatements`，由 `asset.Migrations()` 交出副本。
  **执行入口已接线（2026-10-24）**：`cmd/assetservice` 在 DSN 非空时先 `asset.OpenPG` → `PG.Migrate(ctx)`
  再监听；DSN 为空才退回 `Memory`（日志明确标注非持久化）。PG 实现见 `internal/asset/pgstore.go`。
- 不另建 `migrations/*.sql`：本仓 DDL 的唯一真相是上面那个 Go 数组；`.sql` 会把真相劈成两份。
- **只前滚**：已合并的迁移不改；修错靠新增一条。
- 每条必须可重入（`IF NOT EXISTS` / `IF EXISTS`），且能在空库上一次跑完。
- 跨 schema 取数只走**只读视图或 API**；出现 `join` 别人的表就是边界腐化的第一信号。
- 结构变更的同步义务：本文件 + `docs/ARCHITECTURE.md` 的「数据归属」节必须同一次提交里更新。

## 5. 已裁决

- **A（对象存储 vs BYTEA，2026-10-21）**：本地栈 PostgreSQL `BYTEA`，**单 blob ≤ 8 MiB 且超限 fail-loud**（不静默截断、不落本地盘）；`storage='url'` **只保留列分支、不实现**（将来的逃逸阀）。源文 `tellurion:docs/design/qt-decoupling/strategy-asset-service-api.md` 的裁决记录。
- **#1 新列 6 vs 7（2026-10-23）**：**补 `active_artifact_hash` 第 7 列**，并用 `CHECK ((active_version_hash IS NULL) = (active_artifact_hash IS NULL))`
  钉住「要么都没有、要么都有」。理由：resolve 要能不查 artifact 行就回答「当前生效的是哪个字节」；靠 `status='promoted'` 反查在两工具链场景下不唯一。
  裁决落 `docs/adr/0002-artifact-主键与生效字节的裁决.md`。
- **artifact 主键（2026-10-23，同上 ADR）**：`PRIMARY KEY (strategy_id, artifact_hash)` + `UNIQUE (strategy_id, version_hash, toolchain)`。
  同源码 × 两工具链 → 两行可分别追溯；同字节跨工具链不产生第二行（内容寻址优先，谁编译的以该行 `toolchain` 为准，
  不允许同一份字节出现两条互相矛盾的「编译来源」）。

## 6. 待定

- **`version_hash` 的域标签不在 contracts**：`tellurion-contracts:engine/canonical.go` 只有 `run_spec_hash` / `dataset_snapshot_hash` /
  `universe_hash` / `calendar_hash` / `adjustment_anchor_digests` / `params_hash` 六个域，没有 `strategy_version_hash`。
  本仓暂在 `internal/asset/hash.go` 定义 `tellurion.strategy_version_hash.v1`。
  **风险**：若将来 contracts 收编并改用别的字符串，所有历史 `version_hash` 全部失效；要么 contracts 以这个字符串为准收录，要么本仓保留为唯一 owner。
- **同版本多工具链的 promote 语义**：当前 active 指针只记一个 `artifact_hash`（即「此刻生效的是哪份字节」），
  「同版本另一工具链后来也 promote」会覆盖指针。是否需要「多内核各持一个 active」，取决于 NT 侧接入形态（未决 C）。
