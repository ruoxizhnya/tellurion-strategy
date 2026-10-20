---
status: active
last-verified: 2026-10-20
verified-by: 功能边界固化（三份契约：db / api / dataflow）
---

# API Schema — tellurion-strategy

> **实现状态（2026-10-20）**：本仓目前**只有占位 `doc.go`，没有任何 handler**——下列端点**全部未实现**。
> 本文件是**目标契约**，不是现状说明书；开工顺序见 `docs/TASKS.md` 的「施工序」。

> **契约地位**：HTTP。**写侧 7 端点给作者/AI/CI，读侧 2 端点给引擎** —— 引擎只用那两个，其余端点它不该碰。
> 本文件是**本仓对外接口的唯一真相源**。代码与它不一致时，先判断谁错：契约错 → 改本文 + 立 ADR；实现错 → 改代码。
> 跨仓消费方（core / agent / ui / 其他服务）按本文件编码，**不读本仓实现**。

## 1. 接口面

### 写侧（作者 / AI / 编译服务）

| 方法 | 路径 | 用途 |
|---|---|---|
| POST | `/api/strategies` | 创建定义（carrier / abi_version / **scope** / capability / entry） |
| PUT | `/api/strategies/{id}/source` | 上传源码 → 算 `version_hash` 并登记版本 |
| POST | `/api/strategies/{id}/build` | 触发编译（异步 job）→ artifact + `artifact_hash` |
| POST | `/api/strategies/{id}/versions/{hash}/validate` | 静态校验（imports 白名单 / 体积 / 内存上限） |
| POST | `/api/strategies/{id}/versions/{hash}/promote` | 标为可用（写 `active_version_hash`） |
| POST | `/api/strategies/{id}/versions/{hash}/deprecate` | 下线（不删，历史回测仍可解析） |
| GET | `/api/strategies/{id}/versions` | 版本列表（status / hash / toolchain） |

### 读侧（**引擎只用这两个**）

| 方法 | 路径 | 用途 |
|---|---|---|
| GET | `/api/strategies/{id}/resolve?ref={hash\|latest}` | 解析 `strategy_ref` → 元数据（含 `artifact_hash` / `scope` / `capability`） |
| GET | `/api/strategies/{id}/versions/{hash}/artifact` | 下载字节码（响应头 `X-Artifact-Hash`，支持 `ETag`） |

## 2. 错误码与 fail-closed 语义

| 情况 | 返回 | 调用方行为 |
|---|---|---|
| 无 active 版本 | 404 + `no_active_version` | fail-closed 拒绝跑 |
| artifact 校验不过 | 409 + `artifact_invalid` | 不许执行 |
| `abi_version` 与内核能力不匹配 | 409 + `abi_mismatch` | 拒绝，不降级 |
| hash 不存在 | 404 + `version_not_found` | 拒绝（**不回退 latest**） |

## 3. 幂等性与重试

- `version_hash` / `artifact_hash` 内容寻址 → 重复上传同源码是幂等（同 hash 不新增行）。
- `build` 是异步 job：同 `(id, version_hash, toolchain)` 重复提交应复用已有 artifact。
- `promote` / `deprecate` 幂等（重复调用结果一致）。
- `resolve?ref=hash` 无副作用，可任意重试。

## 4. 版本与兼容

- artifact **永不失效**：引擎按 `artifact_hash` 缓存，服务停机也能跑已缓存的。
- 只有 `latest` 有 TTL / 事件失效（要立 ADR 记录失效策略）。
- 端点废弃走 `deprecated` 头 + 一个 MINOR 周期。

## 5. 不提供什么（边界）

- 不做信号计算、不做回测
- 不做策略运行时（逐 bar 执行在 core 的 WASM host）
- 不提供"策略市场"式搜索（不在本阶段）
- 不做第三方鉴权（`authz` 在 kit，策略权限模型另立）
