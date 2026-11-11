---
status: active
last-verified: 2026-11-11
verified-by: 文档对齐审计——端点数校准（13 业务端点 = 10 公开不挂闸 + `/promote` + 2 `/internal/*`，另 2 探针）、kit pin 记 `v0.2.0`、contracts pin 升 `v0.9.0`、promote 调用方按 ADR-0004 改为 agent/操作者直连、剩余项归为非目标
---

# API Schema — tellurion-strategy

> **实现状态（2026-11-11 复核）**：13 个业务端点——10 条公开不挂鉴权 + 3 条受保护（`/promote` + 2 条 `/internal/*`）
> ——由 `internal/httpapi` 落地，另有 2 条公开探针；语义在 `internal/asset.Service`（均是标准库 `net/http`，不是 gin）。
> 存储有 `Memory`（测试）与 PG（生产，启动执行迁移）；两级缓存、latest TTL、build job 持久化、
> 14 个内置策略登记均已落地。
> **QT-41c（2026-10-25）**：`cmd/assetservice` 用 `-compiler-bin` 装配外部编译进程——build 端点落 queued 行后
> 经 `internal/compilerclient` 派发 `cmd/compiler`，编译进程经内部端点回写 `running` / 终态；
> `-compiler-bin` 为空则不装配，build 端点仍 fail-closed 503 `compile_unavailable`。
> **SVC-STRAT-G1（2026-11-07）**：引 `tellurion-kit`（引入时 `v0.1.4`，现 pin `v0.2.0`），`/promote` + 两条 `/internal/*` 挂鉴权闸
> （`policy.go`，只认 `service` 角色）；promote 要求受信 `X-Actor` 并落 `strategy.strategy_promotions` 审计；
> 新增公开 `/healthz` 与 `/readyz`；按身份令牌桶限流（50 rps / 突发 200）；版本列表支持 `limit` / `offset`；
> build 幂等键落实现。错误码闭合表见 §2.1，逐端点版本与错误码见 §7。
> **非目标（不是欠账）**：`storage=url` 分支（只保留列、fail-closed）、`carrier=external` 的 hash 语义（不单独立文）、
> core 侧 Mount 门（消费侧改造，归 `tellurion` 仓）。`docs/TASKS.md` 当前无开放项。
> `version_hash` 的输入形状与域标签自 contracts `v0.5.0` 起收编（G5 ③，2026-10-24），现 pin `v0.9.0`：`PUT .../source` 的 wire 形状与 hash 值不变。
>
> 本文件是**目标契约 + 现状**：已实现的行以代码为准，未实现的行是欠账，不允许把欠账当现状描述。

> **契约地位**：HTTP。**写侧给作者/AI/CI，读侧 3 端点给引擎/回测入口** —— 引擎只用 `resolve` 与 `artifact`，`promotions` 供审批闸门，其余端点它不该碰。
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

### 读侧（引擎用前两个；promotions 供回测入口的 AI 闸门）

| 方法 | 路径 | 用途 |
|---|---|---|
| GET | `/api/strategies/{id}/resolve?ref={hash\|latest}` | 解析 `strategy_ref` → 元数据（含 `artifact_hash` / `scope` / `capability` / `imports` / **`status`**） |
| GET | `/api/strategies/{id}/versions/{hash}/artifact` | 下载字节码（响应头 `X-Artifact-Hash`，支持 `ETag` / `If-None-Match` → 304） |
| GET | `/api/strategies/{id}/promotions?version_hash=&limit=&offset=` | promote 审计读面（K11 前置）：谁 / 何时 / 结论，新的在前 |

> `ref` 三形态：空 / `latest` → definition 的 active；64 位小写 hex → 先按 `artifact_hash` 查、查不到再按
> `version_hash` 解析到该版本当前 promoted 的 artifact（**回写实际 `artifact_hash`**）；`id@hash` → id 必须与路径一致。
>
> `status` 是解析到的那一行 artifact 的审批态（`built` / `validated` / `promoted` / `rejected` / `deprecated`）。
> 消费方（core 回测入口）必须 `status == promoted` 才继续；**不得用 definition 的 latest 指针或
> `strategies.is_active` 冒充审批**。promotions 审计与 resolve 是两个读面：resolve 回状态，
> `/promotions` 回审批留痕（缺留痕同样是拒绝条件，见 core `QT-32` K11）。

### 探针（公开、无鉴权、不限流）

| 方法 | 路径 | 用途 |
|---|---|---|
| GET | `/healthz` | 进程活着 → 200 `{"status":"ok"}`；不查库 |
| GET | `/readyz` | 就绪 → 200 `{"status":"ready","store":"postgres"}`；PG 池不可连 / 迁移未执行 / 无 PG → 503 `{"status":"not_ready",...}` |

> 两条探针**刻意不鉴权、不限流**：被限流会被编排层读成未登录/坏实例（core 侧 AUD-57 的同型坑）。
> `/healthz` 只回答「进程活着」，库挂了它仍然 200——编排层要用 `/readyz` 做流量准入。

### 鉴权与限流（G1）

> **决策来源**：本节的姿态（10 条公开 / 3 条 `service` 闸 / `DecideStartup` 启动闸 / promote 的 `X-Actor`）已固化为
> [ADR-0005 · 鉴权姿态与端口隔离边界](adr/0005-鉴权姿态与端口隔离边界.md)；本节只留可执行的行为表，取舍理由见 ADR。

**只有三条端点挂鉴权闸**（`internal/httpapi/policy.go`，裁决 ⑫）：

| 方法 | 路径 | 允许角色 |
|---|---|---|
| POST | `/api/strategies/{id}/versions/{hash}/promote` | `service` |
| POST | `/internal/strategies/{id}/artifacts` | `service` |
| POST | `/internal/strategies/{id}/builds/{job_id}` | `service` |

- 凭证：`Authorization: Bearer <access token>`（service token = kit `IssueServiceToken`，`role=service`）。
  无 token / 坏 token / 用 refresh token → 401 `unauthenticated`；角色不对 → 403 `forbidden`。
- 其余 10 条公开端点**不挂鉴权**，靠端口隔离（默认 `127.0.0.1:8090`，只在集群内/回环）。
  启动闸（kit `DecideStartup`）：无 `JWT_SECRET` 且绑定非回环、又未声明 `AUTH_INSECURE_EXPOSURE=loopback-published`
  ⇒ **拒绝启动**，不会出现「漏配 secret 就全开」的进程。
- 限流：进程内按身份分桶（令牌桶 50 rps / 突发 200；未鉴权端点共用一个 `open-access` 桶）。
  超限 → 429 `rate_limited` + `Retry-After: 1`。**鉴权先于限流**：401 / 403 不会被 429 顶替。
- promote 额外要求受信头 `X-Actor`（由 agent / 操作者**直连本仓**时携带；ADR-0004 裁决 A——core 不做 promote 代理）：缺失 → 400 `actor_required`，
  且**不写审计行**。成功 promote 追加一行 `strategy.strategy_promotions`（actor / decided_at / outcome）。
  审计写失败 → 500 `audit_failed`（promote 可能已生效，重试安全，但不会静默丢审计）。

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
> **`error_code`（机器可读归因，随 BuildJob 返回）**：`compile_failed` / `compile_unavailable` / `payload_too_large` / `artifact_invalid` / `compile_timeout`。
> 它与 HTTP 错误码不是一回事：503 `compile_unavailable` 表示「服务没装配/启动不了编译器」，作业里的
> `error_code=compile_failed` 表示「编译器跑了但这次编译失败」，`compile_timeout` 表示「子进程超过硬上界被杀」
> （SVC-STRAT-G4 新增，属 §4.1 的 MINOR 加码）；调用方据此区分。
> 七类失败语义与重试边界见 §2.2 / §3。

## 2. 错误码与 fail-closed 语义

响应体统一 `{"error": "...", "code": "..."}`；`code` 是**领域码**，前端/引擎按它分支。

| 情况 | 返回 | 调用方行为 |
|---|---|---|
| 受保护路由无 token / 坏 token / refresh token | 401 + `unauthenticated` | 换 access token 重试 |
| 受保护路由角色不是 `service` | 403 + `forbidden` | 拒绝（不是权限不足的模糊化，本仓只有一档） |
| promote 缺 `X-Actor`（受信头） | 400 + `actor_required` | 由 agent / 操作者补可信身份后重试（core 不代理 promote）；本仓不猜审批人 |
| promote 审计写失败 | 500 + `audit_failed` | 查库确认 promote 是否已生效；重试只会得到 409，**不会补写审计** |
| 超过令牌桶 | 429 + `rate_limited`（带 `Retry-After`） | 按 `Retry-After` 退避 |
| 受保护路由未装配鉴权闸 | 503 + `auth_unavailable` | 部署缺陷，拒绝流量 |
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
| 载体不支持（含对 builtin / dsl / external 调 promote） | 400 + `unsupported_carrier` | 拒绝；无 artifact 载体没有可审批的产物（2026-11-10 裁决） |
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

### 2.1 哨兵闭合表（`internal/asset/errors.go` → HTTP，G1 ④）

> 每个哨兵都要有**确定的** HTTP 码。落在「其它」不算闭合——本表是 `classify()` 的逐条镜像，
> 新增哨兵不加表 = 缺陷；未在表内的哨兵只允许走 500 `internal`（且必须是真正的服务端故障）。

| 哨兵（`asset.`） | HTTP | wire code |
|---|---|---|
| `ErrStrategyNotFound` | 404 | `strategy_not_found` |
| `ErrVersionNotFound` | 404 | `version_not_found` |
| `ErrNoActiveVersion` | 404 | `no_active_version` |
| `ErrArtifactNotFound` / `ErrJobNotFound` | 404 | `artifact_not_found` / `job_not_found` |
| `ErrStrategyExists` | 409 | `strategy_exists` |
| `ErrArtifactConflict` | 409 | `conflict` |
| `ErrAmbiguousArtifact` | 409 | `ambiguous_artifact` |
| `ErrInvalidTransition` | 409 | `invalid_transition` |
| `ErrInvalidArtifact` / `ErrArtifactHashMismatch` | 409 | `artifact_invalid` |
| `contracts.ErrImportNotWhitelisted` | 409 | `imports_not_allowed` |
| `ErrStorageUnsupported` | 409 | `storage_unsupported` |
| `ErrCompileFailed`（contracts 别名） | 409 | `compile_failed` |
| `ErrCompileUnavailable`（contracts 别名） | 503 | `compile_unavailable` |
| `ErrSourceTooLarge` / `ErrArtifactTooLarge`（contracts 别名） | 413 | `payload_too_large` |
| `ErrUnsupportedCarrier` | 400 | `unsupported_carrier` |
| `ErrInvalidABIVersion` | 400 | `invalid_abi_version` |
| `ErrInvalidScope` | 400 | `invalid_scope` |
| `ErrSourceMissing` | 400 | `source_required` |
| `ErrUnsupportedToolchain` | 400 | `unsupported_toolchain` |
| `ErrInvalidParameter` / `ErrInvalidDefinition` | 400 | `invalid_request` |
| `ErrArtifactFetchFailed` | 500 | `internal`（真正的取字节故障，不吐内部原文） |

> HTTP 层哨兵（不是 `asset.` 领域码）：`errAuthUnavailable` → 503 `auth_unavailable`、
> `errAuditFailed` → 500 `audit_failed`、`errAuditUnavailable` → 503 `audit_unavailable`
> （审计读面未装配存储；读不到 ≠ 没有审批行，不许回空列表）。

> `contracts.ErrAbiVersionMismatch` **不在本服务的出口表里**：本仓不判定内核能力集，只在解析
> `abi_version` 时用 `ErrInvalidABIVersion`（400）。跨仓消费方按契约哨兵 `errors.Is` 判定的语义不变。

### 2.2 编译链路七类失败语义（G4 ①）

> 编译是**异步作业**：build 端点只回答「是否受理」，真正的成败在 `GET /api/strategies/{id}/builds/{job_id}`。
> 因此下表的「HTTP」列给的是**同步面**的返回码（多为 202），终态与归因看 job 的 `state` / `error_code`。
> 调用方必须按 `error_code` 分支，不要解析 `error` 文案。

| # | 类别 | 触发 | 同步面 HTTP | job 终态 / `error_code` | 是否重试 | 调用方该做什么 |
|---|---|---|---|---|---|---|
| 1 | `compile_unavailable` | 服务未装配编译器（`-compiler-bin` 为空）、配置非法，或派发时二进制不可启动 | build 503 `compile_unavailable` | 未落 job；**已落 queued 后**派发失败则 `failed` / `compile_unavailable` | **不自动重试**（同一请求不会自愈） | 停止重投，报运维修装配（bin 路径 / 权限 / 挂载）；修好后重投 |
| 2 | `ErrCompileFailed` | 工具链跑了但退出非零、产物缺失、单次工具链超时且作业级重试耗尽 | build 202 | `failed` / `compile_failed` | 作业级最多 3 次、固定 2s 退避（见 §3.1）；终态后不再重试 | 读 `error` 定位源码 / 工具链；改源码或升工具链后**用新版本**重投；同版本重投是可预期的同样失败 |
| 3 | `ErrSourceTooLarge` | 源码 > 1MiB（`PUT /source`） | 413 `payload_too_large` | 不创建 job（无终态） | 不重试 | 裁剪源码后重新上传；不要改请求头绕 |
| 4 | `ErrArtifactTooLarge` | 产物 > 8MiB（编译侧），或内部登记 > 8MiB | 编译侧 build 202；内部登记 413 `payload_too_large` | `failed` / `payload_too_large`（编译侧重试耗尽后上报） | 作业级重试不改变结果 | 优化产物；确需更大上界另立单改常量，**不要**靠重试 |
| 5 | 派发失败 | job 已落 `queued`，启动 `cmd/compiler` 失败（临时 job 文件写不了 / 进程 fork 失败） | build 503 `compile_unavailable` | `failed` / `compile_unavailable` | 不自动重试 | 排查磁盘 / 权限 / `-compiler-bin`，修好后重投；503 不是「稍后自然好」 |
| 6 | 回写失败 | `cmd/compiler` 无法 POST 内部端点（网络 / 鉴权 / 服务停机），或终态上报被拒 | build 202 | **非终态**：停在 `queued` 或 `running`，无 `error_code` | 编译器侧 running 上报重试 <=3 次；终态上报失败后**不再自动重试** | 继续按 job 轮询但设**停滞阈值**（进程上界 + 2min）；超阈值按 `docs/runbook.md` 人工收口为 `failed`，**不要假设作业还在跑** |
| 7 | 超时 | `cmd/compiler` 子进程超过硬上界 `-compiler-process-timeout`（默认 20m，最大 1h），到点由 `exec.CommandContext` 杀掉 | build 202 | `failed` / `compile_timeout` | **进程级不自动重试**（作业已被杀）；工具链单次超时仍按 §3.1 重试 | 轮询到 `compile_timeout` 后查卡点（锁文件 / 镜像拉取 / 网络解析）；确认是瞬时故障可重新提交（新 job），否则先修环境 |

补充口径：

- **超时上界取「批次预算」与 `-compiler-process-timeout` 的较小者**。批次预算 = `maxAttempts × (timeout + backoff) + 1min`
  （默认 3×(5m+2s)+1m ≈ 16m6s）；默认上界 20m 不截断正常重试，但多尝试 / 长超时的组合会被 20m 截断。
  上界常量在 `internal/compilerclient`（`DefaultProcessTimeout` / `MaxProcessTimeout`），flag 在 `cmd/assetservice`。
- **超时必须是 failed，不许留 running**：到点后等待方回写 `failed + compile_timeout`；回写本身失败就落到第 6 类，
  由 runbook 人工收口。这条是 G4 的核心代码约束（`TestDispatchBuild_ProcessDeadlineFailsJobWithCompileTimeout`）。
- `compile_timeout` 是新增 wire code（§4.1 MINOR），旧调用方忽略未知 `error_code` 即可；旧服务不会产生它。

## 3. 幂等性与重试

- `version_hash` / `artifact_hash` 内容寻址 → 重复上传同源码是幂等（同 hash 不新增行）。
- `build` 幂等键 = `(strategy_id, version_hash, toolchain)`（G1 ⑦）：同键已有 `queued` / `running` 作业时
  **复用同一个 `job_id`**，不排第二份；终态（`succeeded` / `failed`）不复用——重新提交会排新作业。
  已知上界：查重与落库不是同一事务，并发窗口内两次提交仍可能各排一份（未做唯一索引，登记在
  `docs/TASKS.md` SVC-STRAT-G1 施工记录）。
- `promote` / `deprecate` **不是幂等成功**：`promote` 要求 artifact 处于 `validated`、`deprecate` 要求
  `promoted`，重复调用返回 409 `invalid_transition`（实现如此；本文件旧句「重复调用结果一致」与代码不符，
  SVC-STRAT-G1 按实现订正）。
- `resolve?ref=hash` 无副作用，可任意重试。

### 3.1 编译作业重试边界（G4 ③）

**作业级（`cmd/compiler` 内部）**

- 次数：`-max-attempts` 默认 **3**（含首次；`1` = 不重试）。
- 退避：`-retry-backoff` 默认 **2s 固定退避**，不做指数退避（编译失败通常是确定性源码问题，指数只拉长终态时间）。
- 可重试：`ErrCompileFailed`（工具链非零退出、单次超时、产物缺失）、`ErrArtifactFetchFailed`（取源码瞬时故障）、
  以及默认 `DefaultRetryable()` 为真的其它错误。
- 不可重试：`ErrInvalidParameter` / `ErrInvalidDefinition` / `ErrUnsupportedCarrier` /
  `ErrInvalidABIVersion` / `ErrSourceTooLarge` / `ErrArtifactTooLarge` / `ErrArtifactHashMismatch` /
  `ErrCompileUnavailable` / 空服务响应 / 未知工具链——重试只会重复同一次失败。
- running 上报与编译尝试**分开计次**：上报失败不消耗编译尝试；终态上报失败不重试（见 §2.2 第 6 类）。

**进程级（`assetservice` 侧，G4 新增）**

- `cmd/compiler` 子进程只有**一次**机会，存活上界 = min(批次预算, `-compiler-process-timeout`)；到点杀掉并回写
  `failed + compile_timeout`，**不重启子进程**。理由：进程级挂死多是环境（锁 / 镜像 / 网络）问题，自动重投会掩盖故障。

**HTTP 调用方建议**

| 面 | 重试 | 退避 |
|---|---|---|
| 429 `rate_limited` | 是 | 按 `Retry-After` |
| 503 `compile_unavailable` / `auth_unavailable` | **先修部署**，修好后重投 1 次 | 1s / 2s / 4s，上限 30s，最多 5 次 |
| 409 `compile_failed` / `invalid_transition` | 否（确定性） | 改源码 / 换版本后重投 |
| 413 `payload_too_large` | 否 | 裁剪后重传 |
| 202 后轮询 job | GET 可重试（无副作用） | 1–2s 间隔；总预算 = 进程上界 + 2min，超时走人工收口 |
| 5xx `internal` / 网络错误 | 是（GET / PUT 幂等） | 指数 1s 起，上限 30s，最多 5 次 |

## 4. 版本与兼容

- artifact **永不失效**：引擎按 `artifact_hash` 缓存，服务停机也能跑已缓存的。
- 只有 `latest` 有 TTL —— **F 已裁决（2026-10-21）：TTL 300s**，promote 响应携带新 hash 供本进程主动失效，**不引 webhook / 消息总线**（源文 `tellurion:docs/design/qt-decoupling/strategy-source-of-truth-and-cache.md` 的裁决条 E/F/G/H）。
  原文那句「事件失效（要立 ADR）」随该裁决作废，不要再按「待立」对待。
  **已落地（2026-10-24）**：latest 结果按 strategy_id 缓存 300s，`promote` / `deprecate` 成功后同进程立即失效；
  artifact 字节走 L1 内存 LRU + L2 本地盘内容寻址（容量驱逐、内容自证），进程重启后已缓存 hash 仍按原字节返回。
- 端点废弃走 `deprecated` 头 + 一个 MINOR 周期。
- ⚠️ **semver 与 hash 是两套东西**：`strategy_ref` / `artifact_hash` / `version_hash` / `run_spec_hash` 自带版本语义（同 hash = 同内容），**不参与 semver**，也不因它们变化而 bump 版本号；
  本仓的 HTTP 面随 `tellurion-contracts` 的版本纪律走（口径源头 `tellurion:docs/TASKS.md` QT-19 ③，落地形态 `tellurion-contracts:README.md`）。

### 4.1 版本化与兼容（G1 ⑤）

- **URL 不带版本号**：`/api/strategies...` 与 `/internal/strategies...` 是冻结前缀，**不存在** `/v2` 并行面。
  版本身份由「本仓 tag + 依赖的 contracts 版本」共同钉住（今天 contracts `v0.9.0` / kit `v0.2.0`）。
- **加字段 = MINOR**：新增响应字段、新增可选请求字段、新增 wire code、新增端点，都是兼容变更。
  调用方必须忽略未知字段（本仓的解码器也是 `DisallowUnknownFields`，所以**请求侧加字段对旧服务是硬失败**——
  加请求字段必须在同一次发布里让服务先能接收）。
- **改语义 = MAJOR**：改变既有字段的含义、把可选改必填、把 200 改成别的成功码、改变状态机跃迁、改变错误码归属。
- **删端点 / 删字段**：标 `Deprecated` 响应头，保留**一个 MINOR 周期**再删。
- **hash 不参与 semver**：`version_hash` / `artifact_hash` 变化不代表服务版本变化；反过来，服务 MAJOR 也不代表
  历史 artifact 失效——artifact 永不失效（见 §4 第一条）。

### 4.2 分页（G1 ⑥）

- 唯一列表端点 `GET /api/strategies/{id}/versions` 支持 `?limit=&offset=`：
  `limit` 默认 **200**、上界 **1000**（`1..1000`），`offset` 默认 0、必须 `>= 0`；非法值 → 400 `invalid_request`。
- 响应仍是**裸数组**（不是 `{items,total}`）：`offset` 超过末尾返回 `[]`，不报错。
  排序是服务端稳定序（版本创建时间升序，同时间按 hash），调用方不要依赖「第 N 页」语义做增量同步。
- `GET /api/strategies/{id}/promotions` 同一套分页参数与裸数组形态，排序是**新的在前**（审计只能追加，
  按插入序倒排）；`version_hash` 是可选的精确匹配过滤。
- 其余端点没有列表语义（resolve / artifact / job 都是单条），不分页。

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

## 7. 逐端点版本与错误码（G1，闭合表）

> 版本口径见 §4.1：**全部端点的 wire 版本都是 `v1`**（前缀冻结、路径不带版本号），
> 表格里的「v1」不是另一个并行面，而是「本表描述的这套语义」。错误码只列**该端点自身会产生的**；
> 429 `rate_limited` 对所有非探针端点都成立，为可读性在表尾统一说明，不在每行重复。

| # | 端点 | 版本 | 该端点的错误码（除 429 / 500 外） |
|---|---|---|---|
| 1 | `POST /api/strategies` | v1 | 400 `invalid_request` / `unsupported_carrier` / `invalid_abi_version` / `invalid_scope`；409 `strategy_exists` |
| 2 | `PUT /api/strategies/{id}/source` | v1 | 400 `invalid_request` / `source_required`；404 `strategy_not_found`；409 `storage_unsupported`；413 `payload_too_large` |
| 3 | `POST /api/strategies/{id}/build` | v1 | 400 `invalid_request` / `unsupported_carrier` / `unsupported_toolchain`；404 `version_not_found`；503 `compile_unavailable` |
| 4 | `POST /api/strategies/{id}/versions/{hash}/validate` | v1 | 400 `invalid_request` / `unsupported_toolchain`；404 `strategy_not_found` / `version_not_found` / `artifact_not_found`；409 `ambiguous_artifact` / `artifact_invalid` / `imports_not_allowed` |
| 5 | `POST /api/strategies/{id}/versions/{hash}/promote` | v1 | 400 `invalid_request` / `actor_required`；401 `unauthenticated`；403 `forbidden`；404 `strategy_not_found` / `version_not_found` / `artifact_not_found`；409 `ambiguous_artifact` / `artifact_invalid` / `imports_not_allowed` / `invalid_transition`；500 `audit_failed`；503 `auth_unavailable` |
| 6 | `POST /api/strategies/{id}/versions/{hash}/deprecate` | v1 | 400 `invalid_request`；404 `strategy_not_found` / `version_not_found` / `artifact_not_found`；409 `ambiguous_artifact` / `invalid_transition` |
| 7 | `GET /api/strategies/{id}/versions` | v1 | 400 `invalid_request`（分页参数）；404 `strategy_not_found` |
| 8 | `GET /api/strategies/{id}/builds/{job_id}` | v1 | 404 `job_not_found` |
| 9 | `GET /api/strategies/{id}/resolve` | v1 | 400 `invalid_request`（坏 ref）；404 `strategy_not_found` / `version_not_found` / `no_active_version` / `artifact_not_found` |
| 10 | `GET /api/strategies/{id}/versions/{hash}/artifact` | v1 | 400 `invalid_request`；404 `strategy_not_found` / `version_not_found` / `artifact_not_found`；409 `ambiguous_artifact` / `storage_unsupported` |
| 11 | `POST /internal/strategies/{id}/artifacts` | v1 | 400 `invalid_request`；401 `unauthenticated`；403 `forbidden`；404 `strategy_not_found` / `version_not_found`；409 `conflict` / `artifact_invalid` / `imports_not_allowed`；413 `payload_too_large`；503 `auth_unavailable` |
| 12 | `POST /internal/strategies/{id}/builds/{job_id}` | v1 | 400 `invalid_request`；401 `unauthenticated`；403 `forbidden`；404 `job_not_found`；409 `invalid_transition` / `artifact_invalid`；503 `auth_unavailable` |
| 13 | `GET /healthz` | v1 | 仅 200（探针不限流） |
| 14 | `GET /readyz` | v1 | 503（`status=not_ready`，探针不限流；不是 wire code） |
| 15 | `GET /api/strategies/{id}/promotions` | v1 | 400 `invalid_request`（分页参数）；503 `audit_unavailable`（审计读面未装配） |

- **429 `rate_limited`**：1–12 与 15 全部适用（按身份令牌桶，超限带 `Retry-After`）。探针 13–14 豁免。
- **500 `internal`**：所有端点的兜底，只用于真正的服务端故障；promote 的审计故障用更精确的 `audit_failed`。
- **401 / 403 / 503 `auth_unavailable`**：只出现在受保护的三条（5、11、12）。
