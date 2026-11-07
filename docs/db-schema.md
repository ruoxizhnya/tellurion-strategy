---
status: active
last-verified: 2026-10-25
verified-by: QT-41c 落地——`strategy_build_jobs` 追加 `error_code`（可重入 ALTER），PG/Memory 读写同步
---

# DB Schema — tellurion-strategy

> **所有权声明**：本仓**拥有 `strategy` schema 的 DDL**：策略定义、版本、artifact 元数据、字节码、编译作业与 promote 审计。是这六张表的唯一写者。
> 上级依据：`tellurion:docs/design/qt-decoupling/data-service-contract-and-ownership.md` §1 —— **DDL 只有一个 owner**，同一个 PG 实例、每服务一个 schema，将来拆库是"按 schema 搬走"而不是重写。
> 本文件是本仓表结构的**唯一真相源**：代码里的建表语句与它不一致时，**改代码**；要改表结构，先改本文件（结构级变更还要立 ADR）。

## 1. 归属表

| 表 | 内容 | 现状 |
|---|---|---|
| `strategy.strategy_definitions` | 定义（原 `strategies` 升级）：`carrier` / `abi_version` / `scope` / `capability` / `entry` / `active_version_hash` / **`active_artifact_hash`** | 改造（7 新列，见 §5 裁决） |
| `strategy.strategy_versions` | 源码版本：`version_hash`（内容寻址）/ `source` / `carrier` / `abi_version` / `scope` / `entry` | 新建 |
| `strategy.strategy_artifacts` | artifact 元数据：`version_hash` + `artifact_hash` / `size_bytes` / `memory_limit_bytes` / `imports` / `toolchain` / `status` / `storage` / `compile_log` / `built_by` | 新建（主键 `(strategy_id, artifact_hash)`，见 §5 裁决） |
| `strategy.strategy_artifact_blobs` | 字节码本体（本地栈 BYTEA，8 MiB 上限；`storage='url'` 未实现） | 新建 |
| `strategy.strategy_build_jobs` | 编译作业：`job_id` / `version_hash` / `toolchain` / `state` / `artifact_hash` / **`error_code`** / `error` / `finished_at` | 新建（2026-10-24；job 跨重启可查）；`error_code` 于 2026-10-25 追加 |
| `strategy.strategy_promotions` | promote 审计（append-only）：`strategy_id` / `version_hash` / `artifact_hash` / `actor` / `outcome` / `decided_at` | 新建（2026-11-07，SVC-STRAT-G1） |
| `quant.external_signals` | 外部模型信号注入（`content_hash` 幂等去重，`SignalStrategy` 按 `as_of` 拉取） | **不属本仓** · owner = **core strategy-runtime 模块**（与 H5 裁决的 `quant.strategy_state` 同源，SPEC-R-02）；DDL 在 `tellurion:pkg/storage/postgres.go`（Migration 048），写者 `tellurion:pkg/strategy/signal_store.go` |

约束：
- `strategy_versions`：`PRIMARY KEY (strategy_id, version_hash)`。
- `strategy_artifacts`：`PRIMARY KEY (strategy_id, artifact_hash)` + `UNIQUE (strategy_id, version_hash, toolchain)`；
  字节相同即同一 artifact（内容寻址优先），同版本同工具链不允许第二份字节。
- `strategy_artifact_blobs`：`PRIMARY KEY (strategy_id, artifact_hash)` + `octet_length(bytes) <= 8388608`。
- `strategy_build_jobs`：`PRIMARY KEY (id)` + `state IN ('queued','running','succeeded','failed')`；
  `(strategy_id, version_hash)` 外键指向版本行；`finished_at` 只在终态写入。
  `error_code` 是**机器可读归因码**（`compile_failed` / `compile_unavailable` / `payload_too_large` / `artifact_invalid` / `compile_timeout`），
  非空判定由服务层做（成功作业写空串），`NOT NULL DEFAULT ''` 保证旧行可读。
- `strategy_promotions`：`PRIMARY KEY (id)`（BIGSERIAL）+ `actor <> ''` + `outcome <> ''`；
  **不设外键**——策略/版本被删也不能连带抹掉审计；只追加，不 UPDATE / DELETE。
  写者只有 `internal/asset.PG.RecordPromotion`（HTTP 层在 promote 成功后调用，缺 `X-Actor` 时不写）。
- `version_hash` 由**源码 canonical 化后 sha256**派生，内容改必须改 hash。
  输入六项与域标签由 contracts `v0.5.0` 提供（`StrategyVersionInput` / `DomainStrategyVersionHash`，G5 ③ 收编），
  本仓不再定义第二份；域字符串逐字节未变，**历史 `version_hash` 全部仍然有效**。

### 1.1 K5 对照表：`strategy.strategy_definitions` vs core `strategies`（SVC-STRAT-G2 ③）

> 这张表是 K5 的答卷，也是 `tellurion:SVC-CORE-STRAT-RETIRE` 的前置。**逐列/逐语义**对照，
> 出处写到行；结论列写「已对齐 / 待对齐 / 本仓不承担」，不许用「大体一致」。
> **双 owner 事实**：同一逻辑概念（策略定义）今天有**两个写者**——core 写 `quant.strategies`
> （`tellurion:pkg/storage/strategies.go`），本仓写 `strategy.strategy_definitions`（`internal/asset/pgstore.go`）。
> 在 core 停写前，G2 的「它的表只有它写」不可能 ✅；本表只如实登记。

| 维度 | core `strategies`（今天） | 本仓 `strategy.strategy_definitions`（目标态） | 出处 | 结论 |
|---|---|---|---|---|
| schema / 表名 | `public.strategies`（Migration 011 未带 schema，依赖 search_path） | `strategy.strategy_definitions` | `tellurion:docs/migrations/011_add_strategies_table.sql:4`；`internal/asset/ddl.go:27` | 待对齐（退役时改名/搬 schema） |
| 主键与业务键 | `id SERIAL` + `strategy_id VARCHAR(50) UNIQUE` | `id TEXT PRIMARY KEY`（就是 strategy_id） | `tellurion:docs/migrations/011_add_strategies_table.sql:5-6`；`internal/asset/ddl.go:28` | 已对齐（业务键同义） |
| 名称/描述 | `name` / `description` | **无**（入口是 `entry`；名称属展示层） | `tellurion:docs/migrations/011_add_strategies_table.sql:7-8`；`internal/asset/ddl.go:29-38` | 待对齐（core 消费方要名称时需另给映射） |
| 参数 | `params JSONB NOT NULL DEFAULT '{}'` | `capability JSONB`（能力声明，不是运行参数） | `tellurion:docs/migrations/011_add_strategies_table.sql:10`；`internal/asset/ddl.go:32` | 语义不同（不迁移） |
| 类型 | `strategy_type VARCHAR(30)` | **无**；由 `carrier`（builtin/dsl/wasm/so/external）+ `abi_version` 表达 | `tellurion:docs/migrations/011_add_strategies_table.sql:9`；`internal/asset/ddl.go:29-30` | 待对齐（映射表随退役单落） |
| 启用/停用 | `is_active BOOLEAN`，删除=**软删**（`UPDATE ... SET is_active=FALSE`，行仍在） | **无删除**：定义行只增不删；「生效」由 `active_version_hash` / `active_artifact_hash` 表达；下线走 `deprecate`（版本级） | `tellurion:pkg/storage/strategies.go:106-116`；`internal/asset/ddl.go:34-38`；`internal/asset/service.go` 的 `Deprecate` | 语义不同（行消失 vs 版本下线）——**K5 的核心差异** |
| 版本概念 | **无**：一条 `strategy_id` 一行，参数原地覆盖（`ON CONFLICT DO UPDATE`） | 版本链：`strategy_versions` 按 `(strategy_id, version_hash)` 多行；`active_version_hash` 指向当前 | `tellurion:pkg/storage/strategies.go:12-31`；`internal/asset/ddl.go:41-45` | 待对齐（退役单的迁移算法） |
| 内容寻址 | **无** `version_hash` / `artifact_hash` | `version_hash` = 源码 canonical sha256（域 + 六输入由 contracts 定义）；`artifact_hash` = 字节 sha256 | `internal/asset/hash.go`；`docs/api-schema.md` §2 | 本仓新增能力（core 无对应列） |
| 内置策略 | `SeedStrategies` 播种 **3 条**（momentum / value / quality） | 启动登记 **14 条** `carrier=builtin`（无 artifact，`entry`=策略名） | `tellurion:pkg/storage/strategies.go:118-160`；`internal/asset/builtin.go` | **数量不一致**（3 vs 14）——退役前必须对清，见下 |
| 写者（今天） | `SaveStrategyConfig`（upsert）/ `DeleteStrategyConfig`（软删）/ `SeedStrategies`（启动种子）；另 `cmd/analysis/setup.go:336` 调 Seed | 只有本仓：`internal/asset.PG` 的 `CreateDefinition` / `Promote` / `Deprecate` / 内置登记；**零**写 `quant.strategies` | `tellurion:pkg/storage/strategies.go:12,106,119`；`internal/asset/pgstore.go:57,393,420` | 双 owner（定义真相源仍在 core） |
| 时间戳 | `created_at` / `updated_at`（`NOW()`，upsert 时刷新 `updated_at`） | `created_at` / `updated_at`（`now()`；本仓不改写 `created_at`） | `tellurion:docs/migrations/011_add_strategies_table.sql:12-13`；`internal/asset/ddl.go:36-37` | 已对齐 |

**内置策略 3 vs 14 的处置**：core 只播种 3 条，本仓登记 14 条（`internal/asset/builtin.go` 的名单来自
core 引擎 `SignalGenerator` 的实际实现集）。这不是本仓擅自扩表——本仓登记的是**可解析的资产**；
core `strategies` 里的 3 条是**数据库里的配置行**。退役单必须逐条判定「14 条里哪些需要在 core 有配置行」，
不许按数量直接迁移。该判定归 `tellurion:SVC-CORE-STRAT-RETIRE`，本仓只提供两侧名单与出处。

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
- **2026-10-25（QT-41c）**：`strategy_build_jobs` 追加 `error_code`，迁移是数组末尾的一条
  `ALTER TABLE strategy.strategy_build_jobs ADD COLUMN IF NOT EXISTS error_code TEXT NOT NULL DEFAULT ''`；
  已落地的建表语句**一字未改**，空库 / 既有库都能重入执行。
- **2026-11-07（SVC-STRAT-G1）**：新增 `strategy_promotions`（append-only 审计），迁移是数组末尾的
  `CREATE TABLE IF NOT EXISTS strategy.strategy_promotions (...)`；无外键、无回填，空库 / 既有库都可重入。
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

- **同版本多工具链的 promote 语义**：当前 active 指针只记一个 `artifact_hash`（即「此刻生效的是哪份字节」），
  「同版本另一工具链后来也 promote」会覆盖指针。是否需要「多内核各持一个 active」，取决于 NT 侧接入形态（未决 C）。
