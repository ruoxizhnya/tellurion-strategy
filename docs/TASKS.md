---
status: active
last-verified: 2026-10-25
verified-by: QT-41c 收口 QT-41b 余项——编译进程状态回写内部端点 + `internal/compilerclient` + `cmd/assetservice` 外部编译器装配
---

# TASKS — tellurion-strategy 未完成项

> 本仓**唯一**任务台账。跨仓任务只在 §二 留指针，**不镜像正文**（双写必然漂移）。
> 状态：`⬜ 待做` / `🔶 进行中` / `⛔ 阻塞（有未解除前置）` / `✅ 完成（附日期）`
> 每张单六字段（① 目标 ② 上下文 ③ 要求 ④ 约束与非目标 ⑤ 验收 ⑥ 边界），判据见 `tellurion:AGENTS.md` §8.5。
> 完成即标 ✅（附日期）并移入 `docs/archive/TASKS-YYYY-MM-DD-archived.md`；本文件只留未完成项。

---

## 一、本仓任务

> **约定变更（2026-10-21）**：原「占位仓不写正式六字段任务单」作废——X 线任务单已展开为本仓正式任务（见下）。M5/M8 开工前置不变；实现仍未开工，开工前仍须过前置两条。

### QT-41 · X2 策略资产服务（定义/版本/artifact/resolve）[P1] ✅（2026-10-24 G4 收口；余项为非目标/跨仓指针）

> **迁入说明（2026-10-21）**：本单自 core 仓 `tellurion:docs/TASKS.md`「五、X 线」迁入（任务台账批量重分布：X 线单展开到各服务仓为正式六字段单，「占位仓不写正式任务单」旧约定同时作废）。六字段正文逐字保留、不改写；core 侧同号条目已改为指针。

- **① 目标**：策略真相源在服务侧，引擎只持可驱逐只读副本；`id@artifact_hash` 可离线复现。
- **② 上下文**：`strategy-asset-service-api.md` + `strategy-source-of-truth-and-cache.md`（D13/D14）+ QT-03 的 hash 体系；**A/E/F/G/H 已于 2026-10-21 裁决**（见设计集 `README.md` 裁决记录）：A=本地栈 PG BYTEA + 8 MiB 上限、保留 `storage='url'` 逃逸阀；E=内存 LRU 一级 + 本地磁盘内容寻址二级；F=latest 用 TTL 300s，不引入事件；G=保留 init + 同步登记、不做全量迁移；H=`carrier=external` 纳入同一 `strategy_ref` 命名空间。
- **③ 要求**：① `strategies` 加列（carrier/abi_version/scope/capability/entry/`active_version_hash`/`active_artifact_hash`）+ `strategy_artifacts` + blobs（**A 已裁决**：本地栈落 PG BYTEA，单 blob ≤ 8 MiB 且超限 fail-loud，`storage='url'` 只保留列分支作将来逃逸阀）；② 写侧 7 端点、读侧 2 端点（resolve/artifact）；③ 缓存按 `artifact_hash`（永不失效）+ latest（**F 已裁决：TTL 300s**，promote 响应携带新 hash 供本进程主动失效，不引入 webhook/msgbus；**E 已裁决**：内存 LRU 一级 + 本地磁盘内容寻址二级，按容量与 TTL 驱逐）；④ promote 不覆盖 artifact 行；⑤ 14 个内置策略登记为 `carrier=builtin`（**G 已裁决：保留 init 启动 + 同步登记，不做全量迁移**）；⑥ `carrier=external` 纳入同一 `strategy_ref` 命名空间（**H 已裁决**），解析按 carrier 路由，外部载体 hash 语义单独成文、不与 `artifact_hash` 混用。
- **④ 约束与非目标**：策略服务**不算信号**；不改执行路径的"先查 artifact 再回退内置"在 QT-26 已定的顺序；不做对象存储（A 已裁 BYTEA，`storage='url'` 仅保留列分支、不实现）；不引入 webhook/msgbus（F 已裁 TTL）；不把内置策略全量迁移（G 已裁）；**裁决已落，相关子项按裁决值执行，不再按「假设」对待。**
- **⑤ 验收**：服务停机后已缓存 hash 的回测照跑且结果一致；同源码两工具链 → 两 artifact 可分别追溯；`id@version_hash` 解析结果回写实际 `artifact_hash`。
- **⑥ 边界**：只做资产与解析；编译服务是独立单 **QT-41b**（本单 ①~⑤ 不含编译链路）。

> **🔶 开工前置状态（2026-10-22）**：M8「接口对表」**已做完** → 本仓 `docs/api-schema.md` §6（写侧 7 端点 + 读侧 2 端点逐条签名 / 错误语义 / 版本策略 / 今天的实物）。
> M5 ✅ 已结案。⇒ **本单只剩一条前置：QT-05（S1 契约层落码）**——contracts 今天零符号，而本仓 module path 是单段短名 + 禁 `replace ../`，拿不到协议类型就写不了任何 handler。
> 对表逮出**七条前提偏差**（§6.5），其中两条要主任裁：**#1** 新列到底是 6 还是 7（`active_artifact_hash` 只在工单里有）、**#4** `abi_version` 设计四个取值而 `contracts` 只认 `on_bar/1|2`。
> 另两条会直接误导第一行代码：**#3** `version_hash` 策略侧零实现、`ArtifactHash()` 生产路径零调用方；**#5** 设计的 9 个哨兵错误代码里一个都不存在（其中两个与既有 `ErrAbiVersionMismatch` / `ErrImportNotWhitelisted` 语义重叠，另造就是两处真相）。
> 状态保持 ⬜ 未开工是对的，但前置已从「两项 + 一堆假设」收敛为「只剩 QT-05」。

> **施工记录（2026-10-23）**：
>
> **已交付**（`gofmt` / `go build ./...` / `go vet ./...` / `go test ./... -count=1` 全绿）：
> - `internal/asset`：`Definition`(7 列含 `active_artifact_hash`) / `Version` / `Artifact` 模型与 `Validate()`；`VersionHash`
>   （域 `tellurion.strategy_version_hash.v1`，6 输入：strategy_id / carrier / abi_version / scope / capability / entry / source）；
>   `Verify`（五道门里的 Verify：storage=url 拒 → 空字节 → 8 MiB → size 一致 → sha256 → abi 可解析 → memory 上限 →
>   imports 归一化 → wasm 白名单）；`WasmImports`（LEB128 解析 import section）；`ImportPolicyFor`（按 contracts
>   `HostABIContracts()` 的 `Since` 派生白名单，不另造一张表）；`Store` 端口 + `Memory` 实现；`Migrations()`（5 条幂等 DDL）；
>   `Service`（9 端点语义 + `Resolve` 三形态 ref + `Promote`/`Deprecate` 状态机 + build job 注册表）。
> - `internal/httpapi`：标准库 `http.ServeMux` 的 9 个公开端点 + 1 个内部登记端点；严格解码（`DisallowUnknownFields` +
>   `LimitReader` + 拒绝多对象）；`errorBody{error,code}` 与 22 个领域 wire code；`artifact` 端点 ETag / `X-Artifact-Hash` /
>   immutable / 304。
> - `internal/compile`：`Runner`（`-target=wasm-unknown` 精确 token 检查、env 白名单、64 KiB 日志截断、超时、超限 fail-loud）。
> - `internal/compilerjob`：编译 → `artifact_hash` → 内部登记端点回填（hash 与 imports 双向自证）。
> - `cmd/assetservice`（`127.0.0.1:8085`）/ `cmd/compiler`（读 job JSON、严格解码、stdout 回执）。
> - 测试：`internal/asset`（服务语义 + DDL 形状 + 五道门）、`internal/httpapi`（9 端点全生命周期 + 20 条 wire code 表驱动 +
>   幂等/歧义/撒谎字节）、`internal/compile`（真子进程假工具链，含 env 白名单泄露红线）、`internal/compilerjob`（真 Service + httptest）。
> - 文档：`docs/api-schema.md` §1/§2 按实现重写；`docs/db-schema.md` §1/§4/§5/§6 更新；`docs/adr/0002-artifact-主键与生效字节的裁决.md`。
>
> **裁决落地**：偏差 #1 → 补 `active_artifact_hash` 第 7 列 + 双空 CHECK；artifact 主键 → `(strategy_id, artifact_hash)` +
> `UNIQUE(strategy_id, version_hash, toolchain)`；偏差 #5 → 复用 contracts 既有哨兵，不另造 9 个；偏差 #6 → 补齐领域 wire code
> 并删掉本服务不产生的 `abi_mismatch` / `job_not_found`。
>
> **施工记录（G4，2026-10-24）**：10 条未落项逐条处置（✅ 已落 / ➖ 非本单目标或跨仓）：
>
> 1. ✅ **PG `Store` + 启动迁移执行**：`internal/asset/pgstore.go` 实现全部 Store 写读；`cmd/assetservice`
>    DSN 非空时 `OpenPG` → `PG.Migrate(ctx)`（幂等 DDL）再监听；`CHAR(64)` 读回统一 `trim`（PG 会补空格，不 trim 会破坏内容寻址比较）。
> 2. ✅ **build job 持久化 + `job_id` 查询**：新增 `strategy.strategy_build_jobs`（第 6 条 DDL）；
>    `POST .../build` 先落 queued 再起 goroutine；`GET /api/strategies/{id}/builds/{job_id}` 查询，
>    路径 id 必须与作业归属一致，否则 404 `job_not_found`（不跨策略枚举）。
> 3. ✅ **E/F 缓存裁决**：artifact 字节 L1 内存 LRU + L2 本地磁盘内容寻址（`<dir>/<hash[:2]>/<hash>`，
>    读出重算 sha256、不符即删；按容量驱逐，artifact **永不 TTL**）；`latest` 结果 TTL **300s**，
>    `promote` / `deprecate` 成功后同进程主动失效（不引 webhook / msgbus）。
> 4. ✅ **G：14 个内置策略登记**：`internal/asset/builtin.go`；启动时同步登记 `carrier=builtin`、无 artifact、
>    `scope=market`、`entry=策略名`（依据 core `SignalGenerator` 的整张 bars map 与设计 §4「entry 指向二进制内符号」）。
>    已存在同名定义只核对形状、不覆盖；同名不同 carrier/scope/entry = 启动失败。`expression_template` 属 `carrier=dsl`，不在此 14 个内。
> 5. ➖ **`storage='url'`**：仍只保留列、代码 fail-closed `storage_unsupported`；A 裁决明确不实现（非目标，不是欠账）。
> 6. ✅ **so 导出符号表校验**：`internal/asset/elf.go`（标准库 `debug/elf`）：要求 ET_DYN，`entry` 在 `.dynsym`
>    （无则 `.symtab`）里已定义，绑定 GLOBAL/WEAK，类型 FUNC/NOTYPE。`Verify` 增加 `entry` 入参，所有调用点从版本行取。
> 7. ➖ **core 侧 Mount 门**：不在本仓（ADR-027 边界；引擎进程内的 WASM host）。
> 8. ✅ **真 TinyGo 镜像**：`internal/compile/toolchains.lock` 灌官方 `tinygo/tinygo:0.34.0` 的二进制 sha256；
>    真镜像编译两次逐字节一致，`artifact_hash` 固定；证据 `docs/evidence/QT-41b-tinygo-0.34.0.md`。
>    **实测逮到一个可复现性缺陷并修掉**：TinyGo 默认把源文件绝对路径写进 DWARF，Runner 的随机临时目录会让同源码产出两个
>    hash；`compile.Config` 现对 `name=tinygo` 强制要求精确参数 `-no-debug`。
> 9. ✅ **`version_hash` 域标签迁 contracts**：已随 contracts `v0.5.0` 收编（G5 ③，2026-10-24，本文件 §「③ 收编」记录）；本仓不再定义域标签与 `VersionInput`，编码委托 `contracts.StrategyVersionInput.Hash()`。
> 10. ✅ **`Promote` 两步写改事务**：`Store.Promote` / `Store.Deprecate` 把「artifact 状态 + definition 生效指针」
>     收进单事务（PG）或单锁（Memory）；Service 不再分两步写。
>
> **验收证据**：
> - 服务停机后已缓存 hash 照跑且结果一致：`TestService_CachedArtifactServedAfterRestartWithBlobStoreOffline`
>   （新 Cache 实例 = 进程重启；底层 `GetArtifact` 故意失败，仍从 L2 返回逐字节相同的 artifact）。
> - 同源码两工具链 → 两 artifact 可分别追溯：`TestMemoryStore_ArtifactsKeyedByArtifactHash` /
>   `TestPG_ArtifactsKeyedByArtifactHash`（主键 `(strategy_id, artifact_hash)` + `UNIQUE(version, toolchain)`）。
> - `id@version_hash` 解析回写实际 `artifact_hash`：`TestAPI_WasmLifecycle` 与真镜像 E2E
>   （`st_b324a1ff…` / version `cd488c31…` → `artifact_hash=fd0ecc26…`，下载 270 字节同 hash）。
>
> **破坏验证（改坏应红，逐字节还原后应绿；本机实跑）**：
> - Promote 改为先 commit 再写 active → `TestPG_StatusMachineAndTransactionalPromote` 红（"status = promoted after a failed promote"）；还原后绿。
> - promote 不失效 latest → `TestService_LatestCacheTTLAndPromoteInvalidation` 红；还原后绿。
> - 内置登记去掉形状核对 → `TestService_RegisterBuiltinsRejectsShapeConflict` 红；还原后绿。
> - Verify 跳过 so 符号表 / job 端点不校验归属 → 对应测试红；还原后绿。
> - 去掉 `-no-debug` 必填规则 → `TestConfig_TinyGoRequiresNoDebug` 红；还原后绿。
>
> **测试（本机实测）**：`go build ./... && go vet ./... && go test ./... -count=1` 全绿；
> `STRATEGY_TEST_DSN=... go test ./internal/asset -count=1`（PG 集成）全绿；
> `STRATEGY_TINYGO_IMAGE=tinygo/tinygo:0.34.0 go test ./internal/compile -run TestCompile_RealTinyGoImage -v` 绿。
>
> **文档同步**：`db-schema.md`（第 5 张表 + 迁移执行）、`api-schema.md`（job 端点 + `job_not_found` + 缓存落地）、
> `dataflow.md`（启动登记 / job 持久化 / 缓存 / so 符号）、`ARCHITECTURE.md` + `adr/0003`（存储/缓存/作业/可复现性）。
>
> **余项（不属于本单验收，未做也不冒充已完成）**：`storage='url'` 分支、`carrier=external` 的 hash 语义单独立文、
> core 侧 Mount 门。~~`version_hash` 域标签迁 contracts~~ 已随 `v0.5.0` 收编（G5 ③）。

### QT-41b · X2 策略编译服务（Go 子集 → artifact 管线）[P1] ✅（2026-10-22 开工，2026-10-23 首批实装落地；2026-10-24 真 TinyGo 0.34.0 已接；2026-10-25 余项由 QT-41c 收口）

> **迁入说明（2026-10-21）**：本单自 core 仓 `tellurion:docs/TASKS.md`「五、X 线」迁入（任务台账批量重分布：X 线单展开到各服务仓为正式六字段单，「占位仓不写正式任务单」旧约定同时作废）。六字段正文逐字保留、不改写；core 侧同号条目已改为指针。

- **① 目标**：策略源码到 `artifact_hash` 产物的编译链路成文并落地（编译 → 校验 → 登记 artifact），AI 生成闭环（QT-32）与人工 promote 共用同一条管线。
- **② 上下文**：`strategy-asset-service-api.md` §4 五道门中的 Compile 门；`implementation-plan-mvp.md` §6-M2（AI 生成闭环含编译环节）；原 QT-41 ⑥ 曾以「QT-41b」悬空引用（refactor-plan-review H3），本单即其正式落点。**D 已于 2026-10-21 裁决**：编译服务落在**本仓的独立进程/容器（`cmd/compiler`，独立镜像、锁 toolchain 版本）**，不新建第 13 个仓，也不放 agent 仓（`agent` 只负责生成源码并调用编译服务）；ADR-027 §2.6.1「编译在 ai-service」登记为**被本裁决修订**。
- **③ 要求**：① Compile 门语义成文（toolchain 版本固定、env-only 白名单、产物 `-target=wasm-unknown`）；② 编译产物经 QT-03 的 `artifact_hash` 登记进 QT-41 的 `strategy_artifacts`；③ 同源码两工具链 → 两 artifact 可分别追溯（与 QT-41 ⑤ 对齐）；④ 编译失败的错误语义进 contracts 哨兵错误族；⑤ 与 QT-26 ③-⑤ 五道门的 Resolve/Fetch/Verify/Mount 分界逐项对表；⑥ 交付形态按 D 裁决：本仓 `cmd/compiler` 独立进程/容器，toolchain 镜像固定版本，产物登记走本仓资产服务内部 API（不直连 DB、不落 core）。
- **④ 约束与非目标**：不实现第二个编排者（ADR-023）；不放宽 `ValidateImports`；不在本单做 promote 闸门（那是 QT-32）；**不在 agent 仓实现编译器**（D 已裁），不新增第 13 个仓。
- **⑤ 验收**：一份 Go 子集策略源码经本管线产出 artifact 且 `artifact_hash` 可离线复现；编译失败可检索；五道门对表文档归档。
- **⑥ 边界**：只做编译链路；资产存储/解析是 QT-41；AI 生成闭环是 QT-32。

> **🔶 开工前置状态（2026-10-22）**：M8 对表已覆盖本单 —— `docs/api-schema.md` §6.3 把**五道门逐项对表**（Resolve → Fetch → Verify → **Compile** → Mount），并订正了 ③-⑤ 那句「Resolve/Fetch/Verify/Mount」的四项写法（源文是五项、Compile 在 Mount 之前；见 §6.5 偏差 #2，不改写工单正文，以 §6.3 为准）。
> 本单的门禁结论：**编译管线今天不存在**（无 `cmd/compiler`、本机无 tinygo/wat2wasm、`staticcheck.CheckOrError` 返回匿名 error 不可检索），
> 而 agent 侧 `pipeline.go:634 validateCompilation` 只是临时目录 `go build` 的**检查**、不产出字节 ⇒ **产不出 `artifact_hash`**。
> 所以 QT-41b 的第一条产出物不是编译器，是**错误语义进 contracts**（③-④）——那要等 QT-05。D 裁决（本仓 `cmd/compiler`，不建第 13 仓）已由 ADR-027 §2.6.1 的修订承接，见 `tellurion-docs:docs/adr/adr-035-repo-inventory-and-naming.md` 附录 D。

> **施工记录（2026-10-23）**：
>
> **已交付**：
> - `internal/compile`：`Runner` 执行器 —— toolchain 名/版本/路径固定、`Args` 必须含精确 `-target=wasm-unknown`
>   （near-miss 如 `-target=wasm-unknown-extra` 明确拒绝）、`EnvAllowlist` 白名单（父进程其余环境变量一律不传）、
>   超时、`-o` 产物读取、64 KiB 日志截断、编译失败/无产物/产物超限各自 fail-loud。
> - `internal/compilerjob`：`Job`（strategy_id / version_hash / carrier / abi_version / scope / entry / source）严格校验 →
>   调 `Runner` → 算 `artifact_hash` → `POST /internal/strategies/{id}/artifacts` 回填（base64 字节 + hash 自证 + imports 自证）
>   → 校验服务侧回显 hash 与服务端校验结果。
> - `cmd/compiler`：读 job JSON（严格解码）、`-asset-url` / toolchain 参数、stdout 输出 JSON 回执。
> - 与 QT-41 的分界：编译服务**不直连 DB**，只经内部 HTTP 端点登记（③-⑥ 要求）；五道门里的 Compile 在 Verify 之后、Mount 之前。
> - 测试：`internal/compile/compile_test.go`（`TestMain` 里 `go build` 出假工具链，**不是 Skip**；env 泄露红线、超时、
>   无产物、超限、日志截断、`-target` near-miss）、`internal/compilerjob/compilerjob_test.go`（真 Service + httptest 的
>   成功/失败/服务撒谎/不可达）。
>
> **未落项（2026-10-24 更新）**：
> - ✅ **真 TinyGo 镜像**：官方 `tinygo/tinygo:0.34.0`（二进制 sha256 `1440831…`）已接；`internal/compile/toolchains.lock`
>   灌真实值；真镜像编译两次逐字节一致，`artifact_hash=fd0ecc26…`；证据 `docs/evidence/QT-41b-tinygo-0.34.0.md`。
>   同时修掉一个可复现性缺陷：TinyGo 默认把源文件绝对路径写进 DWARF，Runner 的随机临时目录会让同源码产出两个 hash；
>   现 `compile.Config` 对 `name=tinygo` 强制 `-no-debug`。
> - ✅ **编译失败的错误族进 contracts（③-④）**：contracts `v0.4.0`（2026-10-24）已发布四个哨兵；本仓同日升 pin 并改为同名别名，
>   见下面 G5 ② 施工记录。
> - ✅ **编译 job 的队列 / 重试 / 并发编排**：2026-10-24 G5 已落，见下条施工记录。
>
> 工具链锁形态：`internal/compile/lock.go` 的 `toolchains.lock`（名字+版本+二进制 sha256；`cmd/compiler -toolchain-lock` 必填，
> 无锁不编译、二进制被换即拒）；真镜像灌数时把它的 sha256 写进部署侧锁文件即可。
> 另：上一批提交（`832764b`）漏了 `go.mod` 的 testify `require`——新 checkout 的 `go test` 必红，本批 tidy 修正并补 AGENTS.md §4 登记。

> **施工记录（G5，2026-10-24）**：派工三条（job 队列/重试/并发 → 错误族消费 → `version_hash` 对齐），**① ✅ ② ✅ ③ ✅**（③ 在 contracts `v0.5.0` 发布后同日收编）。
>
> **① ✅ 编译 job 的队列 / 重试 / 限并发**
> - `internal/compilerjob/queue.go`：`Queue` 批次语义（Submit 全部作业 → `Run` 由 `Workers` 个 worker 取队首执行 → 全部终态后返回）。
>   状态常量 `StateQueued/Running/Succeeded/Failed` **直接取 `asset.Job*`**——与 `strategy.strategy_build_jobs.state` 的
>   CHECK 是同一套词表，钉子是 `TestQueueStatesMatchBuildJobTable`（同时读 DDL 断言四个取值，任一侧漂移即红）。
> - 重试：`MaxAttempts`（≤0 视为 1）+ `Backoff`（可被 ctx 打断）+ `DefaultRetryable` —— `ErrCompileFailed` / `ErrArtifactFetchFailed`
>   等可重试；作业形状、源码/产物超限、hash 不符、服务未配编译器、服务撒谎**立即终态**，不拿重试掩盖确定性失败。
> - 失败可检索：`Record(job_id)` / `Records()` 返回快照，终态 `failed` 带 `error`（含 toolchain 与日志片段）、`attempts`、起止时间。
> - `Job.JobID`（可空）：资产服务 `strategy_build_jobs` 的 job_id 可作记录主键，终态按同一个 id 检索；**编译进程仍不直连 DB**。
> - `cmd/compiler`：`-job` 改为可重复（一批 job，一个坏 job 不再带走整批）、`-workers`（限并发）、`-max-attempts`、
>   `-retry-backoff`、`-report <path>`；stdout 输出 `{succeeded, failed, jobs[]}` 报告；有作业终态失败即非零退出。
>   批次 ctx 预算按 `ceil(N/workers) × maxAttempts × (timeout+backoff) + 1min` 计算（此前固定 `timeout+1min`，多 job 会被提前掐断）。
>
> **② ✅ 错误族消费（同日补落：v0.4.0 推上远端后立即做）**：G0-C2 的 contracts `v0.4.0`（远端 tag `11a9683`）含
> `ErrCompileFailed` / `ErrCompileUnavailable` / `ErrSourceTooLarge` / `ErrArtifactTooLarge`。
> `go get ...@v0.4.0` + `go mod tidy` 后，`internal/asset/errors.go` 删掉四条本地 `errors.New`，改为**同名别名**
> （文件末尾逐条写映射：契约哨兵 ⇄ 本仓别名 ⇄ HTTP wire code）；`internal/asset/errors_test.go` 的
> `TestCompileErrorFamilyIsContractAlias` 钉住「同一个值 + 文案一致 + `%w` 包装后可用别名判定」。
> **HTTP wire code 零变化**（`compile_failed` / `compile_unavailable` / `payload_too_large` 原样）；`docs/api-schema.md` §2 补了哨兵来源。
> 所有调用点（`internal/compile` 的包装、`httpapi.classify`、`compilerjob.DefaultRetryable`）仍写 `asset.Err*`，行为不变。
> **③ ✅ `version_hash` 收编 contracts（2026-10-24，契约 v0.5.0 发布后）**：**裁-01 = 6 项**
> `strategy_id + carrier + abi_version + scope + entry + source`（不含 `capability`、不含 `params_defaults`），
> 契约腿已随 `v0.5.0` 落地（`StrategyVersionInput` + `DomainStrategyVersionHash`，导出面 443→447；本仓 pin 见 `31e613c`）。
> 收编落点：`internal/asset/hash.go` **删掉本地 `DomainStrategyVersionHash` 与 `VersionInput`**，
> `VersionHash` 改收 `contracts.StrategyVersionInput`，编码直接委托 `in.Hash()`（本仓只保留校验与错误语义，
> 不再抄 canonical 段）；`internal/asset/service.go` 的 `PutSource` 调用点同步。**HTTP / DB 行为零变化**。
> **域标签逐字节未变**（`tellurion.strategy_version_hash.v1`），历史 `version_hash` 全部仍然有效。
> 钉子：`TestVersionHash_MatchesContractGoldenVector` —— 黄金向量 `0e6ad536…` **抄自 contracts 的
> `TestGoldenVector_StrategyVersionHash`**（不是本仓现算），并直接与 `contracts.StrategyVersionInput.MustHash()` 对拍；
> `TestPutSource_ProducesContractHash` 另钉住**服务调用点**的六项映射（定义带 `capability`，证明它没被顺手塞进 hash）。
> 原六项敏感性 / 非法输入用例改用契约类型后照绿。
> **破坏验证**：把 `return in.Hash()` 改成「本仓抄一份编码 + 域标签写 v2」→ `TestVersionHash_MatchesContractGoldenVector`
> 红（`29bd0931… vs 0e6ad536…`，EXIT=1）；逐字节还原后 `internal/asset/hash.go` 的 sha256 与改前相同（`055a00f1…`）并复绿。
> **验收 grep**：`internal/asset/hash.go` 里**没有** `const DomainStrategyVersionHash`、也没有 `type VersionInput`（两个本地定义都已删除；文件里只剩对 `contracts.StrategyVersionInput` 的引用）。
> 门禁：`gofmt -l .` 空 + `go build ./...` + `go vet ./...` + `go test ./... -count=1` 全绿
> （无 `STRATEGY_TEST_DSN` / 真工具链镜像时，PG 与真镜像腿按设计 skip，不是通过）。
>
> **测试（本机实测）**：`go build ./... && go vet ./... && go test ./... -count=1` 全绿；
> `STRATEGY_TEST_DSN=... go test ./internal/asset -count=1`（PG 集成）全绿，EXIT=0。
> 新增用例：`internal/compilerjob/queue_test.go`（第一次失败第二次成功 / 超限终态失败 / 不可重试立即失败 /
> Workers=2 并发上限与提交顺序 / 状态词表对表 / 外部 job_id 与重复拒绝 / Run 后提交拒绝 / 坏作业不入队）、
> `cmd/compiler/main_test.go`（批量全成功 / 一失败不带走整批且报告可检索 / `-max-attempts 1` 关重试）、
> `internal/asset/errors_test.go`（四条编译·资产哨兵是契约别名，不是第二份定义）。
> **破坏验证**：把 `queue.go` 的 `attempt <= q.opts.MaxAttempts` 改成 `attempt <= 1`（关掉重试）→
> `go test ./internal/compilerjob` **EXIT=1**（`TestQueue_RetrySecondAttemptSucceeds` / `TestQueue_ExhaustsAttemptsAndFailsTerminal` 红）、
> `go test ./cmd/compiler` 红（`TestRunKeepsBatchAliveWhenOneJobFails`：attempts 期望 2 实得 1）；逐字节还原后两包复绿（EXIT=0）。
> ② 的破坏验证：把 `ErrCompileFailed` 别名改回本地 `errors.New(...)` → `TestCompileErrorFamilyIsContractAlias`（asset）与
> `TestClassifyCompileSentinelsFromContracts`（httpapi，契约错误掉进 500 internal）双红，EXIT=1；逐字节还原后复绿（EXIT=0）。
>
> **余项（2026-10-25 更新）**：~~队列记录与 `strategy_build_jobs` 的跨进程状态回写~~ —— **✅ 已由 QT-41c 收口**：
> 内部端点 `POST /internal/strategies/{id}/builds/{job_id}` + `cmd/assetservice -compiler-bin` 装配外部编译进程 +
> `internal/compilerclient` 派发 + `compilerjob.Queue` 上报 running/终态。`POST /api/strategies/{id}/build` 不再恒 503
> （装配后 202；未装配仍是 503 `compile_unavailable`，与编译失败可区分）。本单其他非目标不动：`storage='url'` 分支、
> `carrier=external` 的 hash 语义单独立文、core 侧 Mount 门。

### QT-41c · X2 编译链路跨进程闭环（状态回写 + assetservice 装配）[P1] ✅（2026-10-25 收口 QT-41b 唯一余项）

- **① 目标**：编译链路跨进程闭环——`POST /api/strategies/{id}/build` 返回 202 + `job_id`；独立 `cmd/compiler`
  进程执行编译并经内部端点回写 `running` / 终态；`GET .../builds/{job_id}` 能看到 `queued → running → succeeded | failed`
  全程；失败带机器可读 `error_code`；未装配编译器时仍是 503 `compile_unavailable`，与「编译失败」可区分。
- **② 上下文**：QT-41b 的余项（队列记录与 `strategy_build_jobs` 跨进程状态回写、`Options.Compiler` 未装配）；
  契约 pin `contracts v0.5.0`（编译四哨兵不改）。
- **③ 要求**：内部状态回写端点（归属校验 / 状态机 / succeeded 必须指向同 `(strategy, version, toolchain)` 的
  已登记 artifact / failed 的 `error_code` 缺省 `compile_failed`）；`BuildJob.error_code` + 可重入 ALTER；
  可选 `BuildDispatcher` 派发面；`compilerjob.StatusReporter` 与 `Queue` 上报；`internal/compilerclient` 进程派发；
  `cmd/assetservice -compiler-bin` 装配；HTTP wire code 变化同步 `docs/api-schema.md` §2/§6。
- **④ 约束与非目标**：不改 contracts；不建真 TinyGo 测试（用 fake ProcessRunner / fake 编译进程）；
  不改 `Compiler` 既有语义（派发是**可选**接口）；`storage=url`、`carrier=external`、core Mount 门不动。
- **⑤ 验收**：httpapi 内部端点全路径（running/成功/failed/跨策略 404/非法转移/缺 artifact 409/同终态幂等）；
  asset 派发不跑本地 Compile、派发失败 503 + job failed；compilerjob 上报 running/终态与四类归因码、空 job_id 不上报；
  compilerclient 捕获 job JSON 与全套参数、启动失败 503、非零退出补报 failed；cmd/assetservice 装配在空/齐/缺项三态正确。
- **⑥ 边界**：只做资产服务 ↔ 编译进程这一条边；不直连 DB、不 promote。

> **完成记录（2026-10-25）**：
>
> **落点**：
> - `internal/asset/service.go`：`BuildJob.ErrorCode`（`json:"error_code,omitempty"`）、`BuildJobReport`、
>   四个 `ErrorCode*` 常量、可选 `BuildDispatcher` / `DispatchRequest`、`ReportBuildJob`（归属 / 状态机 / artifact 校验）、
>   `SubmitBuild` 派发分支（派发失败把 queued 收口为 `failed` + `compile_unavailable`，端点仍 503）。
> - `internal/asset/ddl.go`：末尾一条可重入
>   `ALTER TABLE strategy.strategy_build_jobs ADD COLUMN IF NOT EXISTS error_code TEXT NOT NULL DEFAULT ''`；
>   已落地语句未改。`internal/asset/pgstore.go` 的 Save/Update/Get 同步 `error_code`。
> - `internal/httpapi/api.go`：`POST /internal/strategies/{id}/builds/{job_id}`（复用既有 wire code：
>   `job_not_found` / `invalid_transition` / `artifact_invalid` / `invalid_request`；**无新增码**）。
> - `internal/compilerjob/status.go`（`StatusReporter` + `ErrorCodeFor`）、`compilerjob.go`（`Config.Reporter`）、
>   `queue.go`（首次尝试前上报 running、终态上报；running 上报失败按可重试处理且不占编译尝试次数；
>   终态上报失败只记日志、不改判）。
> - `internal/compilerclient/`：实现 `asset.Compiler` + `asset.BuildDispatcher`；写临时 job JSON（含 `job_id`），
>   启动 `cmd/compiler`，传齐 `-job/-asset-url/-toolchain-*/ -memory-limit/-timeout/-workers 1/-max-attempts/-retry-backoff/-arg/-env`；
>   `ProcessRunner` 可注入；启动失败映射 `ErrCompileUnavailable`；进程非零退出先查服务侧状态、未终态才补报 failed（幂等）。
> - `cmd/compiler/main.go`：装配 `StatusReporter`（用已有 `-asset-url`）。
> - `cmd/assetservice/main.go` + `compiler.go`：`-compiler-bin`（空 = 不装配）+ asset-url / toolchain 五参 /
>   timeout / max-attempts / retry-backoff / repeatable `-arg`、`-env`；缺项启动期 fail-loud；启动日志区分
>   `external (cmd/compiler)` 与 `disabled`。
> - 文档：`db-schema.md`（列 + ALTER 迁移）、`api-schema.md`（内部端点 / 状态机 / error_code / §6 更新）、
>   `dataflow.md`（真实跨进程链路）、`ARCHITECTURE.md`（包布局 + 依赖方向）、`AGENTS.md` §1。
>
> **验收证据（本机实测）**：
> - 新测试：`internal/httpapi/report_test.go`（全路径 4 个用例）、`internal/asset/dispatch_test.go`（派发不本地编译 /
>   派发失败收口 / 状态机表）、`internal/compilerjob/reporter_test.go`（StatusReporter wire、四类归因码、
>   Queue running/终态、空 job_id 不上报、running 上报重试不占尝试、终态上报失败不改判）、
>   `internal/compilerclient/compilerclient_test.go`（job JSON + 全套参数 / 启动失败 / 非零退出补报 / 已终态不补报）、
>   `cmd/assetservice/compiler_test.go`（装配三态）。
> - 门禁：`gofmt -l .` 空、`go build ./...`、`go vet ./...`、`go test ./... -count=1` 全绿（`cmd/compiler` 的
>   fake 工具链用例真实 `go build`，不是 Skip；PG / 真 TinyGo 镜像腿无 `STRATEGY_TEST_DSN` / 镜像时按设计 skip）。
> - **破坏验证**：把 `SubmitBuild` 的 dispatcher 分支临时改成 `ok && false`（强制走本地 `runBuild`）→
>   `go test ./internal/asset -run 'TestSubmitBuild_' -count=1` **EXIT=1**，红在
>   `TestSubmitBuild_DispatchesWithoutLocalCompile: DispatchBuild calls = 0, want 1` 与
>   `TestSubmitBuild_DispatchFailureMarksJobFailed: err = <nil>, want ErrCompileUnavailable`；逐字节还原后同命令 **EXIT=0**。
>
> **如实登记**：`storage='url'`、`carrier=external` hash 语义、core 侧 Mount 门仍是非目标/跨仓；本次未新增 ADR
> （只在 `cmd/assetservice` ↔ `internal/compilerclient` 之间加了一条既有端口方向内的边，无实质分层变化）。


---

> **⚠️ 开工前置（refactor-plan-review M5/M8，2026-10-20 登记；2026-10-22 本仓 M8 已做完）**：本表任何单开工前，必须先过 core `tellurion:docs/TASKS.md`「五、X 线」的**公共前置**两条：① 建仓/升格前先与 ADR-027 §2 对账（12 仓是上限/当前规划，不是承诺终态）—— ✅ **M5 已结案**，结论在 `tellurion:docs/design/qt-decoupling/repo-inventory.md` §1.2（六仓 ≠ 六进程，`strategy` 在服务拓扑里**有位子**，容器升格仍需一条 ADR）；② 本仓设计文档先写「**接口对表**」小节（签名 + 错误语义 + 版本策略）—— ✅ **M8 已落**，见本仓 [`api-schema.md`](api-schema.md) §6（9 端点 + 五道门逐项对表，附七条前提偏差）。**未对表不开工**这条对本仓两张单已解除。
> 模板：`tellurion:docs/design/qt-decoupling/data-service-contract-and-ownership.md` §4.1。
> ~~模板见 `target-module-topology.md` UnifiedDataPoint 节~~ —— 那是**幻影节名**（core 全文 grep `UnifiedDataPoint` 在该文件 0 命中），2026-10-22 改指 §4.1。正文以彼处为准，此处只留指针、不镜像。

## 施工序（候选单，开工前必须展开成六字段）

> 这张表是**施工顺序**，不是任务台账：每条在执行前要展开成六字段单（目标/上下文/要求/约束/验收/边界），并写明冻结契约的路径与节号。
> 顺序有依赖，**不要跳级**：跳序的代价是返工（例：X1a 的协议没冻结就写 aggregator，等于把协议写死在一个服务里）。
> 2026-10-21：候选单已展开为「一、本仓任务」的正式六字段条目（表中「上游单号」指向的 QT 单正文现在就在本仓）。

| 阶段 | 内容 | 上游单号 | 前置 |
|---|---|---|---|
| 腿1 | DSL 资产化：`carrier=dsl` + `version_hash` + `strategy_ref` 全链路（零新工具链） | `QT-41` | 本仓立项 |
| 腿2 | WASM PoC → go/no-go | `QT-30` | 腿1 |
| X2 | 写侧 7 端点 + 读侧 2 端点 + 五道加载校验 | `QT-41` | 腿1 |

## 二、跨仓指针（不在本仓，勿双写）

| 单号 | 主题 | 在哪 |
|---|---|---|
| QT-41 | X2 策略资产服务（定义/版本/artifact/resolve） | 已迁入本仓「一、本仓任务」（2026-10-21；core `tellurion:docs/TASKS.md` 留同号指针） |
| QT-41b | X2 策略编译服务（Go 子集 → artifact 管线） | 已迁入本仓「一、本仓任务」（2026-10-21；core `tellurion:docs/TASKS.md` 留同号指针） |
| **QT-32（闸门腿）** | **promote 闸门的审批留痕** —— **裁决（2026-10-25，主任口径）⑭**：**审批人身份取 JWT claims 的 `sub`**；**闸门落在 agent 仓**、本仓只保留状态机。**本仓侧的落地动作**：① `POST /api/strategies/{id}/versions/{hash}/promote` 增**必填**审批人入参（**不许 caller 自报身份** —— 沿用 risk 仓 `Actor` 字段用 `json:"-"` 防自报的做法，或直接从 JWT claims 取）；② 落库进 `compile_log`（追加式，已有机制）或新开 `strategy.strategy_promotions` 审计表，留痕三件套：**审批人 / 时间 / 结论**。**依据（实测）**：本仓全仓 `approved_by\|approver\|audit` = **0 命中**（无审批人字段、无独立审计表）；`internal/asset/pgstore.go` 的 `Promote(...)` 的 `log` 参数在 `internal/asset/service.go` 调用处**硬编码为 `"promoted"`** ⇒ 落进 `strategy.strategy_artifacts.compile_log` 的只有「结论」，**没有「谁批的」**。**不改**本仓 promote 端点的既有路径、状态机（`StatusValidated → StatusPromoted`）与 `cache.InvalidateLatest(id)` 同刻失效。裁决全文见 `tellurion:docs/design/qt-decoupling/cross-repo-decision-record-2026-10-25.md` ⑭。**⚠️ 2026-10-25 18:45 实测订正（这条的前提被推翻，本仓侧降级为「只出决策材料」）**：本仓**零鉴权**——全仓 `JWT_SECRET\|Bearer \|Authorization\|RequireRole\|Roles\b` = **0 命中**、无任何 auth/role/guard/middleware 文件、`go.mod` **无 `tellurion-kit`** ⇒ kit auth 没进这个仓；`internal/httpapi/api.go:42-56` 的 **12 条路由全部无保护**；且 core 全仓 `grep "/promote"` = **0 命中** ⇒ **今天既没有鉴权、也没有调用方** ⇒ **「从 JWT claims 取审批人」在本仓没有承载体**。**本仓侧第一步改为出决策材料**（面 A 给本仓引入 kit auth / 面 B core 转发 / 面 C 独立管理面 + 各自代价），由协调者裁完才谈施工 | `tellurion:docs/TASKS.md` QT-32 |
| QT-30 | M0 市场级状态机 PoC（决定 artifact 形态） | `tellurion:docs/TASKS.md` |
| QT-08 | 本仓 CI / AGENTS / 台账由该单一并补齐 | `tellurion:docs/TASKS.md` |
| SPEC-R-02 | 五张无主表 owner 认领——`quant.external_signals` 不属本仓，owner = core strategy-runtime（已补 `docs/db-schema.md` §1 不属本仓行） | `tellurion:docs/TASKS.md` §九 |

---

## 维护本文件

- **新增单**：写全六字段；六字段里必须显式引用**冻结契约的路径与节号**（契约仓的包路径、设计文档的节号），不许写"参考相关实现"。
- **派工前先证伪前提**：读码核对工单写的前提；不符就停下回报 + 给拆法，不硬做、不扩范围。
- **跨仓引用写 `repo:path`**（如 `tellurion:docs/TASKS.md`），不做可点击相对链接。
- **顺序纪律**：同一波次内撞同一批文件的单按单号串行，一单一 commit。
