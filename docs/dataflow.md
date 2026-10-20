---
status: active
last-verified: 2026-10-20
verified-by: 功能边界固化（三份契约：db / api / dataflow）
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
```

## 2. 关键时序

1. `POST /api/strategies` 建定义（`carrier` / `scope` / `capability`）
2. `PUT .../source` 上传源码 → 算 `version_hash`（canonical 化后 sha256）
3. `POST .../build` → 编译 job → 产出字节码 → `artifact_hash`
4. `POST .../validate` → imports 白名单 / 体积 / 内存三项
5. `POST .../promote` → 写 `active_version_hash`
6. 回测时：`GET .../resolve?ref=id@hash` → 元数据 → `GET .../artifact` 拉字节码
7. 引擎按 `artifact_hash` 缓存：**同 hash 第二次不再请求**

## 3. 失败模式（每一处 fail-closed 都要能指名）

| 失败点 | 症状 | 兜底 |
|---|---|---|
| 服务停机 | 回测跑不了 | 已缓存 hash 照跑（验收项） |
| 源码改但 hash 没变 | 版本混乱 | canonical 化规则固定；CI 比对 |
| 校验被绕过 | 跑不可信字节码 | `validate` 是 promote 的前置，无法跳过 |
| 无 active 版本 | 引擎静默用内置策略 | **禁止**：fail-closed 拒绝 |

## 4. 不变量（可被测试钉住的）

1. `version_hash` 只由源码内容决定（同源码同 hash）。
2. `artifact_hash` 只由字节码决定。
3. `promote` 不改历史 artifact 行。
4. 停机后，已缓存 artifact 的回测结果与在线时逐位一致。

## 5. 与契约文档的关系

- 表结构 → `docs/db-schema.md`
- 接口形状 → `docs/api-schema.md`
- 本文件只讲**流向与顺序**，不重复字段定义。
