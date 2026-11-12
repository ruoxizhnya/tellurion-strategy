# AGENTS — tellurion-strategy

> 本文件是**本仓**的唯一入口。判据：一个 agent 只 checkout 本仓，就能知道「怎么构建、边界在哪、依赖哪版契约、活在哪、禁止什么」，**不需要读别的仓的源码**。

## 1. 这是什么仓

策略**资产**服务：策略定义 / 版本 / artifact / resolve（编译与执行三分里的"资产"这一分）。**策略运行时不做成服务**——逐 bar 执行留在 core 进程内。

**状态**：✅ 资产服务落地（2026-11-12 复核）——`internal/asset`（Memory + PG Store、启动迁移、两级缓存、latest TTL、job 持久化、
14 个内置策略登记、so 符号校验）+ `internal/httpapi`（13 个业务端点：**5 条公开读** + **8 条受保护写**——5 条资产写
create / source / build / validate / deprecate 已收紧，加 `/promote` + 2 条 `/internal/*`，均只认 `service`；另有 2 条探针）
+ `internal/compile`（真 TinyGo 0.34.0 锁与 `-no-debug`）
+ `internal/compilerjob` + `cmd/*` 已落地。G5（2026-10-24）补了编译 job 的**队列 / 重试 / 限并发**（`compilerjob.Queue`，
状态词表与 `strategy_build_jobs` 同一套）。QT-41c（2026-10-25）收口了编译链路跨进程闭环：
`POST .../build` → `internal/compilerclient` 派发 `cmd/compiler` → 内部端点回写 `queued → running → succeeded | failed`
（失败带 `error_code`）；`-compiler-bin` 为空时 build 端点仍 fail-closed 503 `compile_unavailable`。
余项：`storage=url` 与 `carrier=external` 的 hash 语义均为**非目标**（只留列 / fail-closed）；`version_hash` 域标签自 contracts `v0.5.0` 起已收编，本仓无本地定义。`docs/TASKS.md` 当前无开放项。
**SVC-STRAT-G1（2026-11-07）**：引 `tellurion-kit`（引入时 `v0.1.4`，现 pin `v0.5.0`，见 §4）——`/promote` + 两条 `/internal/*` 挂 service-token
闸（`internal/httpapi/policy.go`，只认 `service`）；promote 要求 `X-Actor` 并落 `strategy.strategy_promotions`
审计；新增公开 `/healthz` / `/readyz`；按身份令牌桶限流（探针豁免，鉴权先于限流）；版本列表分页；
build 幂等键（同 `(strategy, version, toolchain)` 未终态作业复用 `job_id`）。
**STRAT-WRITE-AUTH-ROLLOUT（2026-11-12）**：5 条资产写端点并入 `servedPolicy`（只认 `service`），跨仓两步走——
agent 侧带 token 能力先发布（`tellurion-agent@a59631e`），本仓随后收紧。鉴权启动闸为**三态**：有 secret ⇒ 全闸；
无 secret + 回环 + 未声明 ⇒ 受保护路由 503 `auth_unavailable`；无 secret + 显式
`AUTH_INSECURE_EXPOSURE=loopback-published` ⇒ **任何绑定都放开**写面（启动 WARN 点名写面无鉴权 + 实际绑定地址，
非回环追加「已对外暴露」）；非回环 + 未声明 ⇒ 拒绝启动。带 token 的本地跑法与三态实测矩阵见 `docs/runbook.md`
与 ADR-0005。
**SVC-STRAT-G4（2026-11-07 落地，✅ 2026-11-10）**：编译链路七类失败语义成文（`docs/api-schema.md` §2.2）、重试边界（§3.1）、
运维手册 `docs/runbook.md`；`cmd/compiler` 子进程加显式硬上界 `-compiler-process-timeout`（默认 20m、最大 1h，
走 `exec.CommandContext`），到点落 `failed + error_code=compile_timeout`，不留 `running`。
三条腿已真跑：真 PG 迁移重放（`tellurion_strategy_test` 全绿，schema 指纹 `10f6ee39…2954`）、真 TinyGo 0.34.0 镜像
（`TestCompile_RealTinyGoImage` PASS 8.61s）、真回写面停机 + 卡死 job 人工收口演练（真 PG + 真服务 `:18090`，腿 A–D 全过）。

**今天的实现**：`core:pkg/strategy`（运行时 + 插件 + `SignalProvider`）；策略**定义**的真相源已归本仓
（core `strategies` 表与资产注册腿于 2026-11-09 随 `SVC-CORE-STRAT-RETIRE` 退役）。本仓只收"资产"这一半，**运行时留在 core**。

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
| `github.com/ruoxizhnya/tellurion-contracts` | `v0.10.0`（2026-11-12 按实际 `go.mod` 订正；此前台账写 `v0.9.0`，go.mod 早已是 `v0.10.0`） | 实际使用 `engine` 子包（package 名仍是 `contracts`）：`StrategyRef` / `ParseStrategyRef` / `ArtifactHash` / `HostABIContracts` / abi 哨兵 / **编译·资产四哨兵**（`ErrCompileFailed` / `ErrCompileUnavailable` / `ErrSourceTooLarge` / `ErrArtifactTooLarge`，本仓以同名别名消费，见 `internal/asset/errors.go`）；QT-22 起消费 `StrategyVersionInput` / `DomainStrategyVersionHash`（G5 ③ 已收编：`internal/asset/hash.go` 无本地定义，编码委托 `StrategyVersionInput.Hash()`；域字符串逐字节未变）。本仓不引用 `contracts/market`，别名退役不影响本仓 |
| `github.com/jackc/pgx/v5` | `v5.11.0`（2026-11-12 随 kit `v0.5.0` 的 MVS 抬升，消除 GO-2026-5004；`go` directive 同步 `1.26.11`） | **生产 PG `Store`**（`internal/asset/pgstore.go`）与连接池；版本与 core / kit / data 仓一致。仅存储层使用，不把 `pgx` 类型泄进领域模型 |
| `github.com/ruoxizhnya/tellurion-kit` | `v0.5.0`（2026-11-12 随 `X-DEP-BUMP-CONSUMERS` 从 `v0.2.0` 升；2026-11-07 引入 `v0.1.4`，G1；2026-11-10 随 kit `KT-13` 升） | `auth` + `auth/httpadapter`：service-token 校验、角色 any-of、`RouteKey`、启动闸 `DecideStartup`。只在 `internal/httpapi` 与 `cmd/assetservice` 使用，不把 kit 类型泄进 `internal/asset` 领域模型 |
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
