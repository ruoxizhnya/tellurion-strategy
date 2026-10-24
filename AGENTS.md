# AGENTS — tellurion-strategy

> 本文件是**本仓**的唯一入口。判据：一个 agent 只 checkout 本仓，就能知道「怎么构建、边界在哪、依赖哪版契约、活在哪、禁止什么」，**不需要读别的仓的源码**。

## 1. 这是什么仓

策略**资产**服务：策略定义 / 版本 / artifact / resolve（编译与执行三分里的"资产"这一分）。**策略运行时不做成服务**——逐 bar 执行留在 core 进程内。

**状态**：🔶 G4 落地（2026-10-24）——`internal/asset`（Memory + PG Store、启动迁移、两级缓存、latest TTL、job 持久化、
14 个内置策略登记、so 符号校验）+ `internal/httpapi`（10+1 端点）+ `internal/compile`（真 TinyGo 0.34.0 锁与 `-no-debug`）
+ `internal/compilerjob` + `cmd/*` 已落地。G5（2026-10-24）补了编译 job 的**队列 / 重试 / 限并发**（`compilerjob.Queue`，
状态词表与 `strategy_build_jobs` 同一套）。余项见 `docs/TASKS.md`（`storage=url` 非目标、`version_hash` 域标签等 G1 契约发布、
跨进程 job 状态回写）。

**今天的实现**：`core:pkg/strategy`（运行时 + 插件 + `SignalProvider`）+ `strategies` 表（定义）。本仓只收"资产"这一半，**运行时留在 core**。

## 2. 构建 / 测试（必须单仓可跑）

```bash
go build ./...
go vet ./...
go test ./...
```

- 这三条是 R1/R2 的判据：**单独 checkout 就能绿**。
- 若某一步需要别的仓的源码目录才过，那是**边界画错了**——停下来修边界，**不要**加 `replace ../xxx` 绕过。
- 本机同时改多个仓时可用 `go.work` 联调，但**它不入库**（`.gitignore` 已挡）。
- 可选集成测试（无外部条件时自动 skip）：
  - PG：`STRATEGY_TEST_DSN='postgres://postgres:postgres@127.0.0.1:5432/tellurion_strategy_test?sslmode=disable' go test ./internal/asset -count=1`
  - 真工具链：`STRATEGY_TINYGO_IMAGE=tinygo/tinygo:0.34.0 go test ./internal/compile -run TestCompile_RealTinyGoImage -v`
    （证据与复现步骤见 `docs/evidence/QT-41b-tinygo-0.34.0.md`）

## 3. 边界（硬约束）

**允许**

- 策略定义、版本、artifact 登记与解析（`carrier=dsl / wasm`）
- 策略资产的内容寻址与回滚

**禁止**

- 策略运行时（逐 bar / 逐笔执行）——那是 core 进程内的 WASM host，永不做成服务
- import core 的实现包
- 把引擎状态搬进本仓

下面三条对所有仓都成立：

- **禁止** `replace` 指向仓外路径（单仓就不可构建了）。
- **禁止**提交 `go.work` / `go.work.sum`（本地产物；已在 `.gitignore`）。
- 新增第三方依赖前，先在第 4 节登记，再改 `go.mod`；没登记就加依赖 = 绕过评审。

## 4. 依赖的契约版本

| 依赖 | 版本 | 说明 |
|---|---|---|
| `github.com/ruoxizhnya/tellurion-contracts` | `v0.5.0`（2026-10-24 发版，非伪版本；G4 切 pin） | 实际使用 `engine` 子包（package 名仍是 `contracts`）：`StrategyRef` / `ParseStrategyRef` / `ArtifactHash` / `HostABIContracts` / abi 哨兵 / **编译·资产四哨兵**（`ErrCompileFailed` / `ErrCompileUnavailable` / `ErrSourceTooLarge` / `ErrArtifactTooLarge`，本仓以同名别名消费，见 `internal/asset/errors.go`）；QT-22 起消费 `StrategyVersionInput` / `DomainStrategyVersionHash`（G5 ③ 已收编：`internal/asset/hash.go` 无本地定义，编码委托 `StrategyVersionInput.Hash()`；域字符串逐字节未变） |
| `github.com/jackc/pgx/v5` | `v5.7.3` | **生产 PG `Store`**（`internal/asset/pgstore.go`）与连接池；版本与 core / kit / data 仓一致。仅存储层使用，不把 `pgx` 类型泄进领域模型 |
| `github.com/ruoxizhnya/tellurion-kit` | **未引入** | 日志 / HTTP / 存储连接。当前实现用标准库；引入前先登记到本表 |
| `github.com/stretchr/testify` | `v1.11.1` | **仅测试**断言（与 contracts 仓同版本）；随 QT-41/41b 测试引入，2026-10-23 补登记 |

> 跨仓依赖**只认版本号**（伪版本或 tag），不认源码路径。契约变更走 `tellurion-contracts` 的发布，不走"顺手改一下"。
> 本仓**禁止** `replace` 指向仓外路径、**禁止**提交 `go.work` / `go.work.sum` —— 单仓必须独立可构建（`go build ./...` 不依赖别的仓源码）。

## 5. 活在哪、文档在哪

- **本仓台账**：`docs/TASKS.md`（唯一）。**没有单号别开工。**
- 文档导航：`docs/README.md`；边界与依赖方向：`docs/ARCHITECTURE.md`；决策：`docs/adr/`。
- 跨仓背景（只读，不在本仓改）：`tellurion:docs/design/qt-decoupling/repo-topology-and-naming.md`、`tellurion:docs/TASKS.md`。

> 跨仓引用一律写 `repo:path` 纯文本，**不做可点击相对链接**——独立 checkout 时 `../../` 必断，R2 会永远红。

## 6. 文档真相源（改功能前先看这里）

本仓的「功能边界」由三份文件钉住，**它们是唯一真相源，不是参考读物**：

| 维度 | 文件 |
|---|---|
| 数据形状（我拥有哪些表、谁能写） | `docs/db-schema.md` |
| 接口形状（对外暴露什么、错误码、幂等） | `docs/api-schema.md` |
| 流向与顺序（数据怎么走、哪里 fail-closed） | `docs/dataflow.md` |

**变更触发是硬约束**（完整表见 `docs/README.md` §3）：改表 → 改 `db-schema.md`；改接口 → 改 `api-schema.md`；改链路 → 改 `dataflow.md`；改分层 → 改 `ARCHITECTURE.md` + 一条 ADR；删档/迁移/换工具 → 一条 ODR。PR 里代码动了而对应文档没动，应被打回。
