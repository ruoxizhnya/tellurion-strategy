---
status: active
last-verified: 2026-10-23
verified-by: 首批实装链路对表（source→version_hash→build→verify→promote→resolve→artifact 下载）
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
  POST .../build ──► cmd/compiler ──┬─ internal/compile.Runner（toolchain，-target=wasm-unknown）
                                    └─ internal/compilerjob ──► POST /internal/strategies/{id}/artifacts
                                                              （base64 字节 + hash 自证 + imports 自证）
```

## 2. 关键时序

1. `POST /api/strategies` 建定义（`carrier` / `abi_version` / `scope` / `capability` / `entry`）
2. `PUT .../source` 上传源码 → `VersionHash`（canonical 化后 sha256，域 `tellurion.strategy_version_hash.v1`）→ 登记 `strategy_versions`
3. `POST .../build` → 返回 `job_id` → `cmd/compiler` 编译 → 内部端点登记 artifact → 写 `strategy_artifacts` + blobs
4. `POST .../versions/{hash}/validate` → `Verify`：storage 分支 / 空字节 / 8 MiB / size 一致 / sha256 / abi 可解析 / memory 上限 / imports 白名单 → `built→validated`
5. `POST .../versions/{hash}/promote` → 写 `active_version_hash` + `active_artifact_hash`（`validated→promoted`）
6. 回测时：`GET .../resolve?ref=id@hash|latest|裸hash` → 元数据 → `GET .../artifact` 拉字节码（ETag = `artifact_hash`）
7. 引擎按 `artifact_hash` 缓存：**同 hash 第二次不再请求**

## 3. 失败模式（每一处 fail-closed 都要能指名）

| 失败点 | 症状 | 兜底 |
|---|---|---|
| 服务停机 | 回测跑不了 | 已缓存 hash 照跑（验收项） |
| 源码改但 hash 没变 | 版本混乱 | canonical 化规则固定；`VersionHash` 输入集冻结 |
| 校验被绕过 | 跑不可信字节码 | `promote` 只能从 `validated` 出发，`built`/`rejected` 一律 409 |
| 无 active 版本 | 引擎静默用内置策略 | **禁止**：404 `no_active_version` |
| 版本 hash 不存在 | 引擎回退 latest | **禁止**：404 `version_not_found` |
| 同版本多工具链且未指定 | 服务替调用方猜 | 409 `ambiguous_artifact` |
| 字节被篡改 | 跑错字节码 | 下载前 `ArtifactHash` 校验；`X-Artifact-Hash` 与 ETag 同源 |
| 编译服务回填撒谎 | 登记错 artifact | 内部端点重算 sha256 + 重解析 imports，不符 409 `artifact_invalid` |

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
