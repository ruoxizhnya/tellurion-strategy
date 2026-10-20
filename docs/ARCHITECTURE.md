---
status: active
last-verified: 2026-10-20
verified-by: 功能边界固化（三份契约：db / api / dataflow）
---

# 架构 — tellurion-strategy

## 1. 定位

策略**资产**服务：定义 / 版本 / artifact / resolve。

它解决的是"策略的真相源在哪"——今天策略编译进二进制，改了要重新发版；未来 AI 生成的策略需要一个可版本化、可校验、可回滚的载体。**但执行不在这里**。

## 2. 分层

```
写侧（作者 / AI / CI）                读侧（引擎）
  create / source / build               resolve —— 解析 ref → 元数据
  validate / promote / deprecate        artifact —— 下载字节码
        │                                     │
        ↓                                     ↓
  strategy.strategy_definitions / _artifacts / _artifact_blobs
                    （DDL 唯一 owner = 本仓）
```

## 3. 组件职责

| 组件 | 职责 | 不做 |
|---|---|---|
| 定义管理 | carrier / scope / capability / entry | 不解释策略逻辑 |
| 版本与 hash | canonical 化 + sha256 | 不改源码内容 |
| 编译（build） | 触发编译 job，产出 artifact | 不做运行时 |
| 校验（validate） | imports / 体积 / 内存 | 不做安全沙箱（那是 core） |
| 解析（resolve） | ref → 元数据 | 不缓存字节码（引擎自己缓存） |

## 4. 三份契约（功能的边界就在这三份文件里）

| 维度 | 文件 | 回答什么问题 |
|---|---|---|
| 数据形状 | [`db-schema.md`](db-schema.md) | 本仓拥有哪些表？谁能写？ |
| 接口形状 | [`api-schema.md`](api-schema.md) | 对外暴露什么？错误码与幂等怎么做？ |
| 流向与顺序 | [`dataflow.md`](dataflow.md) | 数据怎么流？哪里 fail-closed？ |

## 5. 依赖方向

```
contracts（strategy_ref 形状）+ kit（logging/http/db）
        ↑
tellurion-strategy  ──(HTTP 读)──►  core 引擎
        │
        └── 依赖编译工具链（TinyGo / go）—— 只在 build 侧
```

## 6. 关键决策

- [`adr/0001-资产服务不等于运行时.md`](adr/0001-资产服务不等于运行时.md)

## 7. 非目标（明确不做）

- 不做策略运行时（逐 bar 出信号在 core 进程内）
- 不做回测
- 不做策略推荐 / 市场
- 不做信号存储

## 8. 分期与状态

| 阶段 | 内容 | 状态 |
|---|---|---|
| 腿 1 | DSL 资产化（零工具链）：`carrier=dsl` + `version_hash` + `strategy_ref` | 可立即做 |
| 腿 2 | WASM：工具链 + PoC → go/no-go | 依赖 M0 PoC |
| X2 | 本仓完整落地（写 7 + 读 2 端点） | ⬜ |
