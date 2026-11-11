# TASKS 归档 — tellurion-strategy（2026-11）

> **本文件是 `docs/TASKS.md` 的历史归档，进 git、不删除**。归档时间 2026-11-11；收录范围 = 截至 2026-11-10 已收口（✅）的任务原文与当时的施工记录，**逐字搬入、未改写**（原文件 frontmatter 留在新台账；任务标题与正文原样保留）。

> **检索方式**：按任务 ID `grep -n "<ID>" docs/archive/tasks-archive-2026-11.md`；按 commit `grep -n "<sha>" docs/archive/tasks-archive-2026-11.md`；索引表在本节下方，含 ID / 标题 / 收口日期 / commit / 证据链接。

## 检索索引

| ID | 标题 | 收口日期 | commit / 正文所记哈希 | 证据链接 |
|---|---|---|---|---|
| SVC-STRAT-G0 | 服务就绪门 G0：自有部署与本地启动（ADR-036） | 2026-10-26 | —（原文未记） | [原文](#SVC-STRAT-G0) |
| SVC-STRAT-G3 | core 消费与本地注册表退役（ADR-036） | 2026-11-10 | —（原文未记） | [原文](#SVC-STRAT-G3)；[ADR-0004](adr/0004-core消费边界与promote归属.md) |
| SVC-STRAT-G3-B | core 回测闸门的读面：resolve 回写审批态 + promote 审计读端点 | 2026-11-10 | —（原文未记） | [原文](#SVC-STRAT-G3-B) |
| QT-41 | X2 策略资产服务（定义/版本/artifact/resolve） | 2026-10-24 | —（原文未记） | [原文](#QT-41) |
| QT-41b | X2 策略编译服务（Go 子集 → artifact 管线） | 2026-10-22 | `832764b`、`11a9683`、`31e613c` | [原文](#QT-41b) |
| QT-41c | X2 编译链路跨进程闭环（状态回写 + assetservice 装配） | 2026-10-25 | —（原文未记） | [原文](#QT-41c)；[`api-schema.md`](api-schema.md) |
| SVC-STRAT-G1 | 服务就绪门 G1：鉴权 · 公开健康端点 · 错误模型 · 版本化（ADR-036） | 2026-11-07 | —（原文未记） | [原文](#SVC-STRAT-G1) |
| SVC-STRAT-G2 | 服务就绪门 G2：`strategy.*` 唯一 writer 与可重放迁移（ADR-036） | 2026-11-07 | —（原文未记） | [原文](#SVC-STRAT-G2) |
| SVC-STRAT-G4 | 服务就绪门 G4：编译链路失败语义 · 超时重试 · runbook（ADR-036） | 2026-11-10 | —（原文未记） | [原文](#SVC-STRAT-G4) |
| STRAT-GRACEFUL-SHUTDOWN | 资产服务无优雅停机，SIGTERM 直接掐断在途请求 | 2026-11-12 | —（本单 commit 见 `git log --grep=STRAT-GRACEFUL-SHUTDOWN`） | [原文](#STRAT-GRACEFUL-SHUTDOWN) |
| STRAT-AUTH-ADR | 鉴权姿态无 ADR，10 条公开端点的边界决定只散在 api-schema | 2026-11-12 | —（本单 commit 见 `git log --grep=STRAT-AUTH-ADR`） | [原文](#STRAT-AUTH-ADR)；[ADR-0005](adr/0005-鉴权姿态与端口隔离边界.md) |

---

## 原文归档（逐字）

# TASKS — tellurion-strategy 未完成项

> 本仓**唯一**任务台账。跨仓任务只在 §二 留指针，**不镜像正文**（双写必然漂移）。
> 状态：`⬜ 待做` / `🔶 进行中` / `⛔ 阻塞（有未解除前置）` / `✅ 完成（附日期）`
>
> **台账终扫（2026-11-10）**：任务标题带 `⬜` / `🔶` / `⛔` 的 = **0 条**；正文旧标记以标题与最新「收口」块为准。**无开放项**。
> 每张单六字段（① 目标 ② 上下文 ③ 要求 ④ 约束与非目标 ⑤ 验收 ⑥ 边界），判据见 `tellurion:AGENTS.md` §8.5。
> 完成即标 ✅（附日期）并移入 `docs/archive/TASKS-YYYY-MM-DD-archived.md`；本文件只留未完成项。

> **第三轮裁决指针（2026-10-25）**：⑫ promote 审批人 = **面 B1**：core 作唯一对外入口（JWT `sub` → 受信 `X-Actor` + service token）；本仓**只对 `/promote` + 两条 `/internal/*`** 引 kit `auth/httpadapter` 的 service-token 校验（= 本仓首次引 kit），`X-Actor` 缺失 400 `actor_required`，promote 落审计（人/时间/结论）；端口仅集群内/回环，其余 9 条靠端口隔离并在 `api-schema.md` 写明。正文见 `tellurion:docs/design/qt-decoupling/cross-repo-decision-record-2026-10-25.md` §五。

---


## 一、本仓任务

> **约定变更（2026-10-21）**：原「占位仓不写正式六字段任务单」作废——X 线任务单已展开为本仓正式任务（见下）。M5/M8 开工前置不变；实现仍未开工，开工前仍须过前置两条。

<a id="SVC-STRAT-G0"></a>
### SVC-STRAT-G0 · 服务就绪门 G0：自有部署与本地启动（ADR-036）[P0] ✅（2026-10-26）

- **① 目标**：资产服务与编译器都能独立构建、启动/执行；资产服务可被健康检查。
- **② 上下文**：ADR-036 定本仓为服务；今天有 `cmd/assetservice` + `cmd/compiler` + 5 张 `strategy.*` 表，但无 `Dockerfile` / `deploy/`，默认 `:8085` 与 analysis-service 撞号，且未引 kit 鉴权。
- **③ 要求**：新增 `Dockerfile`（资产服务）与 `Dockerfile.compiler`（批处理镜像）；新增 `deploy/compose.yaml`（`STRATEGY_DATABASE_URL`、artifact cache dir、端口 `:8090`）；**默认监听改 `127.0.0.1:8090`**（代码 + 文档 + ARCHITECTURE）；README 写单仓启动命令与 compiler 用法；健康检查用 TCP 探针（公开 `/healthz` 归 G1）。
- **④ 约束与非目标**：不改资产/编译逻辑；不接 core resolve/promote（G3）；不把编译器和资产服务合进程；不引入 K8s。
- **⑤ 验收**：`docker compose -f deploy/compose.yaml config` 通过；资产服务启动后 TCP `8090` 可连；compiler 镜像能处理一个 job JSON；`:8085` 不再被本仓占用；无 `replace` / `go.work`。
- **⑥ 边界**：只改本仓 deployment 与默认端口；core 侧消费由 `SVC-STRAT-G3` 跟踪。

> **施工记录（2026-10-26）**：
> - **落点**：`Dockerfile`（多阶段、非 root、`netcat-openbsd`、`HEALTHCHECK nc -z 127.0.0.1 8090`）、`Dockerfile.compiler`（批处理镜像，**无 healthcheck**）、`deploy/compose.yaml`（服务名 `strategy`、`STRATEGY_DATABASE_URL`、`/data/artifacts` 卷、`127.0.0.1:8090:8090`）；`cmd/assetservice/main.go` 默认 `-addr` 改 `127.0.0.1:8090`；`docs/ARCHITECTURE.md` 与 README 同步端口，README 补资产服务 / compose / compiler 启动与用法。
> - **验收（真实输出）**：`docker compose -f deploy/compose.yaml config -q` → exit 0；`config --services` → `strategy`；`go build ./...` / `go vet ./...` / `go test ./... -count=1` 全绿（7 包，`cmd/assetservice 0.157s`、`cmd/compiler 1.506s`）；`gofmt -l .` 空。
> - **破坏验证**：在 compose 临时给 `strategy` 加 `depends_on: missing-svc` → `config -q` 退出非 0（`service "strategy" depends on undefined service "missing-svc"`）→ 逐字节还原后复绿（exit 0，SHA256 一致）。
> - **审查修复（2026-10-26，容器启动即退出）**：首版镜像以 `USER nobody` 运行，但 `defaultCacheDir()` 走 `os.UserCacheDir()`，nobody 的 HOME 是 `/` ⇒ `assetservice: asset: cache dir: mkdir /.cache: permission denied`，进程启动即退；compose 的命名卷 `strategy-artifacts` 首次挂载为 root 属主，同样不可写。修复（只动 `Dockerfile`，不碰 `internal/asset`）：`RUN mkdir -p /data/artifacts && chown 65534:65534 /data/artifacts` + `ENV XDG_CACHE_HOME=/tmp/.cache`（不传 `-cache-dir` 时缓存落到人人可写的 `/tmp`）。实测：裸跑（默认缓存）与挂命名卷（`-cache-dir /data/artifacts`）两种形态 TCP `8090` 均通、`docker inspect` → `healthy`。
> - **如实登记**：`internal/asset` 业务逻辑未动；未引 `tellurion-kit`；公开 `/healthz` 仍未落（归 G1），故 G0 只能 TCP 探针；promote 鉴权缺口（`internal/httpapi/api.go:46`）**未修**——属 G3 / QT-32 闸门，不能在本轮自行引入 kit。

<a id="SVC-STRAT-G3"></a>
### SVC-STRAT-G3 · core 消费与本地注册表退役（ADR-036）[P0] ✅（2026-11-10 按已落边界重写验收收口，主任裁决 A）

> **2026-11-10 主任裁决 A（收口）**：按已落边界重写本单，不再要求 core 全量解析/注册/promote。边界 = **内置策略走 core 本地 registry（不联网）；非空 `strategy_ref` 走本仓 resolve + 审批闸门；promote 由 agent/操作者直连本仓，core 只读校验；core 本地 `strategies` 表与 DB 资产注册腿已删，运行时 `pkg/strategy` 保留**。成文见 [ADR-0004](adr/0004-core消费边界与promote归属.md)；ADR-036 §6 同步修订。
> - **⑫ 的解释**：`promote 走 core 网关 + X-Actor` 按**部署入口**解释（集群 ingress / 端口隔离），不是 core 代码级转发路由；core 当前无 `/promote` 路由。若将来要求代码级网关，另立单，不在本单验收。
> - **过期结论订正**：下方 2026-11-10 复核中「core 仍零调用」**已过期**——该复核 grep 只覆盖 `strategy-service|STRATEGY_URL|8089|strategyservice`，漏了 `strategy_asset.resolve_url`。K11 已于 2026-11-10 生产接线（core `cmd/analysis/wiring/strategy_asset.go`、`cmd/analysis/main.go:304`、`cmd/analysis/handlers_backtest.go:174`、`internal/orchestrationworker/executor.go:77`、`config/analysis-service.yaml:112`）：非空 ref 真实调用 resolve + promotions。
> - **本次仅文档/台账，无代码改动**；resolve 读面由 `SVC-STRAT-G3-B`（✅）与 core `QT-32`（✅）闭合。

- **① 目标**：core 内置策略走本地 registry（不联网）；非空 `strategy_ref` 走本仓 resolve + 审批闸门；promote 由 agent/操作者直连本仓并落审计，core 只读校验；core 本地 `strategies` 表与 DB 资产注册腿退役，运行时 `pkg/strategy` 保留。
- **② 上下文**：本仓 12 端点已实装；`SVC-STRAT-G1` 已闭合 `/promote` + `/internal/*` 鉴权与审计；`SVC-STRAT-G3-B` 已补 `ResolveResult.status` 与 promotions 读端点；core `SVC-CORE-STRAT-RETIRE`（2026-11-09 ✅）已删 `strategies` 表并定死本地 registry；core `QT-32`（2026-11-10 ✅）的 K11 闸门已生产接线。
- **③ 要求**：① 本仓 `/promote` + 两条 `/internal/*` 保持 kit service-token + `X-Actor`（已落）；② core 对非空 `strategy_ref` 的 resolve 与状态/审计校验保持生产接线（已落）；③ promote 由 agent/操作者直连本仓，core 不提供转发路由；④ 内置策略路径零网络、行为逐字节不变。
- **④ 约束与非目标**：不要求 core 提供 list-all / 注册代理；不恢复 core `strategies` 表；不改 `version_hash` 编码；不让 promote 依赖 core 在线；不把信号计算/回测搬进本仓；不做对象存储。
- **⑤ 验收**：① core `strategies` 表零写入且表已删（`SVC-CORE-STRAT-RETIRE` 计数 0）；② core 对非空 `strategy_ref` 的 resolve + 审批闸门真实生效（K11：错误面 + 真 assetservice 冒烟；空 ref 跳过）；③ core 无 `/promote` 路由；promote 直连本仓并落 `strategy_promotions` 审计；core 只读校验 `status=promoted` + 审计行；④ 内置策略路径不联网、行为逐字节不变；⑤ 旧 DB 注册表已删除（非只读）。
- **⑥ 边界**：本仓只保证资产面；core 代码删除/内置 registry 保留由 `SVC-CORE-STRAT-RETIRE` 跟踪；代码级 promote 网关若将来要求，另立单。

> **历史：⛔ 开工前置复核（2026-10-26 / 2026-11-10，已由上述裁决收口，仅留档）**：
> - **core promote 闸（QT-32）未开**：本仓 `/promote`（`internal/httpapi/api.go:46`）今天无鉴权、无审计；不会在闸门未定时先接 core。**（2026-11-10 订正：鉴权 + 审计已由 `SVC-STRAT-G1` 闭合——三条受保护路由走 kit service token、`X-Actor` 必填、`strategy_promotions` 落三件套；本仓侧不再是 blocker，剩余前置在 core 消费面。）**
> - ~~**`SVC-CORE-STRAT-RETIRE` 未立单**：core 本地注册表退役没有归属单，G3 只能停在「本仓就绪」。~~ → **已立单（2026-10-26 同日补）**：条目在 `tellurion:docs/TASKS.md:130`「六之二、退役执行单」，串行第 6，判据逐字抄 B 组材料 §4.3。G3 仍受「本仓就绪 + 退役单可派发」两条约束，但**不再有「找不到归属单」这一条**。
> - ~~**鉴权腿无法自行闭合**：`go.mod` 无 `tellurion-kit`…~~ **（2026-11-10 订正：`SVC-STRAT-G1` 已引 `kit`，三条受保护路由全部挂闸。）**
> - **题干订正（实测）**：`rg -n "promoted" internal/asset/pgstore.go` 只命中注释与 `asset.StatusPromoted` 常量（`:403` / `:430`），**`:393` 附近没有硬编码字符串**；本轮未动 `internal/asset`。
> - **2026-11-10 复核（已由上方收口块订正）**：core 非测试 Go 对 `strategy-service` / `STRATEGY_URL` / `8089` / `strategyservice` 的命中只有三条**注释**（`pkg/backtest/engine.go:947`、`pkg/bootstrap/auth.go:32`、`pkg/strategy/signal_provider.go:381`，均写明外部 strategy-service 路径已随 REV-16 删除）⇒ core 仍零调用。更关键：`SVC-CORE-STRAT-RETIRE`（2026-11-09 ✅）已把 ③ 的二选一**定死为本地 registry（不改 HTTP）**，理由是 strategy 服务无 list-all 端点 + 定义 schema 语义不同（K5）。因此本单 ①「core 的解析/注册/promote 走本仓」与 ⑤「core `/promote` 真实调用本仓」**与 core 已落裁决冲突，按原文不可能完成**。⇒ 维持 ⛔，但阻塞性质从「等 core 消费面」变为「**待主任裁决**」：要么撤销本单（资产面只做本仓自证），要么把验收改写为「与 core 本地 registry 共存的资产写入面」；裁决前不动代码。**（2026-11-10 主任裁决 A = 后者；此段仅留档，以上方收口块为准。）**

<a id="SVC-STRAT-G3-B"></a>
### SVC-STRAT-G3-B · core 回测闸门的读面：resolve 回写审批态 + promote 审计读端点 [P0] ✅（2026-11-10）

- **① 目标**：让 core 回测入口能用**一次 resolve** 判定资产审批态、用一次审计读证明审批留痕；本仓只出读面，不改 promote 状态机。
- **② 上下文**：core `QT-32` K11（「未 promote 的 AI 产物被回测拒绝」）有两个前置，其中之一是「读面缺判据」——`ResolveResult`（`internal/asset/model.go`）只回 artifact 坐标、不回 `status`；`strategy.strategy_promotions`（`SVC-STRAT-G1`）只有写端口、没有读端点，core 无法回答「谁批的 / 何时 / 结论」。G3 本身仍受 core 消费面阻塞，本单只解**读面**这一半。
- **③ 要求**：① `ResolveResult` 增 `status`（取自解析到的 artifact 行，枚举沿用 `Status` 冻结五值）；② 新增 `GET /api/strategies/{id}/promotions?version_hash=&limit=&offset=`（裸数组、新的在前、版本 hash 精确匹配、分页与 `versions` 同口径）；③ 审计读未装配存储 → 503 `audit_unavailable`（**不许**回空列表冒充「没有审批」）；④ 读面**不改** ⑫ 的三条鉴权边界（只靠端口隔离）。
- **④ 约束与非目标**：不改 promote 状态机；不改 `active_version_hash` / `active_artifact_hash` 的双空约束；**不实现「无 artifact 载体（dsl / builtin / external）的版本级 promote」**——那是结构性裁决（`Promote` 前置 `pickArtifact`，dsl 无产物 ⇒ 在资产服务里无法 promote），另立单；不改 `version_hash` 编码；不加缓存。
- **⑤ 验收**：resolve 在 promoted 版本上回 `status=promoted`；审计读回 `actor` / `outcome` / `version_hash` 且新的在前（Memory + 真 PG 两条腿）；未知策略 / 不匹配 hash 回 `[]`；未装配审计 → 503 `audit_unavailable`；破坏验证两条（去掉 `Status` 赋值 / 把审计排序改 ASC）均红→还原→绿。
- **⑥ 边界**：本仓读面；core 的解析客户端与 K11 闸门在 `tellurion:docs/TASKS.md` QT-32 的拆项里，不在本单。

> **施工记录（2026-11-10）**
>
> - **代码**：`internal/asset/model.go` 的 `ResolveResult` 增 `Status Status `json:"status"``；`service.go` 的 `buildResolveResult` 从 artifact 行回填；`promotions.go` 增 `PromotionFilter` + `PromotionLog.ListPromotions`（Memory 与 PG 两实现，新的在前、精确过滤、`Limit<=0` 不分页——不能写 `LIMIT 0`）；`internal/httpapi/api.go` 增 `GET /api/strategies/{id}/promotions`（openRoute，分页复用 `parsePage`）与哨兵 `errAuditUnavailable` → 503。
> - **用例**：`internal/asset/asset_test.go` 新增 `TestMemoryPromotionLog_ListPromotionsFiltersOrdersAndPages`（倒排 / 过滤 / 分页 / 空切片 / 空策略 id / 非 hex hash 可读回）；`internal/asset/g1_pg_test.go` 新增 `TestPG_ListPromotionsReadsBackWhatWasWritten`；`internal/httpapi/g1_test.go` 在 `TestPromoteRequiresActorAndWritesAudit` 末尾补 resolve `status=promoted` + 审计读回 + 未知策略 `[]` + 不匹配 hash `[]`，并新增 `TestListPromotionsWithoutAuditStoreIsUnavailable`（503）。
> - **真实输出**：`gofmt -l .` 无输出；`go vet ./...` **EXIT=0**；`go test ./... -count=1` **EXIT=0**（7 包全 ok）；真 PG 16.15 库 `tellurion_strategy_test`：`STRATEGY_TEST_DSN=… go test ./internal/asset -run 'TestPG_' -count=1 -v` **EXIT=0**，10 条 PG 用例全 PASS（含新增 `TestPG_ListPromotionsReadsBackWhatWasWritten`）。
> - **破坏验证（真做真还原）**：① 注释掉 `buildResolveResult` 的 `Status: a.Status` ⇒ `TestPromoteRequiresActorAndWritesAudit` 红（`resolve status = "", want promoted`），还原后绿；② 把 PG 审计读的 `ORDER BY id DESC` 改 `ASC` ⇒ `TestPG_ListPromotionsReadsBackWhatWasWritten` 红（`order = [promoted, rejected]`），还原后绿。
> - **文档**：`docs/api-schema.md` 读侧表新增该端点并写明 `status` 语义与「不得用 latest 指针 / `strategies.is_active` 冒充审批」；§2.1 补 HTTP 层哨兵；§4.2 补第二条列表端点；§7 表新增第 15 行。
> - **待主任追认（结构性缺口）→ 2026-11-10 代裁 B（待主任追认）**：原记录为「`Promote` 前置 `pickArtifact`，`carrier=dsl|builtin|external` 没有 artifact 行 ⇒ 在资产服务里无法 promote；而 agent 生成物若落 `dsl`，core K11 会恒拒」。**代裁结论 = 不开放无 artifact 载体的版本级 promote（方案 B）**，理由三条：① core `QT-32` ② 已定 AI 产物经 QT-41b 编译成 **wasm**（TinyGo 产物即 artifact）⇒ AI 闭环不依赖 dsl 放行；② 审批态是**产物级**状态机，给无字节码载体开版本级放行只能让 `resolve` 回一个空 `artifact_hash`，而 core 执行面只消费 wasm/so 字节（`WithWASMArtifacts`），等于造一个「已批准但拿不到资产」的假绿灯；③ 放宽 `active_version_hash`/`active_artifact_hash` 的双空 CHECK 会破坏 `SVC-STRAT-G3-B` ④ 已冻结的约束。**落地**：`internal/asset/service.go` 的 `Promote` 在 `pickArtifact` 之前先取定义，`!Carrier.NeedsArtifact()` ⇒ 400 `unsupported_carrier`（文案点名载体与「promote is defined for wasm|so」），`active_*` 一律不动；`internal/asset/asset_test.go` 新增 `TestService_PromoteRejectsArtifactlessCarrier`（dsl / builtin / external 三载体均为 `ErrUnsupportedCarrier` 且指针未动），`internal/httpapi/api_test.go` 的错误线表新增 `promote artifactless carrier` → 400 `unsupported_carrier`；`docs/api-schema.md` §2.1 同步该行。**破坏验证（真做真还原）**：把守卫改成 `if false && !def.Carrier.NeedsArtifact()` ⇒ 两条用例红（`artifact_not_found` / 404），还原后（`sha256=253CC82C…A6FBDA9C` 与备份一致）复绿。**影响**：agent 若继续以 `carrier=dsl` 登记生成物，promote 会得到 400 —— 这是**明示的边界**，不是恒拒黑箱；生成物须按 QT-32 ② 走 wasm 编译链路。
> - **消费方冒烟（2026-11-10 晚，cross-repo 指针）**：core `QT-32` 新增 opt-in 用例 `cmd/analysis/wiring/strategy_asset_live_test.go`，对**真的 `cmd/assetservice` 进程**（内存 Store、无 `-jwt-secret` 回环开放、`:18090`）走 create → source → `POST /internal/.../artifacts` → validate → promote（`X-Actor`）→ `GET .../promotions`，再从 core 侧断言放行与 404 / 409 两条拒绝。本单的 `status` 与审计读端点在该链路里被真实消费（证据与命令见 `tellurion:docs/TASKS.md` QT-32「2026-11-10 晚」段）；本仓无代码改动。

<a id="QT-41"></a>
### QT-41 · X2 策略资产服务（定义/版本/artifact/resolve）[P1] ✅（2026-10-24 G4 收口；余项为非目标/跨仓指针）

> **迁入说明（2026-10-21）**：本单自 core 仓 `tellurion:docs/TASKS.md`「五、X 线」迁入（任务台账批量重分布：X 线单展开到各服务仓为正式六字段单，「占位仓不写正式任务单」旧约定同时作废）。六字段正文逐字保留、不改写；core 侧同号条目已改为指针。

- **① 目标**：策略真相源在服务侧，引擎只持可驱逐只读副本；`id@artifact_hash` 可离线复现。
- **② 上下文**：`strategy-asset-service-api.md` + `strategy-source-of-truth-and-cache.md`（D13/D14）+ QT-03 的 hash 体系；**A/E/F/G/H 已于 2026-10-21 裁决**（见设计集 `README.md` 裁决记录）：A=本地栈 PG BYTEA + 8 MiB 上限、保留 `storage='url'` 逃逸阀；E=内存 LRU 一级 + 本地磁盘内容寻址二级；F=latest 用 TTL 300s，不引入事件；G=保留 init + 同步登记、不做全量迁移；H=`carrier=external` 纳入同一 `strategy_ref` 命名空间。
- **③ 要求**：① `strategies` 加列（carrier/abi_version/scope/capability/entry/`active_version_hash`/`active_artifact_hash`）+ `strategy_artifacts` + blobs（**A 已裁决**：本地栈落 PG BYTEA，单 blob ≤ 8 MiB 且超限 fail-loud，`storage='url'` 只保留列分支作将来逃逸阀）；② 写侧 7 端点、读侧 2 端点（resolve/artifact）；③ 缓存按 `artifact_hash`（永不失效）+ latest（**F 已裁决：TTL 300s**，promote 响应携带新 hash 供本进程主动失效，不引入 webhook/msgbus；**E 已裁决**：内存 LRU 一级 + 本地磁盘内容寻址二级，按容量与 TTL 驱逐）；④ promote 不覆盖 artifact 行；⑤ 14 个内置策略登记为 `carrier=builtin`（**G 已裁决：保留 init 启动 + 同步登记，不做全量迁移**）；⑥ `carrier=external` 纳入同一 `strategy_ref` 命名空间（**H 已裁决**），解析按 carrier 路由，外部载体 hash 语义单独成文、不与 `artifact_hash` 混用。
- **④ 约束与非目标**：策略服务**不算信号**；不改执行路径的"先查 artifact 再回退内置"在 QT-26 已定的顺序；不做对象存储（A 已裁 BYTEA，`storage='url'` 仅保留列分支、不实现）；不引入 webhook/msgbus（F 已裁 TTL）；不把内置策略全量迁移（G 已裁）；**裁决已落，相关子项按裁决值执行，不再按「假设」对待。**
- **⑤ 验收**：服务停机后已缓存 hash 的回测照跑且结果一致；同源码两工具链 → 两 artifact 可分别追溯；`id@version_hash` 解析结果回写实际 `artifact_hash`。
- **⑥ 边界**：只做资产与解析；编译服务是独立单 **QT-41b**（本单 ①~⑤ 不含编译链路）。

> **🔶 开工前置状态（2026-10-22）**：M8「接口对表」**已做完** → 本仓 `docs/api-schema.md` §6（写侧 7 端点 + 读侧 2 端点逐条签名 / 错误语义 / 版本策略 / 今天的实物）。
> ~~M5 ✅ 已结案~~ → **已被 ADR-036（2026-10-26）取代**：本仓定位为服务，G0 补 `Dockerfile + deploy`（`SVC-STRAT-G0`）。⇒ 本单剩余前置为 QT-05（S1 契约层落码；已落）与 G1 鉴权/契约版本。
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
> - `cmd/assetservice`（`127.0.0.1:8090`；ADR-036 起默认端口从 8085 改为 8090，避开 analysis-service）/ `cmd/compiler`（读 job JSON、严格解码、stdout 回执）。
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

<a id="QT-41b"></a>
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

<a id="QT-41c"></a>
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

<a id="SVC-STRAT-G1"></a>
### SVC-STRAT-G1 · 服务就绪门 G1：鉴权 · 公开健康端点 · 错误模型 · 版本化（ADR-036）[P0] ✅（2026-11-07，真库腿补齐）

- **① 目标**：本仓 10+2 条端点满足 ADR-036 §3 的 G1——版本化 API、错误模型、鉴权/授权、幂等、分页/限流——其中**鉴权是已知缺口，不是新需求**。
- **② 上下文**：`internal/httpapi/api.go:46` 的 `POST /api/strategies/{id}/versions/{hash}/promote` 今天**无鉴权、无审计**（本仓 G0 与 G3 两条单的「如实登记」都点名过它：「promote 鉴权缺口未修——属 G3 / QT-32 闸门，不能在本轮自行引入 kit」）；`go.mod` **未引入 kit**（实测直接依赖 = contracts `v0.5.0` + pgx `v5.7.3` + testify）；公开 `/healthz` 未落（G0 因此只能用 TCP 探针）；`/readyz` 全 workspace 0 命中。**已裁的姿势**：第三轮裁决 ⑫「promote 审批人 = 面 B1」——core 作唯一对外入口（JWT `sub` → 受信 `X-Actor` + service token），本仓**只对 `/promote` + 两条 `/internal/*`** 引 kit `auth/httpadapter`，`X-Actor` 缺失 400 `actor_required`，promote 落审计（人/时间/结论），其余 9 条靠端口隔离并在 `api-schema.md` 写明。
- **③ 要求**：① 引 kit `v0.1.4`（首次引，须先在本仓 `AGENTS.md` §4 依赖表登记，再改 `go.mod`）+ 建 `policy.go` 策略表，档位按裁决 ⑫ 只开三条；② `/promote` 落审计行（人 / 时间 / 结论），`X-Actor` 缺失 400；③ 公开 `/healthz`（无鉴权）+ 新增 `/readyz`（PG 池可连 + 本仓迁移已执行）；④ 错误模型闭合：`internal/asset/errors.go` 的哨兵（含编译/资产四哨兵）→ HTTP 码逐条列进 `api-schema.md`，未列出的视为缺陷；⑤ 版本化与兼容策略成文（加字段 MINOR / 改语义 MAJOR）；⑥ 列表端点给分页或显式「整段返回」+ 上界；⑦ 编译 job 的 `POST .../build` 幂等键语义成文（同一 `(strategy, version)` 重复请求不得排两份 job）。
- **④ 约束与非目标**：不做全站鉴权（按裁决 ⑫ 只三条，其余靠端口隔离）；不接 core 侧客户端（属 `SVC-STRAT-G3`）；不把编译器与资产服务合进程；不引入 K8s；不改 `internal/asset` 的资产/编译业务逻辑；不动 contracts pin（`v0.5.0` → 升 `v0.6.0` 若需要，走单独一笔并核 `StrategyRef` 等已用符号）。
- **⑤ 验收**：① 三条档位用例：无 token → 401/403、`role=service` → 放行三条、放行其余 9 条中的任一条即红（证明档位真的是白名单）；② `X-Actor` 缺失 → 400 `actor_required` 且**不写审计行**；③ `/healthz` 200 无鉴权、`/readyz` 无 PG → 503（两条各一条用例）；④ `api-schema.md` 每条端点有版本 + 错误码，哨兵表与实现逐条对得上；⑤ 三条命令单仓绿。
- **⑥ 边界**：本仓接口与鉴权；core 的 resolve/promote 客户端与 `strategies` 表停写归 `SVC-STRAT-G3` + `tellurion:SVC-CORE-STRAT-RETIRE`。

> **施工记录（2026-11-07）**：
>
> **落点（代码）**：`internal/httpapi/policy.go`（策略表三条，只认 `service`）、`auth.go`（`NewAuthorizer` /
> `requireAuth`：鉴权先于限流；未装配闸 → 503）、`limit.go`（按身份令牌桶 50 rps / 突发 200）、
> `api.go`（公开 `/healthz` `/readyz`；promote 校验受信 `X-Actor` 并落审计；版本列表 `limit`/`offset`；
> 新增 `unauthenticated` / `forbidden` / `actor_required` / `rate_limited` / `auth_unavailable` / `audit_failed`
> 六个 wire code）、`internal/asset/promotions.go`（`PromotionEvent` / `PromotionLog` + Memory/PG 实现 + `PG.Ready`）、
> `internal/asset/ddl.go`（新增 `strategy.strategy_promotions`：append-only、无外键）、
> `internal/asset/store.go` + `pgstore.go`（`ActiveJob`）、`internal/asset/service.go`（`SubmitBuild` 幂等键早退）、
> `cmd/assetservice/main.go`（`-jwt-secret` / `JWT_SECRET` + 启动闸 `DecideStartup`；装配鉴权 / 审计 / 就绪）。
>
> **与 ④ 的偏差（如实登记）**：③-⑦ 的 build 幂等键落到了 `internal/asset.Service.SubmitBuild` + `Store.ActiveJob`，
> 属于 ④ 点名的 `internal/asset` 面；改动只加「同键 `queued`/`running` 作业复用」，未碰 Verify / 状态机 /
> 编译语义。若主任认为 ⑦ 只该成文、不该落码，回退 `ActiveJob` 与那段早退即可（对应用例
> `TestSubmitBuildIsIdempotentWhileActive` 一并删）。
>
> **验收（真实退出码）**：`gofmt -l .` → 空；`go build ./...` → exit 0；`go vet ./...` → exit 0；
> `go test ./... -count=1` → exit 0（8 包）。
>
> **破坏验证（真做真还原，每条先红后绿）**：
> 1. 从 `policy.go` 删掉 promote 策略条目 → `TestServedPolicyIsServiceOnly` + `TestProtectedRoutesAcceptServiceToken` 红
>    （promote 被 403）；还原后绿。
> 2. 把 promote 的「`X-Actor` 缺失即 400」改成兜底 `unknown` → `TestPromoteRequiresActorAndWritesAudit` 红
>    （期望 400 / 0 行，实得 200 / 1 行）；还原后绿。
> 3. 删掉 `SubmitBuild` 的 `ActiveJob` 早退 → `TestSubmitBuildIsIdempotentWhileActive` 红
>    （第二次 `job_id` 不同 / 编译器被调 2 次）；还原后绿。
>
> **真库腿补跑（2026-11-07 晚；便携 PG 16.2 `127.0.0.1:55432`，库 `tellurion_strategy_test`）**：
> - **新增 `internal/asset/g1_pg_test.go`**（无 `STRATEGY_TEST_DSN` 即 skip）：
>   `TestPG_ReadyRequiresMigratedSchema`（PASS 0.15s）——已迁移库 `Ready` → nil；同一 PG 的 `postgres` 库（无本仓迁移）→ 报 `migrations not applied`（真腿证明「能连 ≠ 就绪」）；
>   `TestPG_RecordPromotionIsAppendOnly`（PASS 0.11s）——两条事件落 2 行，空 actor / 空 outcome → `ErrInvalidParameter` 且行数仍 2；
>   `TestPG_ActiveJobPicksEarliestOpenJob`（PASS 0.11s）——`queued`/`running` 三行取最早、最早落终态后退到次早、全终态 → not found。
>   `go test ./internal/asset -count=1 -v -run 'TestPG_(Ready|RecordPromotion|ActiveJob)'` → **EXIT=0**。
> - **真服务 + 真库探针**：`cmd/assetservice`（`STRATEGY_DATABASE_URL` 指向上述库，监听 `127.0.0.1:18090`）→
>   `/healthz` `200 {"status":"ok"}`、`/readyz` `200 {"status":"ready","store":"postgres"}`；`pg_ctl … stop -m fast`（`STOP_EXIT=0`）后
>   `/readyz` → `503 {"status":"not_ready","store":"postgres","error":"postgres ping: failed to connect to … actively refused it."}`，
>   同刻 `/healthz` 仍 `200 {"status":"ok"}`；重启库后 `/readyz` → `200`（**未重启服务**，池自愈）。
>
> **仍未执行（如实，不写成「通过」）**：
> - **真 service token 端到端 promote**：token 签发分发属 `QT-76`（core），本仓只能用 kit 在测试里自签（档位用例已覆盖 401 / 放行三条 / 白名单外红）。
> - `docker compose -f deploy/compose.yaml config`（compose 已改，加 `JWT_SECRET` / `AUTH_INSECURE_EXPOSURE`），Docker 引擎不可用。
>
> **顺带逮到的文档-实现不符（按实现订正，未改代码）**：`api-schema.md` §3 旧句「promote / deprecate 幂等
> （重复调用结果一致）」与状态机不符——重复 promote / deprecate 都返回 409 `invalid_transition`；已在 §3 改写并注明。
> 要改成真幂等属资产状态机变更，建议另立单（G4 / QT-41 余项）。
>
> **已知上界**：build 幂等键是「先查后写」，并发窗口内两次提交仍可能各排一份；未加唯一索引
> （加索引会改变 `strategy_build_jobs` 写入语义，留给 G2「唯一 writer + 可重放迁移」一并做）。
>
> **文档**：`api-schema.md`（§1 探针/鉴权/限流、§2 码表 + §2.1 哨兵闭合表、§3 幂等订正、§4.1 版本化、
> §4.2 分页、§7 逐端点版本 + 错误码）、`db-schema.md`（新表 + 约束 + 迁移）、`ARCHITECTURE.md`（分层/包布局/入口）、
> `AGENTS.md`（§1 状态 + §4 kit `v0.1.4` 登记）、`README.md` 与 `deploy/compose.yaml`（启动变量）。

<a id="SVC-STRAT-G2"></a>
### SVC-STRAT-G2 · 服务就绪门 G2：`strategy.*` 唯一 writer 与可重放迁移（ADR-036）[P0] ✅（2026-11-07，真库重放腿补齐）

- **① 目标**：本仓六张 `strategy.*` 表的 DDL 归属可自证——迁移可重复执行、不跨 schema 写、每张表「谁写」有答案；同时把「core 的 `strategies` 表还在被谁写」这条**双 owner 事实**如实登记，不假装 G2 已过。
- **② 上下文**：`internal/asset` 有 Memory + PG 两个 Store、启动迁移、两级缓存、14 个内置策略登记；本仓 `db-schema.md` 已声明六张表（G1 新增 `strategy_promotions`）；但**定义的真相源今天仍在 core**（`quant.strategies` + core 本地注册表，实测 core 侧写点见 `tellurion:pkg/storage/strategies.go` 的 5 个方法 + `cmd/analysis/setup.go:336` 的 `SeedStrategies`），ADR-036 §5.2 明写双 owner 是**迁移中的临时状态**，G2 的验收是「它的表只有它写」⇒ **本仓 G2 在 core 停写前不可能 ✅**，本单交付的是「本仓这一半的自证 + 缺口登记」。跨仓待核 K5（软删/版本语义对齐）已在 `db-schema.md` §1.1 作答。
- **③ 要求**：① 迁移重放自证：同一测试库连跑两次 DDL，行数 / `pg_indexes` / 列集指纹逐位不变（`STRATEGY_TEST_DSN` 腿，无库 skip）；② 禁跨 schema 写护栏：静态扫本仓 SQL，出现 `quant.` / `public.` / `market.` / `run.` 等非本仓 schema 的写语句即红；③ `strategy.strategy_definitions` 与 core `strategies` 的**软删语义与版本链对照表**（逐列：删除是 `deleted_at` 还是行消失、版本号从哪来、`version_hash` 怎么算），这是 K5 的答卷，也是 `SVC-CORE-STRAT-RETIRE` 的前置；④ 在 `db-schema.md` 里把「今天谁写」按列写实（双 owner 状态 + 指向 core 的具体行号），不许写成目标态。
- **④ 约束与非目标**：不 `DROP` / 不搬迁任何表；不改 core 的 `strategies` 写点（那是 core 侧退役单，串行第 6）；不新建第七张表来「绕过」语义对齐（第六张 `strategy_promotions` 是 G1 的审计表，不是对齐用途）；不在本单接 core 客户端。
- **⑤ 验收**：① 重放腿两次指纹逐位相同（贴输出）；② 破坏验证：往本仓 SQL 常量加一行 `UPDATE quant.strategies` ⇒ 护栏红，还原复绿；③ K5 对照表逐列有出处（`repo:path:line`），并能被 core 侧反查；④ `db-schema.md` 的「今天谁写」与 core 实测一致（抽 2 行核对）；⑤ 三条命令绿。
- **⑥ 边界**：本仓 schema 自证 + K5 答卷；停写与删除归 `SVC-STRAT-G3` 与 `tellurion:SVC-CORE-STRAT-RETIRE`。

> **施工记录（2026-11-07）**：
>
> **③-① 迁移重放自证（2026-11-07 晚真跑）**：`internal/asset/g2_pg_test.go` 的
> `TestPGMigrationsAreReplayable`——同库连跑两次 `Migrate()`，比对列集（`information_schema.columns`）、
> 索引集（`pg_indexes`）与六张表行数压成的 sha256。无 `STRATEGY_TEST_DSN` 时 skip。
> **真库输出（便携 PG 16.2，库 `tellurion_strategy_test`）**：
> `schema fingerprint stable across replay: e1403be15a4608fd2b06856c1d56aebcb13d405bae4668b75598c90cb1c5639a`
> （`--- PASS: TestPGMigrationsAreReplayable (0.08s)`，EXIT=0）——两次重放指纹逐位相同；`strategy_promotions` 在既有库上的可重入迁移同批被覆盖。
>
> **③-② 禁跨 schema 写护栏（已落）**：`internal/asset/g2_test.go` 的 `TestNoCrossSchemaWrites`
> 静态扫全仓 `.go`，命中 `insert/update/delete/create table/create index/alter table/drop table/truncate table`
> + 非 `strategy.` schema 即红；附 `TestStrategyWritesAreFullyQualified` 反向自证本仓写语句都带 `strategy.`。
> **破坏验证（真做真还原）**：在 `internal/asset/promotions.go` 临时加一行
> `UPDATE quant.strategies SET is_active = FALSE` → `TestNoCrossSchemaWrites` 红（报该文件与语句）；
> 删掉该行后复绿（`go test ./internal/asset -run TestNoCrossSchemaWrites` exit 0）。
>
> **③-③ K5 对照表（已落）**：`db-schema.md` §1.1 逐列对照 core `strategies` 与本仓 `strategy_definitions`
> （主键/名称/参数/类型/软删 vs 无删除/版本链/`version_hash`/内置 3 vs 14/今天谁写/时间戳），出处写到
> `tellurion:pkg/storage/strategies.go:12,106,119`、`tellurion:docs/migrations/011_add_strategies_table.sql:4-14`、
> `internal/asset/pgstore.go:57,393,420`、`internal/asset/ddl.go:27-45`。核心差异：core 删除 = `is_active=false`
> 行仍在，本仓**无定义级删除**、下线是版本级 `deprecate`；core 无版本概念、参数原地 upsert，本仓是内容寻址版本链。
>
> **③-④ 「今天谁写」写实**：`db-schema.md` §1.1「写者（今天）」行 + 表下「双 owner 事实」说明，
> 明确今天仍是 core 写 `quant.strategies`、本仓零写它；未写成目标态。
>
> **⑤ 验收对照（2026-11-07 晚补齐后）**：① 重放腿已真跑，两次指纹逐位相同（输出见上，EXIT=0）；② 破坏验证当日已「红 → 逐字还原 → 绿」；
> ③ K5 对照表逐列有出处；④ 抽 2 行核对 core 实测——`tellurion:pkg/storage/strategies.go:14`（`INSERT INTO strategies …`，upsert）
> 与 `:107`（`UPDATE strategies SET is_active = FALSE …`，软删）与 `db-schema.md` §1.1 的「今天谁写 / 删除语义」两行一致；⑤ 三条命令绿。
>
> **文档**：`db-schema.md` §1（六张表）、§1.1（K5 对照表）、§3（新表约束）、§4（迁移清单）。

<a id="SVC-STRAT-G4"></a>
### SVC-STRAT-G4 · 服务就绪门 G4：编译链路失败语义 · 超时重试 · runbook（ADR-036）[P1] ✅（2026-11-10 真 TinyGo 镜像腿补跑通过）

- **① 目标**：把「编译链路挂了会怎么样、多久放弃、怎么查、怎么回滚」成文并跑过一次——本仓的外部依赖（TinyGo 工具链 / `cmd/compiler` 子进程）是全系统最容易静默失败的一环。
- **② 上下文**：QT-41c 已落跨进程闭环：`POST .../build` → `internal/compilerclient` 派发 `cmd/compiler` → 内部端点回写 `queued → running → succeeded | failed`（失败带 `error_code`），`-compiler-bin` 为空时 build 端点 fail-closed 503 `compile_unavailable`；G5 已落编译 job 的队列 / 重试 / 限并发；本仓 `docs/evidence/QT-41b-tinygo-0.34.0.md` 有真工具链取证姿势。**缺口**：无 runbook（全 workspace `runbook` 只在 ADR-036 正文里出现）；无 `/readyz`（属 `SVC-STRAT-G1` ③）；子进程超时上界与「编译器进程被 OOM killer 杀掉」这类失败在文档里没有位置；`STRATEGY_TINYGO_IMAGE` 腿的 skip 与 fail 未区分口径。
- **③ 要求**：① 失败语义表：`compile_unavailable` / `ErrCompileFailed` / `ErrSourceTooLarge` / `ErrArtifactTooLarge` / 派发失败 / 回写失败（内部端点不可达）/ 超时七类，各给「HTTP 码、是否重试、job 终态、调用方该做什么」；② `cmd/compiler` 子进程给**显式超时上界 + `CommandContext`**（core 侧 AUD-56 的同型坑：`go build` 联网解析挂 601s，加 `CommandContext` + 120s 上界后 6.18s），超时后 job 落 `failed` 且有 `error_code`，不许留在 `running`；③ 重试边界成文（哪些错误码可重试、几次、退避）；④ `docs/runbook.md`：起停、看编译 job 状态、重放迁移、真工具链验证（`STRATEGY_TINYGO_IMAGE` 那条）、卡死 job 的人工收口；⑤ 一次故障演练：把 `-compiler-bin` 指向不存在的文件 ⇒ 503 且不静默成功，恢复后复通。
- **④ 约束与非目标**：不改资产/编译的业务口径；不引 APM；不做多副本；不在本单把 TinyGo 版本升级（锁 0.34.0 是既有裁决，动它另立单）；不接 core 消费面。
- **⑤ 验收**：① 失败语义表七类齐全且每条有用例或指明无用例原因；② 超时用例：注入一个永不退出的假 compiler ⇒ 到上界即 `failed` 且有 `error_code`（破坏验证：去掉上界 ⇒ 用例红）；③ 演练有真退出码（503 → 恢复 200）；④ runbook 每条命令当场执行过；⑤ 三条命令绿。
- **⑥ 边界**：本仓失败语义与运维面；core 侧的运行时 WASM host 留 core（ADR-036 §6 第 1 条），不在本单。

> **施工记录（2026-11-07，🔶 未全绿）**
>
> - **②（核心代码）**：`internal/compilerclient` 的子进程固定走 `exec.CommandContext`，新增显式上界
>   `DefaultProcessTimeout=20m` / `MaxProcessTimeout=1h`，flag `-compiler-process-timeout`；实际生效 =
>   min(批次预算, 上界)。到点杀进程后回写 `failed + error_code=compile_timeout`（新增 wire code，§4.1 MINOR），
>   不允许留 `running`。真子进程杀进程用例 `TestExecRunner_KillsProcessAtDeadline`；行为用例
>   `TestDispatchBuild_ProcessDeadlineFailsJobWithCompileTimeout`（150ms 上界）。
> - **①/③**：`docs/api-schema.md` §2.2 七类失败语义表（HTTP / 重试 / job 终态 / 调用方动作）、§3.1 重试边界
>   （作业级 3 次、2s 固定退避；进程级不自动重投；HTTP 调用方退避表）。
> - **④**：`docs/runbook.md` 已建；本机实跑腿标 `[已执行]`，PG / Docker / TinyGo 腿标 `[未执行]`（不假装修过）。
>   迁移重放走 `TestPGMigrationsAreReplayable`（需 `STRATEGY_TEST_DSN`），真工具链走
>   `TestCompile_RealTinyGoImage`（需 `STRATEGY_TINYGO_IMAGE` + Docker）。
> - **⑤**：真进程演练 `-compiler-bin` 指向不存在文件 → HTTP **503** `compile_unavailable`；恢复腿换成可启动的
>   `compiler.exe` → **202** + `job_id`（工具链锁缺失，随后 `failed/compile_failed`，属预期）。单测版演练
>   `TestG4Drill_MissingCompilerBinFailsClosedThenRecovers` 额外断言落库行 `failed/compile_unavailable` 与恢复后 `202/queued`。
>   注：build 的成功码按冻结契约是 **202**（异步受理），不改成 200；「复通」以 202 + 随后
>   `GET .../builds/{job_id}` **200/queued** 证明，成功码语义不动。
> - **七类对应用例**：`compile_unavailable` → 上述演练；`ErrCompileFailed` → 恢复腿真进程 + `compilerjob`
>   既有重试用例；`ErrSourceTooLarge` → `internal/asset/asset_test.go` 超限用例；`ErrArtifactTooLarge` →
>   `asset_test.go` / `pgstore_test.go` 超限用例；派发失败 → 演练单测；回写失败 → `compilerjob`
>   `TestQueue_RetriesRunningReport` / `TestQueue_TerminalReportFailureDoesNotFlipSuccess`（无真停服注入，
>   人工收口见 runbook §6）；超时 → 两个新用例。
> - **PG 腿补齐执行（2026-11-10）**：本机 PostgreSQL 16.15 起库 `tellurion_strategy_test`，
>   `STRATEGY_TEST_DSN=... go test ./internal/asset -count=1` **全绿（EXIT=0）**，含
>   `TestPGMigrationsAreReplayable`（输出 `schema fingerprint stable across replay:
>   10f6ee398ac1553f0912fabb1dd671c5a44dd8bf69494ce432e383d3ea329354`）、`TestPG_ServiceOnPG`、
>   `TestPG_ReadyRequiresMigratedSchema`、`TestPG_MigrateIsIdempotent` 等 10 条 PG 用例。
>   `docs/runbook.md` 该条已由 `[未执行]` 改为 `[已执行 2026-11-10]`。
> - **真 TinyGo 0.34.0 镜像腿（2026-11-10 补跑，最后一项缺口）**：本机 Docker 引擎 `29.8.0`（Docker Desktop 经 `explorer.exe` 拉起后 `docker info` 返回版本号），`docker pull tinygo/tinygo:0.34.0` → `Image is up to date`。`STRATEGY_TINYGO_IMAGE=tinygo/tinygo:0.34.0 go test ./internal/compile -run 'TestCompile_RealTinyGoImage|TestPinnedToolchainLockHasRealTinyGo' -count=1 -v` → `TestPinnedToolchainLockHasRealTinyGo PASS`、`TestCompile_RealTinyGoImage PASS (8.61s)`、`ok tellurion-strategy/internal/compile 9.296s`，**退出码 0**（非 skip）。`docs/runbook.md` §4 该条已改 `[已执行 2026-11-10]`。⇒ ⑤ 五项全部有真实输出，本单翻 ✅。
> - **门禁**：`gofmt -l .` 无输出；`go build ./...`、`go vet ./...`、`go test ./... -count=1` 均退出码 0。
>
> **真回写面停机 + 卡死 job 人工收口演练（2026-11-10，真 PG · 真进程 · 0 代码改动）**
>
> - **环境**：真 PostgreSQL 16.15 @ `127.0.0.1:5432`（一次性库 `tellurion_strategy_drill`）+ `cmd/assetservice` 真二进制 `:18090` + 替身编译进程（Go 程序，按名扫描 `-job` / `-asset-url`，只回写 `running` 后正常退出、永不回写终态）。替身**不是**真 TinyGo，本腿验的是**状态机 + 落库 + 人工收口**，不是编译产物。
> - **腿 A（非零退出自动收口）**：替身因不认识的开关 `exit status 2` ⇒ 服务补报 `failed / compile_failed`，`GET .../builds/{job_id}` 立即 **200 `failed`**（`error` 含 `exit status 2`）。证明进程崩溃不会留 `running`。
> - **腿 B（正常退出但永不回写终态）**：修好替身参数后 `POST .../build` **202**，轮询 `GET .../builds/{job_id}` → **200 `running`**；随后**停掉资产服务**（回写面停机）并等替身退出 ⇒ 端口 `18090=False`、`Get-Process stubcompiler` 空。
> - **腿 C（真 PG 持久）**：同一 DSN 重启服务，`GET .../builds/{job_id}` 仍 **200 `running`**（`created_at` 原样），证明卡死状态在库里而非内存。
> - **腿 D（人工收口 + 幂等 + 终态不可回退）**：内部端点 `{"state":"failed","error_code":"compile_failed",…}` → **200 failed**；重复同一请求 → **200**（同 `finished_at`）；再报 `running` → **409 `invalid_transition`**；psql 直读 `strategy.strategy_build_jobs` → `st_d26d9…|failed|compile_failed|2026-11-09 22:04:47`。
> - **文档订正**：runbook §6 旧句「已是终态会 409」不准确——同终态重复上报是幂等 200，409 只出现在把终态改回 `running` 等非法转移；已按实测改写并新增 §6.1 演练记录。
> - **清场**：服务停、库 `DROP … WITH (FORCE)`（`DROP_EXIT=0`，`pg_database` 无残留）、临时目录（服务/替身二进制、日志、请求响应体、cache、`go.mod`）全删；本仓 `git status` clean。
> - **仍未执行（故仍不标 ✅）**：真 TinyGo 0.34.0 镜像编译（Docker 引擎不可用）。G4 的「真回写面停机」腿据此结清。


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

## 续档（2026-11-12 收口）

<a id="STRAT-GRACEFUL-SHUTDOWN"></a>
### STRAT-GRACEFUL-SHUTDOWN · 资产服务无优雅停机，SIGTERM 直接掐断在途请求 [P1] ✅（2026-11-12）

- **① 目标**：`cmd/assetservice` 收到 SIGTERM/SIGINT 时先停止接受新连接、给在途请求一个有界 drain 窗口再退出，而不是被信号直接杀死。
- **② 上下文（2026-11-11 全面审查 W5-F2，证据 ODR-072 `tellurion-docs:docs/archive/odr/odr-072-audit-wave5-security-reliability-deps.md`）**：`cmd/assetservice/main.go:175-189` 已建 `http.Server`（带 `ReadHeaderTimeout: 5s`）并在 `:186` 用 `errors.Is(err, http.ErrServerClosed)` 兜底，但**全文件无 `signal.Notify` / `srv.Shutdown` / `NotifyContext`**（grep 零命中）⇒ `ErrServerClosed` 分支是死代码，没有任何东西触发 `Shutdown`，SIGTERM 走默认处置直接终止进程。同批四仓（data/risk/analytics/strategy）同形缺陷，见各仓 `*-GRACEFUL-SHUTDOWN` 单。
- **③ 要求**：① 用 `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)` 或等价机制捕获终止信号；② 信号到达后调 `srv.Shutdown(drainCtx)`，drain 窗口有界（建议 ≤10s，与既有超时口径一致）；③ drain 超时则 `srv.Close()` 兜底并非零退出；④ 正常 `ListenAndServe` 返回 `ErrServerClosed` 时退出码 0（现有兜底保留）；⑤ 启动日志点名 drain 窗口时长。
- **④ 约束与非目标**：不改路由/鉴权/限流语义；不引入新依赖（用标准库 `os/signal` + `context`）；不动 `RegisterBuiltins` 与编译器装配；不改成「无限等待在途请求」。
- **⑤ 验收**：真起服务（A4 授权）→ 发一个慢请求 → SIGTERM → 断言该请求在 drain 窗口内正常完成、进程退出码 0；破坏验证 = 把 `Shutdown` 改回裸 `ListenAndServe`（去掉信号捕获）⇒ 慢请求被掐断（连接 reset / 非零退出），还原复绿。`go build/vet/test ./... -count=1` 全绿。
- **⑥ 边界**：本仓 `cmd/assetservice/main.go` 的 run/serve 段；其余四仓的停机归各自单。
- **owner**：待定

> **施工记录（2026-11-12）**：
> - **落点**：`cmd/assetservice/main.go`——新增 `shutdownDrainTimeout = 10s`；`run()` 改为先 `net.Listen` 再 `serve(ln, srv, shutdownDrainTimeout)`；新增 `serve`（`signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`）与可测内核 `serveWithContext`（信号/ctx 取消 → `srv.Shutdown(drainCtx)`；drain 超时 → `srv.Close()` + 返回包裹 `context.DeadlineExceeded` 的错误 ⇒ 非零退出；`ErrServerClosed` → nil ⇒ 退出码 0）；启动日志新增 `drain=10s` 字段。未改路由/鉴权/限流/编译器装配。
> - **为什么把 listener 提到 run()**：`serveWithContext` 需要预先绑定的 `net.Listener` 才能被单测确定性地连上（端口 0 由内核分配），否则测试只能用「猜端口」的写法；生产路径仍是同一个 `net.Listen` + `srv.Serve`，语义与 `ListenAndServe` 等价。
> - **测试**：新增 `cmd/assetservice/shutdown_test.go` 两条——① 慢请求（handler 阻塞在 release channel）→ 取消 ctx（等价 SIGTERM）→ 请求仍在 drain 窗口内返回 204、`serveWithContext` 返回 nil；② handler 永不返回 → drain=100ms → `serveWithContext` 返回包裹 `context.DeadlineExceeded` 的错误（非零退出）。
> - **验收（真实输出，本机 Windows）**：`go build ./...`、`go vet ./...` 退出码 0；`go test ./... -count=1` 全绿（7 包，`cmd/assetservice 0.426s`）；`TestServeWithContextDrainsInFlightRequest` 与 `TestServeWithContextDrainTimeoutIsNonZeroExit` PASS，日志出现 `termination signal received; draining in-flight requests drain=5s|100ms`。
> - **破坏验证**：把 `serveWithContext` 改成裸 `return srv.Serve(ln)`（去掉信号捕获与 Shutdown）⇒ 两条用例同时红（`serveWithContext did not return after the clean drain` / `...after the drain window elapsed`，各 5s 超时）；逐字节还原后两条复绿。
> - **如实登记**：未在本机对**真实进程**发 SIGTERM 做黑盒演练（Windows 无 unix SIGTERM 语义，`os/signal` 腿由 `serveWithContext` 等价覆盖）；Linux 容器内的 SIGTERM 行为由同一 `signal.NotifyContext` 路径承载，未在容器内单独复跑。

<a id="STRAT-AUTH-ADR"></a>
### STRAT-AUTH-ADR · 鉴权姿态无 ADR，10 条公开端点的边界决定只散在 api-schema [P2] ✅（2026-11-12）

- **① 目标**：给本仓的鉴权姿态补一条 ADR，把「10 条读端点公开 + 端口隔离、3 条写端点 service-token、`DecideStartup` 启动闸、promote 额外 `X-Actor`」这套**决定**固化成可追溯的架构决策，而不是只留在接口文档的行为描述里。
- **② 上下文（2026-11-11 全面审查 W2-F4，证据 ODR-069 `tellurion-docs:docs/archive/odr/odr-069-audit-wave2-core-architecture.md`）**：`docs/api-schema.md:78-95` 已完整描述鉴权行为（service token = kit `IssueServiceToken`、无/坏 token→401、角色不对→403、10 条公开端点靠 `127.0.0.1:8090` 端口隔离、`DecideStartup` 拒「漏配 secret 又非回环」、限流 50rps/突发200、promote 要 `X-Actor` 否则 400 `actor_required`）。但 `docs/adr/` 只有 `0001`~`0004`，**无一条是鉴权姿态 ADR**（`0004-core消费边界与promote归属` 只裁 promote 归属，不覆盖整体鉴权边界）。对照 risk/data/analytics 均有鉴权 ADR（ADR-0005/0006 族），本仓缺这一条。
- **③ 要求**：① 新增 `docs/adr/0005-鉴权姿态与端口隔离边界.md`（编号续 0004），记录：为何读端点公开（单人自托管 + 回环/集群内端口隔离）、为何写端点只认 `service`、`DecideStartup` 的 fail-closed 判据、promote 的 `X-Actor` 与审计；② 显式 cross-ref `ADR-0004`（promote 归属）与 kit `auth/httpadapter`；③ `docs/api-schema.md` 鉴权节加一行指回该 ADR；④ `docs/adr/README.md` 索引登记。
- **④ 约束与非目标**：**不改任何鉴权代码与策略表**（本单纯文档/决策固化）；不新增端点；不放宽现有 fail-closed；不把 api-schema 的行为表整段抄进 ADR（ADR 写「为什么这么定」，行为表留在 api-schema）。
- **⑤ 验收**：ADR-0005 存在且 Status=Accepted、含 Context/Decision/Consequences；api-schema 鉴权节有指回链接；`docs/adr/README.md` 索引含 0005；`python ../tellurion-docs/tools/check_doc_links.py`（或本仓等价链接巡检）活跃层 0 断链。
- **⑥ 边界**：本仓 `docs/adr/` 与 `docs/api-schema.md` 鉴权节；鉴权实现归 kit，跨仓服务间角色口径归 core/各服务 ADR。
- **owner**：待定

> **施工记录（2026-11-12）**：
> - **落点**：新增 `docs/adr/0005-鉴权姿态与端口隔离边界.md`（状态=已裁决 / Accepted，含场景 Context、决策 Decision、理由、代价 / 后果 Consequences、与 kit JWT 及其他仓关系、替代关系）；`docs/adr/README.md` 索引登记 0005；`docs/api-schema.md`「鉴权与限流（G1）」节顶部加指回 ADR-0005 的决策来源段。**未改任何鉴权代码与策略表。**
> - **前提证伪（如实登记，与工单措辞不一致）**：工单写「10 条**读**端点公开」，实测 `internal/httpapi/api.go:112-133` 的 10 条公开端点里只有 5 条是读（`versions` / `promotions` / `builds/{job_id}` / `resolve` / `artifact`），另 5 条是**写**（create / source / build / validate / deprecate）；受保护的 3 条才是 promote + 2 条 `/internal/*`（`policy.go`）。ADR 按**实测事实**写，并把「公开写端点在端口隔离失效时完全开放」「启动闸只覆盖无 secret 组合」列为**残余风险**，明确「若要求这些端点也需身份，先改 `policy.go` 与文档并另立单，本轮不改代码」——不在 ADR 里把工单的错误措辞固化成"读端点公开"。
> - **与既有 ADR 的关系**：cross-ref ADR-0004（promote 归属 / 部署入口解释）与 kit `auth/httpadapter`（`DecideStartup` / `ServiceRole`）；按派单要求**不声称跨仓统一**——broker 的写侧静态 token / 读侧开放是它自己 ADR 的分叉例外，本 ADR 只写本仓姿态。
> - **验收（真实输出）**：`python ../tellurion-docs/tools/check_doc_links.py` → `活跃层：247 条链接，0 条断`（历史层 758 条 / 174 断，只统计不判红）；ADR-0005 状态行含 `已裁决 / Accepted`；api-schema 与 README 索引均有指回/登记。
> - **破坏验证（链接层，真实执行）**：`check_doc_links.py` 把**指向** `docs/adr` 的链接归入"历史层、不判红"（实测把 api-schema 里的目标改成 `0005-not-exist.md` 后活跃层仍是 `0 断`），所以另用存在性扫描（`docs/*.md` + `docs/adr/README.md` 中所有 `](adr/...)` 目标逐个 `Test-Path`）：正常态 `broken count: 0`；把 ADR 文件临时重命名为 `0005-tmp-break.md` ⇒ `BROKEN api-schema.md -> adr/0005-鉴权姿态与端口隔离边界.md`、`broken count: 1`；改回原名后复绿 `0`。

