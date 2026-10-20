# AGENTS — tellurion-strategy

## 这是什么仓

策略资产的**真相源**：定义、版本、artifact 构建与解析、参数 schema。运行时（`on_bar`、cursor、WASM 实例）留在 core。

状态：**预留（触发式）**——当前为占位仓，**未启动**。触发条件同 X 线通则；启动前先立单。

## 构建 / 测试

```bash
go build ./...
go vet ./...
go test ./...
```

本仓必须**单独 checkout 就能跑通**这三条（R1/R2）。若某一步需要别的仓的源码目录，
那是**契约画错了**，不是正常状态——停下来先修边界，不要加 `replace ../xxx`。

## 边界（硬约束）

允许：
- 持有 `strategy` schema 的 DDL
- 管理 artifact 与版本（`artifact_hash` 是执行真相，`version_hash` 只是人读标签）

禁止：
- 在回测内循环里被执行（逐 bar 远程 = 硬否决）
- 覆盖已 promote 的 artifact 行（promote 只能新增指针）
- `replace` 指向仓外路径；`go.work*` 提交入库。
- 引入未在本文件声明的第三方依赖（先改本文件，再改代码）。

## 依赖的契约版本

| 依赖 | 版本 | 说明 |
|---|---|---|
| `github.com/ruoxizhnya/tellurion-contracts` | 待定（S2 后 `v0.1.0`） | 契约变更只走 tag，不走源码路径 |
| `github.com/ruoxizhnya/tellurion-kit` | 待定（S2 后 `v0.1.0`） | 同上 |

## 该做什么

**唯一台账是 `tellurion` 仓的 `docs/TASKS.md`。** 本仓没有独立 TASKS，
跨仓只留指针、不镜像正文（避免双写漂移）。

开工前先读：
1. `tellurion:docs/design/qt-decoupling/repo-inventory.md` —— 仓名与 module 规则
2. `tellurion:docs/TASKS.md` —— 找本仓的单号；没有单号就别开工
3. `tellurion:docs/design/qt-decoupling/p0-round2-review.md` §4 —— wave 顺序
