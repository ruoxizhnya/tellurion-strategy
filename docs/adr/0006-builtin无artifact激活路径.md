# 0006 · builtin 无 artifact 激活路径

- **状态**：已裁决（2026-11-12，主任；supersedes 2026-11-10「无 artifact 载体一律不 promote」对 builtin 的适用部分）
- **背景**：业务面容器实测（`127.0.0.1:8090`，真请求）暴露一条生命周期断头：

  ```
  POST /api/strategies {"carrier":"builtin",...}        → 200 {"id":"st_a19..."}
  PUT  /api/strategies/{id}/source                      → 200 {"version_hash":"09364fc1..."}
  GET  /api/strategies/{id}/versions                    → 200 []
  GET  /api/strategies/{id}/resolve?ref=latest          → 404 no_active_version
  POST /api/strategies/{id}/versions/{hash}/validate    → 404 artifact_not_found
  POST /api/strategies/{id}/build                       → 400 unsupported_carrier
  POST /api/strategies/{id}/versions/{hash}/promote     → 400 unsupported_carrier
  ```

  builtin 能建、能写源，但**永远拿不到 active 版本**：`ai.generation_chain` 若登记 builtin，就卡死在这。
  根因是 2026-11-10 的裁决「无 artifact 载体没有可审批产物，一律不 promote」把 builtin 与 dsl / external
  当成同一类。但三者的失败面不同：builtin 的实现**编译在本仓进程的二进制里**，运行时按 `entry` 名解析；
  dsl / external 在本仓没有运行时实现。旧裁决对前者是误伤，对后者才是正确的 fail-closed。

- **决策（Decision）**：
  1. **builtin 是「内置载体指针」，不产 artifact 是它的本质，不是缺陷。** 创建定义不因此被拒。
  2. **`validate` 对 builtin**：校验定义与源的结构契约——`entry` 是合法符号名（`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`）、
     版本记录与定义形状一致（`carrier` / `abi_version` / `scope` / `entry` 不漂移）、`capability` 闭合（无空白项 / 重复项）、
     source 是合法 UTF-8 且非空（builtin 的 source 是登记指针，不是可执行代码）。通过后回 200 产物视图，
     `artifact_hash=""`、`status=validated`。**不要求 artifact。**
  3. **`promote` 对 builtin**：允许把某个版本标为 active，只写 `active_version_hash`，
     `active_artifact_hash` 保持空；响应明确 `carrier=builtin`、`artifact_hash=""`、`status=promoted`，
     并写进审计（`strategy.strategy_promotions` 的 `artifact_hash` 列为空串）。
     语义 = 「无 artifact，运行时按 entry 名解析内置实现」。
  4. **`resolve?ref=latest` 对 builtin**：回 `carrier=builtin`、`entry`、`version_hash`、`status=promoted`，
     `artifact_hash=""`，不返回 404。`ref=<hash>` 只接受**当前 active 版本**的 hash，其余版本 404 `no_active_version`。
     消费方的判据从「有 artifact_hash」改为「`status=promoted` 且 `carrier` 明确」。
  5. **`build` 对 builtin 仍拒绝**（它确实不该产 artifact），但文案改为
     `carrier builtin needs no build; use validate then promote (runtime resolves the in-binary implementation by entry %q)`。
  6. **`dsl` / `external` 一行不放宽**：validate 仍 404 `artifact_not_found`、promote 仍 400 `unsupported_carrier`、
     resolve latest 仍 404 `no_active_version`。
  7. **DB 约束随之放开一格**：追加迁移把 `strategy_definitions` 的
     `CHECK ((active_version_hash IS NULL) = (active_artifact_hash IS NULL))` 换为具名约束
     `strategy_definitions_active_ck`，在原有两支之外允许
     `active_version_hash IS NOT NULL AND active_artifact_hash IS NULL AND carrier = 'builtin'`。
     清空（deprecate）仍要求两者一起清；wasm / so 仍要求两者同时出现。

- **理由（为什么是它而不是别的）**：
  - **「没有一个字节」不等于「不能执行」**：builtin 的执行体在二进制里，`carrier=builtin + entry + status=promoted`
    就是一条可执行的完整解析；`artifact_hash=""` 是契约的一部分，不是缺字段。把它读成假绿灯，是把
    wasm 的产物模型硬套到内置载体上。
  - **`wasm|so` 的闸门没有松动**：它们仍必须 build → artifact → validate（真字节/imports/体积/内存）→ promote；
    本轮改动只在 `CarrierBuiltin` 分支内生效，`NeedsArtifact()` 的语义与相关代码路径未变。
  - **dsl / external 必须继续关门**：本仓没有它们的运行时，回一个空 `artifact_hash` 才会让回测入口拿不到可执行体，
    那才是真正的假绿灯。三类无 artifact 载体按「有没有本仓运行时」而非「有没有 artifact」分类。
  - **validate 结论不落库，所以 promote 必须重跑结构契约**：builtin 没有 artifact 行可承载 `validated` 状态，
    依赖「先 validate 过」的隐式状态会在进程重启 / 多实例下消失。promote 内重算一遍是确定性的，代价可忽略。

- **对 2026-11-10 裁决的反驳**：旧结论「审批态是产物级状态机，没有字节码就没有被批准的可执行资产」成立的前提是
  「可执行资产 = 本仓产出的字节码」。对 builtin，可执行资产是**二进制内的注册项**，`entry` 就是它的地址；
  前提不成立，结论不能直接套用。旧裁决保留对 dsl / external 的适用性——它的举证责任（本仓能否执行）在那两类上仍成立。

- **否决的替代方案**：
  1. **给 builtin 造一个占位 artifact（空字节 / 伪 hash）**：污染内容寻址语义，`artifact_hash` 将不再可信，
     且 `GET /versions/{hash}/artifact` 会吐出伪造字节。否决。
  2. **builtin 也走 build，产出「注册表快照」字节**：本仓不消费该字节，纯为满足状态机而产文件，属仪式性产物；
     还会让 `strategy_artifacts` 出现无运行意义的行。否决。
  3. **保持 400，要求调用方把 builtin 改登记为 wasm**：等于要求内置策略先编译成 wasm，改变载体分层与部署形态；
     与 `ai.generation_chain` 的登记语义冲突。否决。
  4. **只放开 resolve、仍拒 promote（读时兜底）**：`active_version_hash` 永远为空，resolve latest 只能靠猜；
     且审计里没有「谁批过哪个内置版本」。否决。

- **代价 / 后果（Consequences）**：
  1. **`resolve` 的消费判据变了**：core 回测入口不能再用「`artifact_hash` 非空」当可执行判据；必须
     `status == promoted`，并在 `carrier == "builtin"` 时不取字节、直接按 `entry` 找内置实现。
     `ResolveResult` 因此新增 `entry` 字段（加法变更，旧消费方忽略即可）。
  2. **`GET /versions` 不反映 builtin 的激活态**：列表状态由 artifact 行派生，builtin 没有 artifact 行，
     仍显示 `status="source"`。生效版本以 `resolve` / definition 的 `active_version_hash` 为准，不要用列表判断。
  3. **promote 审计的 `artifact_hash` 为空串**：`strategy_promotions.artifact_hash` 是 `CHAR(64) NOT NULL`，
     builtin 行写入空串（PG 会补空格）。查审计时按 `version_hash` 关联，不要假设 `artifact_hash` 有值。
  4. **约束名是 `strategy_definitions_active_ck`**：旧库启动时由可重入 `DO` 块删掉自动命名的旧 CHECK 再建新约束；
     新库同样走这条追加迁移。手工改过约束名的库需要先对齐，否则会出现两条并存约束。
  5. **机械检查**：`internal/httpapi/builtin_lifecycle_test.go`（`TestBuiltinLifecyclePromotesWithoutArtifact`
     全链、`TestArtifactlessNonBuiltinCarriersStayClosed` 关门）、`internal/asset/builtin_activation_test.go`
     （`TestValidateBuiltinActivation` 结构契约表）、`internal/asset/asset_test.go`
     （`TestService_PromoteArtifactlessCarriers` 按载体分类）、`TestAPI_ErrorWireCodes`（dsl 仍 400）、
     `TestMigrations_PinTheAdjudicatedShape`（新约束必须出现在迁移里）。
  6. **实测（2026-11-12，真二进制 `cmd/assetservice` + curl，memory store，`127.0.0.1:18090`）**：
     create **201** → source **200** → validate **200**（`status=validated`、`artifact_hash=""`、`carrier=builtin`）→
     promote **200**（`status=promoted`、`artifact_hash=""`、`carrier=builtin`）→ resolve latest **200**
     （`entry=momentum`、`status=promoted`）→ build **400**（`unsupported_carrier`，文案含 `needs no build` /
     `validate then promote`）。
