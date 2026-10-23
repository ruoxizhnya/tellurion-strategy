---
status: active
last-verified: 2026-10-24
verified-by: G4 链路对表 + G5 编译作业队列（限并发 / 重试 / 终态可检索，状态词表对齐 strategy_build_jobs）
---

# Dataflow — tellurion-strategy

> 目的：把「数据从哪来、在谁手里变形、到哪去」写死。**任何跨仓数据交换都必须能在本图上找到一条边**；找不到 = 这段代码在偷偷建第二条路。

## 1. 主链路

```
作者 / AI pipeline / CI
        │  POST source / build / validate / promote
        ↓
  tellurion-strategy  ── 写 strategy.* ──►  （schema 唯一 owner）
        │  GET resolve / artifact（读侧只这两个）
        ↓
      core 引擎  ── 按 artifact_hash 缓存（永不失效）──►  执行（进程内 WASM host）

编译支线（QT-41b，独立进程）：
  job JSON 批次 ──► cmd/compiler
                     ├─ internal/compilerjob.Queue（queued → running → succeeded | failed；
                     │    -workers 限并发、-max-attempts 重试、终态带 error 可检索）
                     ├─ internal/compile.Runner（toolchain，-target=wasm-unknown）
                     └─ POST /internal/strategies/{id}/artifacts
                        （base64 字节 + hash 自证 + imports 自证）

  资产服务的 POST /api/strategies/{id}/build 是另一条入口：先落 strategy_build_jobs
  （queued）再在**本进程内**调 asset.Compiler。今天 cmd/assetservice 未装配编译器 ⇒
  该端点恒 503 compile_unavailable；编译进程与 strategy_build_jobs 行的状态回写
  尚未接线（见 TASKS.md QT-41b G5 余项）。
```

## 2. 关键时序

0. 启动（`cmd/assetservice`）：`PG.Migrate`（幂等 DDL）→ `RegisterBuiltins`（14 个内置策略同步登记，
   `carrier=builtin`、无 artifact；G 裁决：保留 core 的 init 注册，不做全量迁移）→ 开始监听。
   已存在同名定义只核对形状、不覆盖；同名不同 carrier/scope/entry = 启动失败。
1. `POST /api/strategies` 建定义（`carrier` / `abi_version` / `scope` / `capability` / `entry`）
2. `PUT .../source` 上传源码 → `VersionHash`（canonical 化后 sha256，域 `tellurion.strategy_version_hash.v1`）→ 登记 `strategy_versions`
3. 资产服务的 `POST .../build` → 先落 `strategy_build_jobs`（queued）→ 在**本进程内**调 `asset.Compiler` →
   `GET .../builds/{job_id}` 轮询 → 终态写回该行。**现状**：`cmd/assetservice` 未装配编译器 ⇒ 恒 503
   `compile_unavailable`；资产服务与独立编译进程之间的派发 / 状态回写尚未接线（见 `TASKS.md` QT-41b G5 余项）。
3b. 独立编译进程：`cmd/compiler -job … [-job …]` → `compilerjob.Queue`（`-workers` 限并发、`-max-attempts` 重试）
   → 内部端点登记 artifact → 逐作业终态经 stdout / `-report` 输出；状态词表与 `strategy_build_jobs.state`
   同一套（queued / running / succeeded / failed），作业可带资产服务签发的 `job_id` 作记录主键。
4. `POST .../versions/{hash}/validate` → `Verify`：storage 分支 / 空字节 / 8 MiB / size 一致 / sha256 / abi 可解析 / memory 上限 / imports 白名单
   → wasm 再验 import section；so 再验导出符号表（`entry` 必须是已定义 GLOBAL/WEAK 符号）→ `built→validated`
5. `POST .../versions/{hash}/promote` → 写 `active_version_hash` + `active_artifact_hash`（`validated→promoted`）
6. 回测时：`GET .../resolve?ref=id@hash|latest|裸hash` → 元数据 → `GET .../artifact` 拉字节码（ETag = `artifact_hash`）
7. 引擎按 `artifact_hash` 缓存：**同 hash 第二次不再请求**
8. 服务侧缓存（E/F 裁决，2026-10-24 落地）：
   - artifact 字节：L1 内存 LRU → L2 本地磁盘内容寻址（`<dir>/<hash[:2]>/<hash>`）→ 回源 PG；L2 读出必须重算 sha256 与文件名一致，
     不一致当 miss 并删文件。artifact **永不失效**（同 hash 同内容），只按容量驱逐。
   - `resolve?ref=latest`：结果按 strategy_id 缓存 **TTL 300s**；`promote` / `deprecate` 成功后在同一进程内主动失效
     （响应已携带新 hash，不引 webhook / msgbus）。

## 3. 失败模式（每一处 fail-closed 都要能指名）

| 失败点 | 症状 | 兜底 |
|---|---|---|
| 服务停机 | 回测跑不了 | 已缓存 hash 照跑（验收项）：服务侧的 L2 内容寻址缓存跨进程重启复用；字节回源不可用时，已缓存 hash 仍按原字节返回 |
| 缓存被篡改 | 跑错字节码 | L1 是进程内副本；L2 读出重算 sha256，与文件名不符即删并 miss，绝不返回 |
| latest 指针已变但缓存未过期 | 5 分钟内跑旧版本 | promote / deprecate 同进程主动失效；跨进程最多 300s（F 裁决的显式取舍） |
| 源码改但 hash 没变 | 版本混乱 | canonical 化规则固定；`VersionHash` 输入集冻结 |
| 校验被绕过 | 跑不可信字节码 | `promote` 只能从 `validated` 出发，`built`/`rejected` 一律 409 |
| 无 active 版本 | 引擎静默用内置策略 | **禁止**：404 `no_active_version` |
| 版本 hash 不存在 | 引擎回退 latest | **禁止**：404 `version_not_found` |
| 同版本多工具链且未指定 | 服务替调用方猜 | 409 `ambiguous_artifact` |
| 字节被篡改 | 跑错字节码 | 下载前 `ArtifactHash` 校验；`X-Artifact-Hash` 与 ETag 同源 |
| 编译服务回填撒谎 | 登记错 artifact | 内部端点重算 sha256 + 重解析 imports，不符 409 `artifact_invalid` |
| 编译失败 / 工具链抖动 | 单作业失败（坏 job 不带走整批） | `compilerjob.Queue` 按 `-max-attempts` 重试；确定性失败（形状 / 超限 / hash 不符 / 服务未配）立即终态不重试；终态 `failed` 带 `job_id` + `attempts` + `error` 可检索 |
| 重试会拖长批次 | 排队作业相互等待 | `-workers` 控并发；批次 ctx 预算按 `ceil(N/workers) × maxAttempts × (timeout+backoff) + 1min` 计算，超时按失败收口 |

## 4. 不变量（可被测试钉住的）

1. `version_hash` 只由源码内容 + 定义字段决定（同输入同 hash）。
2. `artifact_hash` 只由字节码决定。
3. `promote` / `deprecate` 不改历史 artifact 字节行。
4. 停机后，已缓存 artifact 的回测结果与在线时逐位一致。
5. `resolve` 返回的 `artifact_hash` 与 `artifact` 端点 `X-Artifact-Hash` 恒等。

## 5. 与契约文档的关系

- 表结构 → `docs/db-schema.md`
- 接口形状 → `docs/api-schema.md`
- 本文件只讲**流向与顺序**，不重复字段定义。
