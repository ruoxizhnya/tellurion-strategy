# tellurion-strategy

> module: `tellurion-strategy` ｜ 状态：**🔶 G4 落地（2026-10-24）**——10+1 端点跑在 PG Store（启动迁移）上，两级缓存 / latest TTL /
> build job 持久化 / 14 个内置策略登记 / 真 TinyGo 0.34.0 工具链已落；余 `storage=url`（非目标）与 `version_hash` 契约收编

策略**资产**服务：策略定义 / 版本 / artifact / resolve（编译与执行三分里的"资产"这一分）。**策略运行时不做成服务**——逐 bar 执行留在 core 进程内。

## 目录

| 路径 | 内容 |
|---|---|
| `cmd/assetservice/` | 资产服务进程入口（`127.0.0.1:8085`；DSN 非空走 PG 并在启动时迁移，否则 Memory） |
| `cmd/compiler/` | 编译服务进程入口（读一批 job JSON → 队列限并发 / 重试 → 编译 → 内部端点登记 artifact；逐作业终态可检索） |
| `internal/asset/` | 领域核心：模型 / `VersionHash` / `Verify` / `WasmImports` / Store 端口 + `Memory`/`PG` / DDL / `Service` / 两级缓存 / 内置登记 / so 符号校验 |
| `internal/httpapi/` | 传输层：标准库 `net/http` 的 10 个公开端点 + 1 个内部登记端点 |
| `internal/compile/` | Compile 门：toolchain 执行器（`-target=wasm-unknown`、tinygo `-no-debug`、env 白名单、`toolchains.lock`、超时、日志截断） |
| `internal/compilerjob/` | 编译作业管线：Queue（排队 / 限并发 / 重试，状态与 `strategy_build_jobs` 同词表）→ `artifact_hash` → 内部端点回填 |
| `docs/` | 本仓文档：台账 / 边界 / 决策 / 归档 —— 从这里进：`docs/README.md` |
| `AGENTS.md` | 本仓入口（构建、边界、禁止、依赖版本） |
| `doc.go` | 仓级包文档（无代码） |

## 快速开始

```bash
go build ./...
go vet ./...
go test ./...
```

本仓**单独 checkout 就能跑通**上面三条。需要别的仓源码才过 → 边界错了，不是加 `replace` 能解决的问题。

跑起来：

```bash
STRATEGY_DATABASE_URL='postgres://postgres:postgres@127.0.0.1:5432/tellurion?sslmode=disable' \
  go run ./cmd/assetservice -addr 127.0.0.1:8085
```

不设 DSN 时退回内存 Store（日志会明确标注非持久化）。PG 集成测试用 `STRATEGY_TEST_DSN` 显式开启；
真工具链回归用 `STRATEGY_TINYGO_IMAGE=tinygo/tinygo:0.34.0`（见 `docs/evidence/QT-41b-tinygo-0.34.0.md`）。

## 边界

**可以**：策略定义、版本、artifact 登记与解析（`carrier=dsl / wasm`）；策略资产的内容寻址与回滚

**不可以**：策略运行时（逐 bar / 逐笔执行）——那是 core 进程内的 WASM host，永不做成服务；import core 的实现包；把引擎状态搬进本仓

## 文档

- 文档导航：`docs/README.md`
- 本仓台账：`docs/TASKS.md`（唯一；跨仓任务只留指针）
- 边界与依赖方向：`docs/ARCHITECTURE.md`
- 决策记录：`docs/adr/`

## 许可

MIT（见 `LICENSE`）。
