---
service-card: 1
repo: tellurion-strategy
kind: service
lifecycle: production
owner: ruoxizhnya
impact: degrading

requires:
  - tellurion-contracts
  - tellurion-kit
required-by: []
calls: []
called-by:
  - tellurion
  - tellurion-agent

truth:
  readme: README.md
  agents: AGENTS.md
  architecture: docs/ARCHITECTURE.md
  api: docs/api-schema.md
  db: docs/db-schema.md
  dataflow: docs/dataflow.md
  runbook: docs/runbook.md
  tasks: docs/TASKS.md

status: evergreen
last-verified: 2026-11-11
verified-by: 2026-11-11 实读 README / AGENTS / ARCHITECTURE / api-schema / db-schema / dataflow / runbook / go.mod / deploy/compose.yaml；端口、端点数量、表数量未写入
---

# 服务卡：tellurion-strategy

> **一句话**：策略资产服务——定义、版本、artifact 登记与 resolve；逐 bar 运行时仍在 core 进程内。
>
> `service` · `production` · `degrading` · `ruoxizhnya`

## 1. 这是什么

它只负责「资产」这一分：策略定义与版本的内容寻址、artifact 字节码的登记与校验、promote 状态迁移、以及给 core / agent 的 resolve。编译与执行三分里的「执行」不在本仓——WASM host 与逐 bar 执行留在 core。

**它拥有**：`strategy` schema 的 DDL 与唯一写权（定义 / 版本 / artifact / 构建作业 / promote 审计）；`version_hash` 的内容寻址规则（域标签由 contracts 收编，本仓无本地定义）；内置策略的登记清单。
**它不拥有**：策略运行时、回测引擎、行情与因子、订单与持仓。

## 2. 它不做什么

- 不做策略运行时：逐 bar / 逐笔执行是 core 进程内的 WASM host，永不做成服务。
- 不自己造 `version_hash` 规则：域标签与形状归 `tellurion-contracts`。
- 不执行任意代码：只编译受控 DSL / wasm，工具链走白名单与锁文件。
- 不替调用方 promote：promote 要求 `X-Actor` 并落审计，机器提交不等于放行。
- 不把 `storage=url` / `carrier=external` 当已支持：只留列并 fail-closed，属于非目标。

## 3. 与谁相连

### 调用它（入边）

| 来源 | 协议 | 用来做什么 | 本仓挂了会怎样 |
|---|---|---|---|
| `tellurion` | HTTP | `strategy_ref` 非空时 resolve 资产；读定义 / 版本 | fail-closed：core 拒绝执行该策略；内置策略仍可用 |
| `tellurion-agent` | HTTP | 生成物登记：建定义 → 传源码 → 可选 build，取回服务端签发的坐标 | agent 的生成链在登记步失败，不产出无坐标产物 |

> `/promote` 与 `/internal/*` 挂 service-token 闸；公开面不挂闸。UI 不直连本仓，操作经 core。

### 它调用（出边）

| 目标 | 协议 | 用来做什么 | 目标挂了会怎样 |
|---|---|---|---|
| PostgreSQL | PG | `strategy.*` 的 DDL、读与写 | 启动迁移失败则拒起；读端点 503，不返回空资产冒充成功 |
| 编译进程（`cmd/compiler`） | 进程内 / 子进程 | 把 queued 作业编译成 wasm 并回写 artifact | 未配置编译入口时 build 端点 fail-closed `503 compile_unavailable`；作业停在 `failed` 并带 `error_code` |

> 不写 URL、端口、超时。地址见 `deploy/compose.yaml` 与进程环境变量；接口形状见 `docs/api-schema.md`。

## 4. 运行时身份

- **常驻进程**：`cmd/assetservice`（唯一 HTTP 监听）。
- **非常驻进程**：`cmd/compiler` 按批读取作业、编译、回写终态后结束，不是 HTTP 服务。
- **存活/就绪语义**：
  - `GET /healthz`：进程存活。
  - `GET /readyz`：PG 可连且启动迁移完成；未配置编译入口**不算**未就绪，而是 build 时 503 `compile_unavailable`。
- **鉴权**：`JWT_SECRET` 与 core 共享；无 secret 时只在回环或显式声明 loopback 暴露时可起。
- **部署真相**：`deploy/compose.yaml`、`Dockerfile`、`README.md` 快速开始。
- **端口**：不在此卡；见上述部署真相。

## 5. 数据与契约归属

| 维度 | 归属 | 真相源 |
|---|---|---|
| DB schema | `strategy` 的唯一写者与 DDL | `docs/db-schema.md` |
| 跨仓 DTO / 契约 | 形状归 `tellurion-contracts`；`version_hash` 域标签已收编，本仓无本地定义 | `tellurion-contracts:docs/API.md` |
| 对外 HTTP | 定义 / 版本 / build / promote / resolve | `docs/api-schema.md` |

> 只写「拥有哪块」，不写表名清单与端点数量。

## 6. 坏了怎么办

- **影响面**：`degrading`——`strategy_ref` 非空的策略无法解析或执行；内置策略与不依赖资产的回测不受影响。资产侧 fail-closed，不会拿旧版本冒充新版本。
- **先确认（按顺序）**：
  1. `GET /healthz` 是否 200；不是则看进程日志与启动配置（尤其无 secret + 非回环拒起）。
  2. `GET /readyz` 是否 200；503 时看 PG 连通与启动迁移。
  3. build 失败看 `strategy_build_jobs` 终态与 `error_code`（七类失败语义）；编译入口是否配置；按 runbook §4~§6 走。
- **升级路径**：`owner` → 跨仓争议 → core `docs/TASKS.md`
- **runbook**：`docs/runbook.md`（起停 §1~§3、失败语义与重试 §4~§5、真 PG / 工具链复核 §6）。

## 7. 5 分钟后去哪

| 我想…… | 读这个 |
|---|---|
| 知道资产端点与七类编译失败语义 | `docs/api-schema.md` |
| 知道 `strategy.*` 表与状态词表 | `docs/db-schema.md` |
| 知道资产如何被 core / agent 消费 | `docs/dataflow.md` |
| 起停 / 编译卡住怎么收口 | `docs/runbook.md` |
| 当前在做什么 | `docs/TASKS.md` |
| 架构与决策 | `docs/ARCHITECTURE.md` / `docs/adr/` |
