---
status: active
last-verified: 2026-10-25
verified-by: QT-41c 跨进程编译闭环——新增 `internal/compilerclient` + `cmd/assetservice` 外部编译器装配 + 内部 build job 状态回写端点（依赖方向仍单向）
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
  strategy.strategy_definitions / _versions / _artifacts / _artifact_blobs / _build_jobs
                    （DDL 唯一 owner = 本仓）
```

## 3. 组件职责

| 组件 | 职责 | 不做 |
|---|---|---|
| 定义管理 | carrier / scope / capability / entry | 不解释策略逻辑 |
| 版本与 hash | canonical 化 + sha256 | 不改源码内容 |
| 编译（build） | 触发编译 job，产出 artifact | 不做运行时 |
| 校验（validate） | imports / 体积 / 内存 | 不做安全沙箱（那是 core） |
| 解析（resolve） | ref → 元数据；latest 结果按 TTL 缓存 | 不缓存字节码给引擎（引擎自己缓存） |
| 缓存（cache） | artifact 字节 L1 内存 LRU + L2 本地盘内容寻址；latest TTL 300s | 不改写 artifact / 不参与状态机 |

## 3.1 包布局（现状，2026-10-25）

```
cmd/
  assetservice/      进程入口：DSN → asset.OpenPG + PG.Migrate → httpapi.New + http.Server（127.0.0.1:8085）；
                     DSN 为空才用 Memory（日志标注非持久化）。`-compiler-bin` 非空时装配
                     internal/compilerclient（外部编译进程）；为空则 Compiler=nil，build 端点 503
  compiler/          进程入口：读一批 job JSON → internal/compilerjob.Queue → internal/compile.Runner
                     → 内部端点登记 artifact / 回写 running 与终态 → stdout / -report 输出逐作业终态与失败原因
internal/
  asset/             领域核心：模型 / VersionHash / Verify / WasmImports / ImportPolicyFor
                     / Store 端口 + Memory 与 PG 实现 / Migrations()（幂等 DDL）/ Service（9 端点语义）
                     / cache.go（L1 内存 LRU + L2 磁盘内容寻址 + latest TTL）
                     / builtin.go（14 个内置策略的 carrier=builtin 同步登记）
                     / elf.go（carrier=so 的导出符号表校验：entry 必须是已定义 GLOBAL/WEAK 符号）
  httpapi/           传输层：标准库 http.ServeMux，10 个公开端点 + 2 个内部端点（artifact 登记 / job 状态回写）
  compile/           Compile 门：Toolchain 执行器（-target=wasm-unknown、tinygo 强制 -no-debug、env 白名单、
                     toolchains.lock 二进制 sha256、超时、日志截断）
  compilerjob/       编译作业管线：Queue（批次排队 / 限并发 / 重试，状态词表与
                     strategy_build_jobs 同一套，终态可检索）→ 编译 → artifact_hash → 内部端点回填
                     + StatusReporter（running / 终态回写内部端点，error_code 归因）
  compilerclient/    资产服务侧的编译端口：实现 asset.Compiler + asset.BuildDispatcher，把作业写成
                     临时 job JSON 并启动 cmd/compiler；Compile 不可用（派发模式）
doc.go               仓级包文档（无代码）
```

**依赖方向单一**：`cmd/*` → `internal/httpapi` → `internal/asset`；`cmd/compiler` → `internal/compilerjob` → `internal/compile`；
`cmd/assetservice` → `internal/compilerclient` → `internal/asset`（只走 `asset.Compiler` / `asset.BuildDispatcher` 端口）。
`internal/asset` 不 import `internal/compile` 与 `internal/compilerjob`（编译是本服务的外部能力，通过 `asset.Compiler` 端口注入）。
`internal/asset` 的存储依赖只有 `pgx`（PG 实现）；领域模型不出现 `pgx` 类型。

**事务边界**：`promote` / `deprecate` 的「artifact 状态 + definition 生效指针」在 Store 内单事务提交
（PG）或单锁内完成（Memory）；HTTP/Service 层不再分两步写。

**未落**：`storage=url` 分支（只保留列）、`carrier=external` 的 hash 语义单独立文。清单见 `docs/TASKS.md`。
（`version_hash` 的输入形状与域标签已于 G5 ③ 收编 contracts `v0.5.0`；`internal/asset/hash.go` 不再持有本地定义。）

## 4. 三份契约（功能的边界就在这三份文件里）

| 维度 | 文件 | 回答什么问题 |
|---|---|---|
| 数据形状 | [`db-schema.md`](db-schema.md) | 本仓拥有哪些表？谁能写？ |
| 接口形状 | [`api-schema.md`](api-schema.md) | 对外暴露什么？错误码与幂等怎么做？ |
| 流向与顺序 | [`dataflow.md`](dataflow.md) | 数据怎么流？哪里 fail-closed？ |

## 5. 依赖方向

```
contracts（strategy_ref / ArtifactHash / HostABIContracts）  +  pgx（PG Store）
        ↑
tellurion-strategy  ──(HTTP 读)──►  core 引擎
        │
        ├──(exec)──►  cmd/compiler（独立进程）
        │                 └──(HTTP 内部端点)──►  tellurion-strategy（artifact 登记 / build job 状态回写）
        └── 依赖编译工具链（tinygo 0.34.0 镜像 / gcc）—— 只在 build 侧（cmd/compiler）
```

- **跨进程编译不引入反向依赖**：`cmd/assetservice` 只通过 `internal/compilerclient` 启动 `cmd/compiler`；
  编译进程只经 HTTP 内部端点回写，不直连 DB、不 import 资产服务实现。
- **contracts `v0.4.0` 是已发的 tag**（2026-10-24，不是伪版本）。`engine` 子包（package 名仍是 `contracts`）提供 `StrategyRef` /
  `ParseStrategyRef` / `ArtifactHash` / `HostABIContracts` / abi 哨兵，以及 QT-41b ④ 的编译·资产四哨兵
  （`ErrCompileFailed` / `ErrCompileUnavailable` / `ErrSourceTooLarge` / `ErrArtifactTooLarge`）—— 本仓**不复制**这些符号，
  四条只留同名别名（`internal/asset/errors.go`），HTTP 错误码表不变。
- **`pgx/v5` 只出现在存储实现**（`internal/asset/pgstore.go`）；领域模型与 HTTP 层不出现 `pgx` 类型。
- **不依赖 `tellurion-kit`**：HTTP / 日志用标准库（`net/http`、`log/slog`）；引入 kit 前要先在 `AGENTS.md` §4 登记。
- 禁止 `replace` 指向仓外路径、禁止提交 `go.work` —— 单仓必须能独立构建。

## 6. 关键决策

- [`adr/0001-资产服务不等于运行时.md`](adr/0001-资产服务不等于运行时.md)
- [`adr/0002-artifact-主键与生效字节的裁决.md`](adr/0002-artifact-主键与生效字节的裁决.md)（偏差 #1 七列 + artifact 主键 + 生效字节指针）
- [`adr/0003-存储缓存作业与产物可复现性.md`](adr/0003-存储缓存作业与产物可复现性.md)（PG / 两级缓存 / job 落库 / so 符号 / TinyGo `-no-debug`）

## 7. 非目标（明确不做）

- 不做策略运行时（逐 bar 出信号在 core 进程内）
- 不做回测
- 不做策略推荐 / 市场
- 不做信号存储

## 8. 分期与状态

| 阶段 | 内容 | 状态 |
|---|---|---|
| 腿 1 | DSL 资产化（零工具链）：`carrier=dsl` + `version_hash` + `strategy_ref` | ✅ `version_hash` / `strategy_ref` 解析已落地（2026-10-23） |
| 腿 2 | WASM：工具链 + PoC → go/no-go | ✅ 编译执行器 + 真 TinyGo 0.34.0 镜像已接、产物可复现（QT-41b）；跨进程装配与状态回写已收口（QT-41c） |
| X2 | 本仓完整落地（写 7 + 读 2 端点） | 🔶 10 个公开端点 + 2 个内部端点（artifact 登记 / build job 状态回写）已实装；PG / 迁移 / 缓存 / 内置登记已落；余 `storage=url`、`carrier=external` 的 hash 语义单独立文 |
