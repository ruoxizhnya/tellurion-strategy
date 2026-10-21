---
status: active
last-verified: 2026-10-21
verified-by: 任务台账批量重分布（X 线单展开：QT-41/QT-41b 迁入）
---

# TASKS — tellurion-strategy 未完成项

> 本仓**唯一**任务台账。跨仓任务只在 §二 留指针，**不镜像正文**（双写必然漂移）。
> 状态：`⬜ 待做` / `🔶 进行中` / `⛔ 阻塞（有未解除前置）` / `✅ 完成（附日期）`
> 每张单六字段（① 目标 ② 上下文 ③ 要求 ④ 约束与非目标 ⑤ 验收 ⑥ 边界），判据见 `tellurion:AGENTS.md` §8.5。
> 完成即标 ✅（附日期）并移入 `docs/archive/TASKS-YYYY-MM-DD-archived.md`；本文件只留未完成项。

---

## 一、本仓任务

> **约定变更（2026-10-21）**：原「占位仓不写正式六字段任务单」作废——X 线任务单已展开为本仓正式任务（见下）。M5/M8 开工前置不变；实现仍未开工，开工前仍须过前置两条。

### QT-41 · X2 策略资产服务（定义/版本/artifact/resolve）[P1] ⬜

> **迁入说明（2026-10-21）**：本单自 core 仓 `tellurion:docs/TASKS.md`「五、X 线」迁入（任务台账批量重分布：X 线单展开到各服务仓为正式六字段单，「占位仓不写正式任务单」旧约定同时作废）。六字段正文逐字保留、不改写；core 侧同号条目已改为指针。

- **① 目标**：策略真相源在服务侧，引擎只持可驱逐只读副本；`id@artifact_hash` 可离线复现。
- **② 上下文**：`strategy-asset-service-api.md` + `strategy-source-of-truth-and-cache.md`（D13/D14）+ QT-03 的 hash 体系；**未决 A（BYTEA vs 对象存储）——下文 ③-① 的 BYTEA 落地是「假设，需裁决后生效」**（阻塞单：本单，裁决见设计集 README 待裁决表 A 行；未裁决前不开工 ③-①）。
- **③ 要求**：① `strategies` 加列（carrier/abi_version/scope/capability/entry/`active_version_hash`/`active_artifact_hash`）+ `strategy_artifacts` + blobs（**假设，需裁决 A 生效后落地**：blob 载体按 BYTEA 假设写，A 若改判对象存储则本项重写）；② 写侧 7 端点、读侧 2 端点（resolve/artifact）；③ 缓存按 `artifact_hash`（永不失效）+ latest 的失效策略——**TTL vs 事件失效二选一属未决 F，未裁决前本项按「假设，需裁决后生效」处理，不得静默二选一**（阻塞单：本单，裁决见 README 待裁决表 F 行）；④ promote 不覆盖 artifact 行；⑤ 14 个内置策略登记为 `carrier=builtin`。
- **④ 约束与非目标**：策略服务**不算信号**；不改执行路径的"先查 artifact 再回退内置"在 QT-26 已定的顺序；不做对象存储（除非 A 改判）；**A/F 未裁决的假设项不许按假设值直接上 main**（裁决落地前相关子项保持 ⛔）。
- **⑤ 验收**：服务停机后已缓存 hash 的回测照跑且结果一致；同源码两工具链 → 两 artifact 可分别追溯；`id@version_hash` 解析结果回写实际 `artifact_hash`。
- **⑥ 边界**：只做资产与解析；编译服务是独立单 **QT-41b**（本单 ①~⑤ 不含编译链路）。

### QT-41b · X2 策略编译服务（Go 子集 → artifact 管线）[P1] ⛔（阻塞于待裁决 D：编译服务归属）

> **迁入说明（2026-10-21）**：本单自 core 仓 `tellurion:docs/TASKS.md`「五、X 线」迁入（任务台账批量重分布：X 线单展开到各服务仓为正式六字段单，「占位仓不写正式任务单」旧约定同时作废）。六字段正文逐字保留、不改写；core 侧同号条目已改为指针。

- **① 目标**：策略源码到 `artifact_hash` 产物的编译链路成文并落地（编译 → 校验 → 登记 artifact），AI 生成闭环（QT-32）与人工 promote 共用同一条管线。
- **② 上下文**：`strategy-asset-service-api.md` §4 五道门中的 Compile 门；`implementation-plan-mvp.md` §6-M2（AI 生成闭环含编译环节）；原 QT-41 ⑥ 曾以「QT-41b」悬空引用（refactor-plan-review H3），本单即其正式落点。**归属未定**：编译服务归策略资产服务（X2）还是独立能力，属待裁决 D（见设计集 README 待裁决表）；未裁决前本单 ⛔ 不开工。
- **③ 要求**：① Compile 门语义成文（toolchain 版本固定、env-only 白名单、产物 `-target=wasm-unknown`）；② 编译产物经 QT-03 的 `artifact_hash` 登记进 QT-41 的 `strategy_artifacts`；③ 同源码两工具链 → 两 artifact 可分别追溯（与 QT-41 ⑤ 对齐）；④ 编译失败的错误语义进 contracts 哨兵错误族；⑤ 与 QT-26 ③-⑤ 五道门的 Resolve/Fetch/Verify/Mount 分界逐项对表。
- **④ 约束与非目标**：不实现第二个编排者（ADR-023）；不放宽 `ValidateImports`；不在本单做 promote 闸门（那是 QT-32）。
- **⑤ 验收**：一份 Go 子集策略源码经本管线产出 artifact 且 `artifact_hash` 可离线复现；编译失败可检索；五道门对表文档归档。
- **⑥ 边界**：只做编译链路；资产存储/解析是 QT-41；AI 生成闭环是 QT-32。


---

> **⚠️ 开工前置（refactor-plan-review M5/M8，2026-10-20 登记）**：本表任何单开工前，必须先过 core `tellurion:docs/TASKS.md`「五、X 线」的**公共前置**两条：① 建仓/升格前先与 ADR-027 §2 对账（12 仓是上限/当前规划，不是承诺终态）；② 本仓设计文档先写「**接口对表**」小节（签名 + 错误语义 + 版本策略，模板见 `tellurion:docs/design/qt-decoupling/target-module-topology.md` UnifiedDataPoint 节），**未对表不开工**。正文以彼处为准，此处只留指针、不镜像。

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
