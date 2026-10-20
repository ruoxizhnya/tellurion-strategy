# 本仓边界与依赖方向 — tellurion-strategy

## 定位

策略**资产**服务：策略定义 / 版本 / artifact / resolve（编译与执行三分里的"资产"这一分）。**策略运行时不做成服务**——逐 bar 执行留在 core 进程内。

状态：**预留（S0 占位，触发式启动）——尚未启动，无实现**

## 依赖方向（编译器强制）

```
contracts + kit
        ↑
tellurion-strategy  ──(HTTP)──▶  core（运行时拉 artifact，执行在 core）
```

- 箭头 = import 方向。**不允许反向**，也不允许成环 —— 反向 import 应当**编译失败**，这是判据不是口号。
- 跨仓只认版本号：`tellurion-strategy` 以需要的版本出现在下游 `go.mod`。

## 边界表

| | 内容 |
|---|---|
| 允许 | 策略定义、版本、artifact 登记与解析（`carrier=dsl / wasm`）；策略资产的内容寻址与回滚 |
| 禁止 | 策略运行时（逐 bar / 逐笔执行）——那是 core 进程内的 WASM host，永不做成服务；import core 的实现包；把引擎状态搬进本仓 |

## 关键约束

1. **单仓可构建**：`go build ./...` / `go vet ./...` / `go test ./...` 不依赖任何兄弟目录。
2. **无 `replace`**：指向仓外的 `replace` 一律不接受（CI 会查）。
3. **无 `go.work` 入库**：本地联调可以建，提交即错。
4. **资产 ≠ 运行时**：任何"在服务里跑策略"的设计都是错的（ADR-D6 的硬边界）。

## 未决 / 待定

- `carrier` 的两种形态（DSL / WASM）谁先落地，取决于 W3 的 M0 PoC 结论。
