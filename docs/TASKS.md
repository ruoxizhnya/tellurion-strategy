---
status: active
last-verified: 2026-10-22
verified-by: QT-41/QT-41b 的 M8 接口对表落定（api-schema.md §6）+ 开工前置幻影模板指针改指 §4.1
---

# TASKS — tellurion-strategy 未完成项

> 本仓**唯一**任务台账。跨仓任务只在 §二 留指针，**不镜像正文**（双写必然漂移）。
> 状态：`⬜ 待做` / `🔶 进行中` / `⛔ 阻塞（有未解除前置）` / `✅ 完成（附日期）`
> 每张单六字段（① 目标 ② 上下文 ③ 要求 ④ 约束与非目标 ⑤ 验收 ⑥ 边界），判据见 `tellurion:AGENTS.md` §8.5。
> 完成即标 ✅（附日期）并移入 `docs/archive/TASKS-YYYY-MM-DD-archived.md`；本文件只留未完成项。

---

## 一、本仓任务

> **约定变更（2026-10-21）**：原「占位仓不写正式六字段任务单」作废——X 线任务单已展开为本仓正式任务（见下）。M5/M8 开工前置不变；实现仍未开工，开工前仍须过前置两条。

### QT-41 · X2 策略资产服务（定义/版本/artifact/resolve）[P1] ⬜（2026-10-21：A/E/F/G/H 五项已裁决，阻塞解除）

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

### QT-41b · X2 策略编译服务（Go 子集 → artifact 管线）[P1] ⬜（2026-10-21：D 已裁决，阻塞解除）

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
| QT-30 | M0 市场级状态机 PoC（决定 artifact 形态） | `tellurion:docs/TASKS.md` |
| QT-08 | 本仓 CI / AGENTS / 台账由该单一并补齐 | `tellurion:docs/TASKS.md` |
| SPEC-R-02 | 五张无主表 owner 认领——`quant.external_signals` 不属本仓，owner = core strategy-runtime（已补 `docs/db-schema.md` §1 不属本仓行） | `tellurion:docs/TASKS.md` §九 |

---

## 维护本文件

- **新增单**：写全六字段；六字段里必须显式引用**冻结契约的路径与节号**（契约仓的包路径、设计文档的节号），不许写"参考相关实现"。
- **派工前先证伪前提**：读码核对工单写的前提；不符就停下回报 + 给拆法，不硬做、不扩范围。
- **跨仓引用写 `repo:path`**（如 `tellurion:docs/TASKS.md`），不做可点击相对链接。
- **顺序纪律**：同一波次内撞同一批文件的单按单号串行，一单一 commit。
