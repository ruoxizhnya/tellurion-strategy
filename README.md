# tellurion-strategy

> module: `tellurion-strategy` ｜ 状态：**预留（触发式）**——当前为占位仓，**未启动**。触发条件同 X 线通则；启动前先立单。

策略资产的**真相源**：定义、版本、artifact 构建与解析、参数 schema。运行时（`on_bar`、cursor、WASM 实例）留在 core。

这是「切一半」的仓：`pkg/strategy` 的定义/资产侧出来，**运行时侧**留下来。整包搬走会让策略执行变远程调用。

## 边界

**可以**
- 持有 `strategy` schema 的 DDL
- 管理 artifact 与版本（`artifact_hash` 是执行真相，`version_hash` 只是人读标签）

**不可以**
- 在回测内循环里被执行（逐 bar 远程 = 硬否决）
- 覆盖已 promote 的 artifact 行（promote 只能新增指针）

## 依赖方向

```
tellurion-strategy
```

- 契约与工具一律以 **tag** 依赖（`github.com/ruoxizhnya/tellurion-contracts` / `-kit`），
  **禁止 `replace` 指向仓外路径**（单仓不可构建）。
- `go.work` 一律不入库。

## 施工入口

本仓所有任务登记在 **`tellurion` 仓的 `docs/TASKS.md`**（单一台账，避免双写）。
在台账出现本仓的单号之前，**不要在此写实现**。

## 权威文档

| 主题 | 位置 |
|---|---|
| 仓清单与命名 | `tellurion:docs/design/qt-decoupling/repo-inventory.md` |
| 分仓拓扑与阶梯 | `tellurion:docs/design/qt-decoupling/repo-topology-and-naming.md` |
| P0 修复基线 | `tellurion:docs/design/qt-decoupling/p0-round2-review.md` |
