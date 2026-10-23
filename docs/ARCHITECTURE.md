---
status: active
last-verified: 2026-10-23
verified-by: 首批实装包布局对表（internal/asset · httpapi · compile · compilerjob · cmd/assetservice · cmd/compiler）
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

## 3.1 包布局（现状，2026-10-23）

```
cmd/
  assetservice/      进程入口：Memory Store + httpapi.New + http.Server（127.0.0.1:8085）
  compiler/          进程入口：读 job JSON → internal/compile.Runner → internal/compilerjob.Run → stdout 回执
internal/
  asset/             领域核心：模型 / VersionHash / Verify / WasmImports / ImportPolicyFor
                     / Store 端口 + Memory 实现 / Migrations()（5 条幂等 DDL）/ Service（9 端点语义）
  httpapi/           传输层：标准库 http.ServeMux，9 个公开端点 + 1 个内部登记端点，wire code 映射
  compile/           Compile 门：Toolchain 执行器（-target=wasm-unknown、env 白名单、超时、日志截断）
  compilerjob/       编译作业管线：Job → 编译 → artifact_hash → 内部端点回填
doc.go               仓级包文档（无代码）
```

**依赖方向单一**：`cmd/*` → `internal/httpapi` → `internal/asset`；`cmd/compiler` → `internal/compilerjob` → `internal/compile`。
`internal/asset` 不 import `internal/compile` 与 `internal/compilerjob`（编译是本服务的外部能力，通过 `asset.Compiler` 端口注入）。

**未落**：PG `Store` 实现与迁移执行入口（当前只有 `Memory`）；build job 持久化；latest TTL 与两级缓存。清单见 `docs/TASKS.md` QT-41 施工记录。

## 4. 三份契约（功能的边界就在这三份文件里）

| 维度 | 文件 | 回答什么问题 |
|---|---|---|
| 数据形状 | [`db-schema.md`](db-schema.md) | 本仓拥有哪些表？谁能写？ |
| 接口形状 | [`api-schema.md`](api-schema.md) | 对外暴露什么？错误码与幂等怎么做？ |
| 流向与顺序 | [`dataflow.md`](dataflow.md) | 数据怎么流？哪里 fail-closed？ |

## 5. 依赖方向

```
contracts（strategy_ref / ArtifactHash / HostABIContracts）  +  kit（logging/http/db，尚未启用）
        ↑
tellurion-strategy  ──(HTTP 读)──►  core 引擎
        │
        └── 依赖编译工具链（TinyGo / go）—— 只在 build 侧（cmd/compiler）
```

- **contracts `v0.1.0` 是已发的 tag**（不是伪版本）。`engine` 子包（package 名仍是 `contracts`）提供 `StrategyRef` /
  `ParseStrategyRef` / `ArtifactHash` / `HostABIContracts` / 两个 abi 哨兵 —— 本仓**不复制**这些符号。
- **不依赖 `tellurion-kit`**：当前实现用标准库 `net/http` + 内存 Store，没有日志/存储连接需求；引入前要先在 `AGENTS.md` §4 登记。
- 禁止 `replace` 指向仓外路径、禁止提交 `go.work` —— 单仓必须能独立构建。

## 6. 关键决策

- [`adr/0001-资产服务不等于运行时.md`](adr/0001-资产服务不等于运行时.md)
- [`adr/0002-artifact-主键与生效字节的裁决.md`](adr/0002-artifact-主键与生效字节的裁决.md)（偏差 #1 七列 + artifact 主键 + 生效字节指针）

## 7. 非目标（明确不做）

- 不做策略运行时（逐 bar 出信号在 core 进程内）
- 不做回测
- 不做策略推荐 / 市场
- 不做信号存储

## 8. 分期与状态

| 阶段 | 内容 | 状态 |
|---|---|---|
| 腿 1 | DSL 资产化（零工具链）：`carrier=dsl` + `version_hash` + `strategy_ref` | ✅ `version_hash` / `strategy_ref` 解析已落地（2026-10-23） |
| 腿 2 | WASM：工具链 + PoC → go/no-go | 🔶 执行器与装配已落地，真工具链镜像待接 |
| X2 | 本仓完整落地（写 7 + 读 2 端点） | 🔶 9+1 端点已实装（`Memory` Store）；PG Store / 迁移执行 / 缓存 / 内置策略登记未落 |
