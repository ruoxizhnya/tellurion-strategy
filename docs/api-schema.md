---
status: active
last-verified: 2026-10-25
verified-by: QT-41c 落地——build job 状态回写内部端点 + 外部编译进程装配（wire code 复用，无新增）
---

# API Schema — tellurion-strategy

> **实现状态（2026-10-25）**：10 个公开端点 + 2 个内部端点（artifact 登记 / build job 状态回写）由
> `internal/httpapi` 落地，语义在 `internal/asset.Service`（均是标准库 `net/http`，不是 gin）。
> 存储有 `Memory`（测试）与 PG（生产，启动执行迁移）；两级缓存、latest TTL、build job 持久化、
> 14 个内置策略登记均已落地。
> **QT-41c（2026-10-25）**：`cmd/assetservice` 用 `-compiler-bin` 装配外部编译进程——build 端点落 queued 行后
> 经 `internal/compilerclient` 派发 `cmd/compiler`，编译进程经内部端点回写 `running` / 终态；
> `-compiler-bin` 为空则不装配，build 端点仍 fail-closed 503 `compile_unavailable`。
> **剩余未落项**：`storage=url` 分支（只保留列、fail-closed）、`carrier=external` 的 hash 语义单独立文、core 侧 Mount 门。
> 逐条登记在 `docs/TASKS.md` QT-41/41b/41c 的施工记录里。
> `version_hash` 的输入形状与域标签已收编 contracts `v0.5.0`（G5 ③，2026-10-24）：`PUT .../source` 的 wire 形状与 hash 值不变。
>
> 本文件是**目标契约 + 现状**：已实现的行以代码为准，未实现的行是欠账，不允许把欠账当现状描述。

> **契约地位**：HTTP。**写侧 7 端点给作者/AI/CI，读侧 2 端点给引擎** —— 引擎只用那两个，其余端点它不该碰。
> 本文件是**本仓对外接口的唯一真相源**。代码与它不一致时，先判断谁错：契约错 → 改本文 + 立 ADR；实现错 → 改代码。
> 跨仓消费方（core / agent / ui / 其他服务）按本文件编码，**不读本仓实现**。

## 1. 接口面

### 写侧（作者 / AI / 编译服务）

| 方法 | 路径 | 用途 |
|---|---|---|
| POST | `/api/strategies` | 创建定义（carrier / abi_version / **scope** / capability / entry）→ 201 `{"id": ...}` |
| PUT | `/api/strategies/{id}/source` | 上传源码 → 算 `version_hash` 并登记版本 → 200 `{"version_hash": ...}` |
| POST | `/api/strategies/{id}/build` | 触发编译（异步 job）→ artifact + `artifact_hash` → 202 `{"job_id": ...}` |
| POST | `/api/strategies/{id}/versions/{hash}/validate` | 静态校验（imports 白名单 / 体积 / 内存上限）→ 200 artifact 视图 |
| POST | `/api/strategies/{id}/versions/{hash}/promote` | 标为可用（写 `active_version_hash` / `active_artifact_hash`）→ 200 artifact 视图 |
| POST | `/api/strategies/{id}/versions/{hash}/deprecate` | 下线（不删，历史回测仍可解析）→ 200 artifact 视图 |
| GET | `/api/strategies/{id}/versions` | 版本列表（status / hash / toolchain）；无版本返回 `[]`（不是 `null`） |
| GET | `/api/strategies/{id}/builds/{job_id}` | 查询 build 作业（`202` 返回的 `job_id`）；跨重启可查，路径 id 必须与作业归属一致 |

> `validate` / `promote` / `deprecate` / `artifact` 接受可选 body `{"toolchain": "..."}`（`artifact` 用同名 query 参数）；
> 同一版本有多个工具链产物且未指定 toolchain 时返回 `409 ambiguous_artifact`，**不替调用方猜**。

### 读侧（**引擎只用这两个**）

| 方法 | 路径 | 用途 |
|---|---|---|
| GET | `/api/strategies/{id}/resolve?ref={hash\|latest}` | 解析 `strategy_ref` → 元数据（含 `artifact_hash` / `scope` / `capability` / `imports`） |
| GET | `/api/strategies/{id}/versions/{hash}/artifact` | 下载字节码（响应头 `X-Artifact-Hash`，支持 `ETag` / `If-None-Match` → 304） |

> `ref` 三形态：空 / `latest` → definition 的 active；64 位小写 hex → 先按 `artifact_hash` 查、查不到再按
> `version_hash` 解析到该版本当前 promoted 的 artifact（**回写实际 `artifact_hash`**）；`id@hash` → id 必须与路径一致。

### 内部面（编译服务专用，不经公网暴露）

| 方法 | 路径 | 用途 |
|---|---|---|
| POST | `/internal/strategies/{id}/artifacts` | 编译服务回填 artifact（base64 字节 + hash 自证 + imports 自证）→ 201 artifact 视图 |
| POST | `/internal/strategies/{id}/builds/{job_id}` | 编译进程回写 build job 状态：body `{state, artifact_hash, error_code, error}` → 200 BuildJob |

> **build job 状态机（QT-41c）**：`queued → running → succeeded | failed`，`queued` 可直接到终态；
> 同终态重复上报幂等；终态不可回退。非法状态 / 未知值 → 400 `invalid_request`；不允许的跃迁 → 409 `invalid_transition`。
> `succeeded` 必须带 64 位小写 hex `artifact_hash`，且该 artifact 行已登记并属于同一 `(strategy, version, toolchain)`，
> 否则 409 `artifact_invalid`。`failed` 的 `error_code` 缺省 `compile_failed`。job 不存在或不属于路径 id → 404 `job_not_found`（不跨策略枚举）。
>
> **`error_code`（机器可读归因，随 BuildJob 返回）**：`compile_failed` / `compile_unavailable` / `payload_too_large` / `artifact_invalid`。
> 它与 HTTP 错误码不是一回事：503 `compile_unavailable` 表示「服务没装配/启动不了编译器」，作业里的
> `error_code=compile_failed` 表示「编译器跑了但这次编译失败」，调用方据此区分。

## 2. 错误码与 fail-closed 语义

响应体统一 `{"error": "...", "code": "..."}`；`code` 是**领域码**，前端/引擎按它分支。

| 情况 | 返回 | 调用方行为 |
|---|---|---|
| 策略不存在 | 404 + `strategy_not_found` | 拒绝 |
| 版本不存在 | 404 + `version_not_found` | 拒绝（**不回退 latest**） |
| 无 active 版本 | 404 + `no_active_version` | fail-closed 拒绝跑 |
| 版本没有该工具链产物 | 404 + `artifact_not_found` | 拒绝 |
| build job 不存在 / 路径 id 与作业归属不符 | 404 + `job_not_found` | 拒绝（不跨策略枚举） |
| 未指定 toolchain 而存在多行 | 409 + `ambiguous_artifact` | 由调用方显式选定 |
| 重复创建 | 409 + `strategy_exists` | 拒绝 |
| 同字节被登记成不同内容 / 同版本同工具链第二份产物 | 409 + `conflict` | 拒绝 |
| artifact 校验不过（hash 不符 / 元数据撒谎 / 体积内存越界） | 409 + `artifact_invalid` | 不许执行 |
| import 越界 | 409 + `imports_not_allowed` | 不许执行 |
| 状态机不允许的跃迁 | 409 + `invalid_transition` | 拒绝 |
| `storage=url`（未实现分支） | 409 + `storage_unsupported` | 拒绝 |
| 编译失败 | 409 + `compile_failed` | 拒绝 |
| 源码 / 产物超限 | 413 + `payload_too_large` | 拒绝 |
| 编译服务未配置 | 503 + `compile_unavailable` | 拒绝 |
| 载体不支持 | 400 + `unsupported_carrier` | 拒绝 |
| `abi_version` 不在 `on_bar/1\|2` | 400 + `invalid_abi_version` | 拒绝 |
| `scope` 非法 / v1 声明 market | 400 + `invalid_scope` | 拒绝 |
| 源码为空 | 400 + `source_required` | 拒绝 |
| toolchain 与配置不符 | 400 + `unsupported_toolchain` | 拒绝 |
| 请求体 / 参数 / 定义不合法 | 400 + `invalid_request` | 拒绝 |
| 其它 | 500 + `internal`（**不吐内部原文**） | 服务端问题 |

> **`abi_mismatch` 不在本服务产生**：本服务没有内核能力集，`abi_version` 与内核的不匹配由**引擎侧**装载门判定
> （`tellurion-contracts:engine` 的 `ErrAbiVersionMismatch`）。资产服务只回答「这个 abi_version 是否可解析、是否与字节自证相符」。
> **`job_not_found` 在对外面**：build 返回的 `job_id` 落 `strategy.strategy_build_jobs`，用
> `GET /api/strategies/{id}/builds/{job_id}` 查询（2026-10-24 落地；此前只在进程内可查）。
> **编译 / 资产四条哨兵自 contracts `v0.4.0` 起由契约仓定义**（`engine/execution_errors.go`：`ErrCompileFailed` /
> `ErrCompileUnavailable` / `ErrSourceTooLarge` / `ErrArtifactTooLarge`），本仓 `internal/asset/errors.go` 只留**同名别名**，
> 上表 wire code 与 HTTP 状态不变。跨仓消费方（agent 生成闭环 / core 取 artifact / 编排终态）按契约哨兵 `errors.Is`
> 判定，不必 import 本仓实现包；判定语义是 `%w` 保链，`err.Error()` 的措辞不属于契约。

## 3. 幂等性与重试

- `version_hash` / `artifact_hash` 内容寻址 → 重复上传同源码是幂等（同 hash 不新增行）。
- `build` 是异步 job：同 `(id, version_hash, toolchain)` 重复提交应复用已有 artifact。
- `promote` / `deprecate` 幂等（重复调用结果一致）。
- `resolve?ref=hash` 无副作用，可任意重试。

## 4. 版本与兼容

- artifact **永不失效**：引擎按 `artifact_hash` 缓存，服务停机也能跑已缓存的。
- 只有 `latest` 有 TTL —— **F 已裁决（2026-10-21）：TTL 300s**，promote 响应携带新 hash 供本进程主动失效，**不引 webhook / 消息总线**（源文 `tellurion:docs/design/qt-decoupling/strategy-source-of-truth-and-cache.md` 的裁决条 E/F/G/H）。
  原文那句「事件失效（要立 ADR）」随该裁决作废，不要再按「待立」对待。
  **已落地（2026-10-24）**：latest 结果按 strategy_id 缓存 300s，`promote` / `deprecate` 成功后同进程立即失效；
  artifact 字节走 L1 内存 LRU + L2 本地盘内容寻址（容量驱逐、内容自证），进程重启后已缓存 hash 仍按原字节返回。
- 端点废弃走 `deprecated` 头 + 一个 MINOR 周期。
- ⚠️ **semver 与 hash 是两套东西**：`strategy_ref` / `artifact_hash` / `version_hash` / `run_spec_hash` 自带版本语义（同 hash = 同内容），**不参与 semver**，也不因它们变化而 bump 版本号；
  本仓的 HTTP 面随 `tellurion-contracts` 的版本纪律走（口径源头 `tellurion:docs/TASKS.md` QT-19 ③，落地形态 `tellurion-contracts:README.md`）。

## 5. 不提供什么（边界）

- 不做信号计算、不做回测
- 不做策略运行时（逐 bar 执行在 core 的 WASM host）
- 不提供"策略市场"式搜索（不在本阶段）
- 不做第三方鉴权（`authz` 在 kit，策略权限模型另立）

## 6. 接口对表（QT-41 / QT-41b 的开工前置，2026-10-22 落）

> **这份表不是纸面作业**：每个端点/门都逐项问了**签名 · 错误语义 · 版本策略 · 今天有没有对应实物**四件事，回 core 源码取的。
> 结论先说：**§1 那 9 个端点的签名基本能对上，但错误语义这一列几乎全是「目标」**——设计里 9 个哨兵错误在代码里一个都不存在，
> 而 §2 已写的 4 个 wire code 既对不上设计里的哨兵、也覆盖不到 Verify/Compile/Mount 三道门（见偏差 #5/#6）。
> 模板：`tellurion:docs/design/qt-decoupling/data-service-contract-and-ownership.md` §4.1（QT-40 已按此做完，逮出三条前提偏差——本表的写法与判据以它为准）。
> ⚠️ 本仓 `docs/TASKS.md` 的开工前置那条原指向 `target-module-topology.md` 的「UnifiedDataPoint 节」，**那是幻影节名**（core 全文 grep 该词 0 命中），已改指本节上面的 §4.1。

### 6.1 写侧端点（7）

| # | 签名（目标） | 错误语义（wire code ↔ Go 侧） | 版本策略 | 今天的实物 |
|---|---|---|---|---|
| W1 | `POST /api/strategies` body `{carrier, abi_version, scope, capability, entry, ...}` → `201 {id}` | 新 wire code 未定（见 §2 缺列）；`carrier` 未知值必须 fail-closed 400，不许默认 `builtin` | `abi_version` 是**冻结枚举**，加值走 abi 冻结评审（不是新增字符串随便写） | ⛔ core `strategies` 表今天只有 8 列（`id, strategy_id, name, description, strategy_type, params JSONB, is_active, created_at, updated_at`，`postgres.go:284-294`），设计的 6/7 新列**一个都没有** |
| W2 | `PUT /api/strategies/{id}/source` body `{source}` → `200 {version_hash}` | 内容寻址冲突（同 hash 已存在）= 幂等 200，**不是** 409 | `version_hash` = canonical JSON over `strategy_id+carrier+abi_version+scope+entry+source` 的 sha256（api.md §2 `:50-62`） | ⚠️ 策略侧 `version_hash` **零实现**；今天唯一在跑的同类函数是因子侧 `FactorDefinition.VersionHash()`（`pkg/domain/factor_def.go:151`），域前缀与字段集都不同，**不能拿来充当**（偏差 #3） |
| W3 | `POST /api/strategies/{id}/build` → `202 {job_id}`（异步）→ 完成后 `artifact_hash` | 编译失败要有 code 且**可检索**；今天的 `staticcheck.CheckOrError` 返回**匿名 `fmt.Errorf`**，没有类型、没有 code，不可检索 | 同源码 × 两 toolchain → **两个 artifact 行**，各自可追溯（QT-41b ③-③） | ⛔ 编译管线不存在：无 `cmd/compiler`、无 TinyGo（`sandbox_test.go:6` 记录本机无 tinygo/wat2wasm）；agent 侧 `pipeline.go:634 validateCompilation` 只是临时目录 `go build` 的**检查**，**不产出字节 ⇒ 产不出 `artifact_hash`** |
| W4 | `POST /api/strategies/{id}/versions/{hash}/validate` | 体积 / imports / 内存三类失败各自的 code | 校验结果随 `artifact_hash` 落库（内容寻址：同 hash 不重算） | 实物有三块可复用：`staticcheck.Check(code) []Finding`（`pkg/sandbox/staticcheck/staticcheck.go:245`）、`wasm.ValidateImports(cm)`（`abi_check.go:46`）、`wasm.ValidateArtifactAbi(cm, declared)`（`:148`）；**但都不返回 wire code**，需要 contracts 侧补哨兵族 |
| W5 | `POST /api/strategies/{id}/versions/{hash}/promote` | 只能 promote `validated` 状态；未校验 → 409（code 未定） | **promote 不覆盖 artifact 行**（QT-41 ③-④）；响应必须带新 hash 供调用方主动失效缓存（F 裁决） | ⚠️ `active_version_hash` 列不存在，promote 今天**无处可写**；`active_artifact_hash` 更是只有工单里有（偏差 #1） |
| W6 | `POST /api/strategies/{id}/versions/{hash}/deprecate` | 已 promote 版本可 deprecate；历史回测仍可解析（解析已 promote 的 hash 不因 deprecate 失败） | **1.0 之前**删符号无义务，但本仓的 wire 面一旦发布要按 C5 保留一个 MINOR（`tellurion-contracts:CT-02` 的版本规则表） | ⛔ `strategy_artifacts.status` 状态机（`built→validated→promoted/rejected/deprecated`）无表无实现 |
| W7 | `GET /api/strategies/{id}/versions` → `[{status, version_hash, artifact_hash, toolchain}]` | 无版本 → `200 []`，**不是** `null`（DATA-14 的教训：空列表编码成 null 等于契约说谎） | 分页与排序键要现在定（后续加排序是 breaking） | ⛔ 同上，无表 |

> **2026-10-25 更新（QT-41c）**：W3 的「编译管线不存在」已收口——`cmd/compiler` + `internal/compilerclient`
> 的跨进程闭环落地（build 端点 202 → 外部进程编译 → 内部端点回写状态）。wire 面**没有新增码**：
> 状态回写复用 §2 既有 `invalid_transition` / `artifact_invalid` / `job_not_found` / `invalid_request`。

### 6.2 读侧端点（2，引擎只用这两个）

| # | 签名（目标） | 错误语义 | 版本策略 | 今天的实物 |
|---|---|---|---|---|
| R1 | `GET /api/strategies/{id}/resolve?ref={hash\|latest}` → `{strategy_id, version_hash, artifact_hash, carrier, abi_version, scope, capability, size_bytes, imports, artifact_url}` | `hash 不存在` → 404 `version_not_found` 且**不回退 latest**；`无 active 版本` → 404 `no_active_version`；**同名内置不回退**是硬约束（api.md `:88-92`） | `ref` 语法 = `id@{64 位小写 hex}` 或裸 `id`；`latest` 的 TTL = **300s**（F 已裁，不引事件） | ✅ **全表唯一在今天就实打实存在的一块**：`contracts.StrategyRef{ID,Hash}`（`pkg/backtest/contracts/provenance.go:245`）、`StrategyRefSeparator="@"`（`:262`）、`ParseStrategyRef(raw)`（`:274`，强校验 64 位小写 hex `:293`）、哨兵 `ErrStrategyRefMalformed`（`:265`）。**解析有，服务没有** |
| R2 | `GET /api/strategies/{id}/versions/{hash}/artifact` → 字节流，响应头 `X-Artifact-Hash` + `ETag` | hash 与响应体不符 → **必须 fail-closed**（设计名 `ErrArtifactHashMismatch`，今天不存在）；体积超限 → `ErrArtifactTooLarge`（不存在；`contracts` 里只有 `ErrParamsTooLarge`，语义不是同一件事） | 缓存键 = `artifact_hash`，**永不失效**；`ETag` 即 hash | ⛔ 两张 artifact 表都不存在：全仓（`.go` + `.sql`）grep `strategy_artifact` 只有 **1 处命中且不是表** —— `provenance.go:195` 的日志字段名 `strategy_artifact_hash`。唯一 BYTEA 是 `state BYTEA`（`postgres.go:1312`），与本仓无关 |

### 6.3 五道门（QT-41b ③-⑤ 要求逐项对表）

**顺序按源文，不按工单**：api.md §4 `:98-106` 与 core `docs/TASKS.md` QT-26 ③-⑤ 都写 **Resolve → Fetch → Verify → Compile → Mount**；
QT-41b ③-⑤ 那句「Resolve/Fetch/Verify/Mount 分界」漏了 Compile 且把 Mount 提前（偏差 #2）。**Compile 在 Verify 之后、Mount 之前**——
先验字节、再编、编出来的产物还要过 abi 校验，最后挂进 host。

| 门 | 输入 → 输出 | 失败语义（设计名 → 今天的实物） | 归属 |
|---|---|---|---|
| Resolve | `strategy_ref` → 元数据（含 `artifact_hash`） | `ErrStrategyNotFound` / `ErrNoActiveVersion` → **代码里 0 命中**；最接近的实物是 `Registry.Get(name)` 的 `fmt.Errorf("strategy not found: %s")`（`pkg/strategy/registry.go:51-57`），**按名字找、不是按 hash** | 资产服务 |
| Fetch | `artifact_hash` → 字节 | `ErrArtifactFetchFailed` / `ErrArtifactTooLarge` → 0 命中；`storage='url'` 分支已裁**不实现**（A 裁 BYTEA ≤ 8 MiB，超限 fail-loud） | 资产服务 |
| Verify | 字节 → 校验结论 | `ErrArtifactHashMismatch` → 0 命中；`ErrImportNotAllowed` → 实物是 `contracts.ErrImportNotWhitelisted`（`abi.go:650` 一族 17 个 `Err*` 里语义最贴的），**名字不同，对表时以既有名为准，不要再造第二个**；2026-10-24 补：`carrier=so` 校验导出符号表（`entry` 必须是已定义 GLOBAL/WEAK FUNC/NOTYPE） | 资产服务（编译无关的 abi 结构校验） |
| Compile | 源码 → 目标产物（`-target=wasm-unknown`，toolchain 版本固定，env-only 白名单） | `ErrCompileFailed` → 0 命中；`staticcheck.CheckOrError` 返回匿名 error，**不可检索、不可当契约错误** | **本仓 `cmd/compiler` 独立进程**（D 已裁，2026-10-21；不放 agent 仓、不建第 13 仓，`tellurion-docs:docs/adr/adr-035-repo-inventory-and-naming.md` 附录 D） |
| Mount | 产物 → host 可调用实例 | `ErrMountFailed` → 0 命中；实物：`wasm.ValidateLoadedArtifact(inst, family)`（`abi_check.go:292`）+ `ErrAbiVersionUnknown`/`ErrAbiVersionMismatch`（`abi.go:650` 族）+ sandbox 的 `ErrTimeout`/`ErrMemoryLimitExceeded`/`ErrModuleNotFound`/`ErrFunctionNotExported`/`ErrMemoryOutOfBounds`（`sandbox.go:56-72`）。**挂载目标存在（`SignalProvider`，`signal_provider.go:41-45`），但没有任何东西往里挂** | core 进程内（WASM host 永不做成服务） |

**为什么 6.3 的最后一列要写归属**：五道门跨两个进程（服务 ↔ core 的 host），门的**错误语义**要进 contracts 哨兵族（跨边界），门的**实现**分处两仓。这正是 C5 要求「保留一个版本周期」的那类边界。

### 6.4 版本策略（三句话，可核对）

1. **abi_version 是冻结枚举**：`contracts.HostAbiVersion` 只有 `on_bar/1` 与 `on_bar/2`（`abi.go:34-41`），`HostAbiVersionFromName()` 只认这两个写法（`:55`），其余一律 `ErrAbiVersionUnknown`。
   ⚠️ api.md `:20` 声明了四个取值（另加 `go-plugin/1`、`dsl/2`）—— **代码不接受**（偏差 #4）。要扩必须先改 abi 表、走一次 abi 冻结，再让解析与校验跟上。
2. **host 函数数量的现行数是 12（8 个 v1 + `get_bar2`/`get_series_len2`/`get_date2`/`emit_target2`）**，由 `contracts.HostFnCount()`（`abi.go:294`）返回枚举结果而不是文档字面数；设计文档里那句「13 个」在 2026-10-21 已订正。
3. **hash 与 semver 分家**：`artifact_hash` / `version_hash` 参与 run 的复现（`RunSpecInput.Hash()` 已把 `StrategyArtifactHash` / `StrategyVersionHash` 折进 `run_spec_hash`，`canonical.go:145-146`、`:195-196`，域 `tellurion.run_spec_hash.v1`），但**不参与本仓或 contracts 的版本号**。

### 6.5 前提偏差登记（七条，逐条给 QT-41 开工用）

| # | 偏差 | 证据 | 对开工的后果 |
|---|---|---|---|
| 1 | QT-41 ③-① 说要加 **7** 列，其中 `active_artifact_hash` **不在设计文档里** | api.md §1.1 `:17-24` 只有 6 列（含 `active_version_hash`）；本仓 `docs/db-schema.md:17` 也列 6 列；`active_artifact_hash` 只出现在 `docs/TASKS.md:26` 工单正文 | 二者必须收口成一个数。§1.1 的 `active_version_hash` + `UNIQUE(strategy_id, version_hash)` **答不出「当前生效的是哪个字节」**——要么补 `active_artifact_hash` 列（并回写 api.md §1.1），要么让 promote 明确写入 `strategy_artifacts.status` 后由 resolve 反查。这是**裁决不是笔误** |
| 2 | QT-41b ③-⑤ 的五道门名单漏一项、顺序也错 | 工单写「Resolve/Fetch/Verify/Mount」四项；api.md §4 `:98-106` 与 core QT-26 ③-⑤ 都是五项且 Compile 在 Mount 前 | 按源文顺序对表（见 6.3）。工单那句不改写，以本表为准 |
| 3 | 内容寻址「体系」其实只有**一半**在代码里 | `ArtifactHash(bytes)`（`provenance.go:39`）**在 pkg/cmd 生产路径零调用方**；`ParseStrategyRef` 有；策略侧 `version_hash` 无任何实现（只有因子侧同名概念） | QT-41 ③-② 不能假设「hash 体系已就绪、只管调用」。第一条要写的生产代码就是 canonical 化的 `version_hash`，并且必须选一个**域前缀**（因子侧那套不能复用） |
| 4 | `abi_version` 的取值集合：设计四个、代码两个 | api.md `:20`（含 `go-plugin/1` / `dsl/2`）vs `abi.go:34-41` `:55` `:650` | `carrier` 与 `abi_version` 是两个正交维度，但 W1 的校验逻辑今天会**直接拒绝** DSL 与 plugin 载体。要么改 abi 表（走冻结），要么把非 wasm 载体的 abi_version 定义为空并在服务端校验——**别在服务里私加枚举值** |
| 5 | §4 设计的 9 个哨兵错误**全部不存在** | grep `ErrStrategyNotFound` / `ErrNoActiveVersion` / `ErrArtifactFetchFailed` / `ErrArtifactHashMismatch` / `ErrABIIncompatible` / `ErrArtifactTooLarge` / `ErrImportNotAllowed` / `ErrCompileFailed` / `ErrMountFailed` = 各 0 命中；`ErrUnsupportedCarrier` / `ErrArtifactNotFound` 也 0 | 错误语义这一列在实现前必须先落 contracts（QT-09 的 C3/C5 靠它）。其中 `ErrABIIncompatible` ↔ 既有 `ErrAbiVersionMismatch`、`ErrImportNotAllowed` ↔ 既有 `ErrImportNotWhitelisted` 语义重叠，**照设计名新建就是同一概念两处真相** |
| 6 | §2 已登记的 4 个 wire code 既不对应哨兵、也覆盖不到三道门 | `api-schema.md:39-44` 的 `no_active_version`/`artifact_invalid`/`abi_mismatch`/`version_not_found`；而 `ErrArtifactHashMismatch`/`ErrCompileFailed`/`ErrMountFailed`/`ErrArtifactFetchFailed` **没有任何 wire code** | Verify / Compile / Mount 三道门**没有对外错误面**。开工前补齐 §2（并按 QT-50 落 openapi.yaml，wire DTO 是生成物、禁手改） |
| 7 | 内置策略的数是 **15 不是 14**，且多的那个正是 DSL 载体 | `GlobalRegister(s)` 在 `pkg/strategy/plugins/` 共 14 处 + `pkg/strategy/expression/strategy.go:354-361` 注册 `expression_template` | QT-41 ③-⑤ 的「14 个登记为 `carrier=builtin`」会**漏掉 `carrier=dsl` 这一条腿**，而施工序的腿1 恰好依赖 DSL 资产化。G 裁决（保留 init + 同步登记）不变，但登记范围要按实测 15 处来 |

### 6.6 对表之后，QT-41 / QT-41b 还剩什么前置

- **M8（本前置）✅ 已做**：本节。
- **M5 ✅ 已结案**：`tellurion:docs/design/qt-decoupling/repo-inventory.md` §1.2 —— `strategy` 在 ADR-027 §2.6.4 的服务拓扑里**有位子**（六仓里只有 data / strategy 有），容器升格仍需一条 ADR。
- **唯一硬前置：QT-05（S1 契约层落码）**。contracts 仓今天**零符号**，而本仓 module path 是单段短名 + 禁止 `replace ../` ⇒ **拿不到协议类型就无法写任何 handler**。跳序的代价已在本表两处显形：错误语义（6.3/偏差 #5）与 wire code（偏差 #6）都必须先在 contracts 定形，本仓才能落实现。
- 因此两张单的状态**保持 ⬜ 未开工**，但前置从「两项 + 一堆假设」收敛为「只剩 QT-05」；上面七条偏差是 QT-41 动手第一天的输入，不是可以留到实现期再说的小事。
