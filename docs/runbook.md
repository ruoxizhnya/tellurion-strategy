# 运行手册（SVC-STRAT-G4 ④）

> 适用：`tellurion-strategy` 的资产服务（`cmd/assetservice`）与编译进程（`cmd/compiler`）。
> 失败语义与重试边界见 `docs/api-schema.md` §2.2 / §3.1；本文只讲**怎么起、怎么看、怎么收口**。
>
> 记录口径：每条命令后标了执行状态。`[已执行 2026-11-07]` 是本机（Windows 10 / PowerShell 5，
> 无 PG、Docker 引擎不可用、无 TinyGo 镜像）真跑过的；`[未执行]` 是需要真库 / 容器 / 真工具链的腿，
> 不在本机假装通过。
>
> **环境更正（2026-11-10）**：本机现在有真 PostgreSQL 16.15（`127.0.0.1:5432`），Docker 引擎仍不可用。
> 上文「无 PG」是当时快照；真库腿的当前证据见 `docs/TASKS.md` SVC-STRAT-G4 与本文 §6.1。

## 0. 环境约定

- 仓库根：`c:\Users\ruoxi\Workspace\tellurion-strategy`（下称 `<repo>`）。
- 构建产物放仓外临时目录，避免污染工作区：`$dir = Join-Path $env:TEMP 'strategy-runbook'`。
- Windows PowerShell 5 不支持 `&&`；用 `;` 串联。每条命令前重置 PATH 与 `CGO_ENABLED`：

```powershell
$env:Path = [System.Environment]::GetEnvironmentVariable('Path','Machine') + ';' + [System.Environment]::GetEnvironmentVariable('Path','User'); $env:CGO_ENABLED='0'
```

- 下文 `curl` 指 Windows 自带的 `curl.exe`；**JSON 请求体写文件再用 `--data-binary "@file"`**，
  不要把带引号的 JSON 直接塞进 PowerShell 参数（引号会被吞，实测会得到
  `invalid character 'c' looking for beginning of object key string`）。

## 1. 起停

### 1.1 构建

```powershell
go build -o "$dir\assetservice.exe" ./cmd/assetservice
go build -o "$dir\compiler.exe" ./cmd/compiler
```

`[已执行 2026-11-07]` 两条均退出码 0。

### 1.2 起资产服务（内存 Store，本地排障用）

```powershell
$p = Start-Process -FilePath "$dir\assetservice.exe" -ArgumentList @(
  '-addr','127.0.0.1:18090',
  '-cache-dir',(Join-Path $dir 'cache'),
  '-compiler-bin',"$dir\compiler.exe",
  '-compiler-asset-url','http://127.0.0.1:18090',
  '-compiler-toolchain-name','tinygo',
  '-compiler-toolchain-version','0.34.0',
  '-compiler-toolchain-path','C:\path\to\tinygo.exe',
  '-compiler-toolchain-lock','C:\path\to\toolchains.lock',
  '-compiler-process-timeout','20m'
) -PassThru -RedirectStandardOutput "$dir\out.log" -RedirectStandardError "$dir\err.log"
```

- `-compiler-bin` 为空 = 编译能力关闭，build 端点恒 503 `compile_unavailable`（fail-closed）。
- `-compiler-process-timeout` 是 **cmd/compiler 子进程硬上界**，默认 `20m`、最大 `1h`；
  实际生效值 = min(批次预算, 该值)。批次预算 = `maxAttempts × (timeout + backoff) + 1min`。
- 鉴权启动闸是**三态**（kit `DecideStartup`，与 risk / analytics 等同口径）：
  1. 有 `JWT_SECRET` ⇒ 全闸；
  2. 无 secret + 回环 + 未声明 ⇒ 进程可起，但**受保护路由一律 503 `auth_unavailable`**
     （回环豁免只免「启动拒绝」，不免写端点鉴权）；
  3. 无 secret + **显式** `AUTH_INSECURE_EXPOSURE=loopback-published` ⇒ **任何绑定都放开**，
     受保护路由走业务；启动 WARN 点名「受保护/写面无鉴权」与**实际绑定地址**，非回环再追加
     「已对外暴露」。
  无 secret + 非回环 + 未声明（含拼错）⇒ **拒绝启动**。生产必须给 `JWT_SECRET` /
  `AUTH_JWT_SECRET`；只有在明确接受写面无鉴权时才声明第 3 态。
- **写端点从 2026-11-12 起要 service token**：5 条资产写（create / source / build / validate / deprecate）
  与 `/promote`、2 条 `/internal/*` 全部只认 `role=service`（`internal/httpapi/policy.go`）。
  没配 secret 且未声明第 3 态时受保护路由 503 `auth_unavailable`；配了 secret 但请求不带 token 时
  401 `unauthenticated`。第 3 态（显式 `loopback-published`）是唯一免 token 的可写档。
  本地直跑要给写链路放行，二选一：注入 `JWT_SECRET` + service token（推荐），或显式声明第 3 态。
  实测状态码见 ADR-0005「实测矩阵」。带 token 的三步配法（secret 只走 env，不进 argv）：

  ```powershell
  # 1) core 仓签发：stdout 只有 token；sub 必须是 service:<repo>
  $env:JWT_SECRET = '<shared secret>'
  $env:STRATEGY_ASSET_TOKEN = go run tools/issue_service_token.go -sub service:agent -ttl 720h

  # 2) 起 assetservice 时给同一个 secret（-jwt-secret 或 JWT_SECRET）
  # 3) 调用侧带 Authorization: Bearer <token>；agent copilot 登记链直接从
  #    STRATEGY_ASSET_TOKEN 读同一个值（config/ai-service.yaml 的 strategy_asset.token）
  ```

  读端点（versions / promotions / builds / resolve / artifact）不需要 token，保持不变。
- 生产把 `-dsn` 换成 `STRATEGY_DATABASE_URL`（或 `-dsn`）；启动时先执行幂等 DDL 再监听。

健康检查：

```powershell
curl.exe -s -o NUL -w "%{http_code}`n" http://127.0.0.1:18090/healthz   # 期望 200
```

`[已执行 2026-11-07]` 返回 200（`HEALTH_READY=True`）。

### 1.3 停

```powershell
Stop-Process -Id $p.Id
```

`[已执行 2026-11-07]` 进程退出；被它拉起的 `cmd/compiler` 子进程由 `exec.CommandContext`
在上界到点时杀掉，不会遗留孤儿。

### 1.4 独立跑一批编译作业（不经资产服务）

```powershell
"$dir\compiler.exe" -job "$dir\job.json" `
  -asset-url http://127.0.0.1:18090 `
  -toolchain-name tinygo -toolchain-version 0.34.0 `
  -toolchain-path C:\path\to\tinygo.exe -toolchain-lock C:\path\to\toolchains.lock
```

`[未执行]` 需要可执行的工具链；本机无 TinyGo，跑起来只会得到
`read toolchain lock: ... The system cannot find the file specified`（见 §5 恢复腿）。

## 2. 看编译 job 状态

### 2.1 HTTP（对外面）

```powershell
curl.exe -s "http://127.0.0.1:18090/api/strategies/<strategy_id>/builds/<job_id>"
```

返回 `{"job_id","strategy_id","version_hash","toolchain","state","error_code","error","created_at","finished_at"}`。
`state` ∈ `queued | running | succeeded | failed`；终态看 `error_code`（`compile_failed` /
`compile_unavailable` / `payload_too_large` / `artifact_invalid` / `compile_timeout`）。

`[已执行 2026-11-07]` 恢复腿实际返回：

```json
{"job_id":"st_7fb71094eedfaafefb5ff8ff3121b72d","strategy_id":"st_88348bc7a2a919e263d32ea0976902ab","version_hash":"adc9fe530f3887685824084f8a97b706dba2098c288128d095acb01232cce152","toolchain":"tinygo 0.34.0","state":"failed","error_code":"compile_failed","error":"compiler process: exit status 1: compiler: compile: read toolchain lock: open C:\\Users\\ruoxi\\AppData\\Local\\Temp\\g4drill\\toolchains.lock: The system cannot find the file specified.","created_at":"2026-11-07T04:34:44.038893Z","finished_at":"2026-11-07T04:34:44.1121305Z"}
```

### 2.2 PG（生产面）

```sql
-- 最近作业
SELECT id, strategy_id, version_hash, toolchain, state, error_code,
       left(error, 200) AS error, created_at, finished_at
  FROM strategy.strategy_build_jobs
 ORDER BY created_at DESC
 LIMIT 20;

-- 卡死候选：非终态且超过「进程上界 + 2min」没有推进
SELECT id, strategy_id, state, created_at, now() - created_at AS age
  FROM strategy.strategy_build_jobs
 WHERE state IN ('queued','running')
   AND created_at < now() - interval '22 minutes'
 ORDER BY created_at;
```

`[未执行]` 本机无 PostgreSQL；SQL 未在真库上跑过（语法按 `internal/asset/ddl.go` 的列集写）。

## 3. 重放迁移

迁移是 `internal/asset/ddl.go` 里的幂等 DDL，**没有独立 migrate 子命令**；重放 = 再跑一次
`Migrate`。两种方式：

1. **重启服务**（推荐，生产同路径）：`cmd/assetservice` 在连库后、监听前调用 `pg.Migrate(ctx)`，
   失败即退出（fail-loud，不带着半套 schema 起服务）。
2. **单测重放自证**（会连真库，连跑两次并比对 schema 指纹）：

```powershell
$env:STRATEGY_TEST_DSN = 'postgres://user:pass@127.0.0.1:5432/strategy?sslmode=disable'
go test ./internal/asset -run TestPGMigrationsAreReplayable -count=1 -v
```

期望：`schema fingerprint stable across replay: <sha256>`。

`[已执行 2026-11-10]` 本机 PostgreSQL 16.15（Windows 服务，`127.0.0.1:5432`，库 `tellurion_strategy_test`）：
本命令退出码 0，输出 `schema fingerprint stable across replay: 10f6ee398ac1553f0912fabb1dd671c5a44dd8bf69494ce432e383d3ea329354`。
同套 PG 腿（`TestPG_ReadyRequiresMigratedSchema` / `TestPG_MigrateIsIdempotent` / `TestPG_ServiceOnPG` 等）全部 PASS。
**skip 不等于通过**：未设 `STRATEGY_TEST_DSN` 时仍会 skip，上线前必须在目标库跑一次；真 TinyGo 镜像腿见 §4（2026-11-10 已执行）。

核对表是否齐（六张表）：

```sql
SELECT table_name FROM information_schema.tables
 WHERE table_schema = 'strategy' ORDER BY table_name;
```

`[未执行]` 同上。

## 4. 真工具链验证（TinyGo 0.34.0）

前置：本机有 Docker 引擎，且能拉/用 `tinygo/tinygo:0.34.0`。

```powershell
$env:STRATEGY_TINYGO_IMAGE = 'tinygo/tinygo:0.34.0'
go test ./internal/compile -run TestCompile_RealTinyGoImage -count=1 -v
```

该用例会：

1. `docker run --rm <image> sha256sum /usr/local/tinygo/bin/tinygo`，与 `toolchains.lock` 的
   `tinyGoBinarySHA256` 逐位比对；
2. `--network none` 编译两份源码，断言产物可 `Verify` 且同源码同产物；
3. 任何一步不符直接 `FAIL`，不降级成 skip。

`[已执行 2026-11-10]` 本机 Docker 引擎 `29.8.0`、镜像 `tinygo/tinygo:0.34.0`（`docker pull` 返回 `Image is up to date`）：
`TestPinnedToolchainLockHasRealTinyGo` **PASS**、`TestCompile_RealTinyGoImage` **PASS（8.61s）**，
`ok tellurion-strategy/internal/compile 9.296s`，退出码 0。若 `STRATEGY_TINYGO_IMAGE` 已设但
Docker 不可用，用例会 `SKIP: docker is not available`——这同样**不算验证通过**。

离线可跑的部分（不需要 Docker）：

```powershell
go test ./internal/compile -count=1
```

`[已执行 2026-11-07]` `ok tellurion-strategy/internal/compile`（含 pinned artifact / 假工具链用例）。

## 5. 故障演练：`-compiler-bin` 指向不存在的文件

目标：确认 build **不会静默成功**，503 之后作业被收口成 `failed`；换上可启动的编译器后同一份
源码能重新受理（202）。

### 5.1 断腿（真进程，`[已执行 2026-11-07]`）

```powershell
# 1) 起服务，-compiler-bin 指向不存在的文件
$p = Start-Process -FilePath "$dir\assetservice.exe" -ArgumentList @(
  '-addr','127.0.0.1:18090','-cache-dir',(Join-Path $dir 'cache'),
  '-compiler-bin',"$dir\no-such-compiler.exe",
  '-compiler-asset-url','http://127.0.0.1:18090',
  '-compiler-toolchain-name','tinygo','-compiler-toolchain-version','0.34.0',
  '-compiler-toolchain-path',"$dir\no-such-tinygo.exe",
  '-compiler-toolchain-lock',"$dir\toolchains.lock"
) -PassThru
# 2) 建定义 + 传源码（JSON 走文件；写端点要 service token，见 §1.2）
$h = @('-H','Content-Type: application/json','-H',"Authorization: Bearer $env:STRATEGY_ASSET_TOKEN")
curl.exe -s -X POST http://127.0.0.1:18090/api/strategies @h --data-binary "@$dir\create.json"
curl.exe -s -X PUT  http://127.0.0.1:18090/api/strategies/$id/source @h --data-binary "@$dir\source.json"
# 3) build
curl.exe -s -o "$dir\buildresp.json" -w "%{http_code}" -X POST `
  http://127.0.0.1:18090/api/strategies/$id/build @h --data-binary "@$dir\buildreq.json"
```

实测输出：

```text
BUILD_HTTP=503
{"error":"服务器内部错误","code":"compile_unavailable"}
```

- 503 而不是 202/200：**没有静默成功**。
- 2026-11-07 实测时写端点尚未挂闸；2026-11-12 收紧后同一组请求需按 §1.2 带 service token
  （不带会先得到 401，判据「503 `compile_unavailable`」需在带 token 的前提下观察）。
- 同一进程内由 `asset.Service` 把刚落的 `queued` 行收口为 `failed +
  compile_unavailable`；该断言由单测 `TestG4Drill_MissingCompilerBinFailsClosedThenRecovers`
  读取落库行验证（`state=failed` / `error_code=compile_unavailable`，见 §5.3）。

### 5.2 恢复腿（真进程，`[已执行 2026-11-07]`）

把 `-compiler-bin` 换成刚构建的 `$dir\compiler.exe`（能启动，但工具链锁文件仍不存在）：

```text
BUILD_HTTP=202
{"job_id":"st_7fb71094eedfaafefb5ff8ff3121b72d"}
```

`GET .../builds/{job_id}` 随后是 `failed / compile_failed`，`error` 指明 `read toolchain lock`
——这是**工具链腿没配好**的预期失败，不是「复通」失败。要看到 `succeeded` 必须按 §4 配真
TinyGo（本机未执行）。

### 5.3 单测版演练（httptest，`[已执行 2026-11-07]`）

```powershell
go test ./cmd/assetservice ./internal/compilerclient -count=1 -v `
  -run 'TestG4Drill|TestDispatchBuild_ProcessDeadline|TestExecRunner'
```

覆盖：

- `TestG4Drill_MissingCompilerBinFailsClosedThenRecovers`：503 `compile_unavailable` → 落库行
  `failed/compile_unavailable` → 同一 store 换可用编译器 → 202 / `queued`。
- `TestDispatchBuild_ProcessDeadlineFailsJobWithCompileTimeout`：注入永不退出的假编译器，
  150ms 上界到点回写 `failed + compile_timeout`。
- `TestExecRunner_KillsProcessAtDeadline`：真起一个不退出的子进程，`exec.CommandContext`
  在 300ms 到点杀掉，`Wait` 返回错误。

实测：

```text
--- PASS: TestDispatchBuild_ProcessDeadlineFailsJobWithCompileTimeout (0.15s)
--- PASS: TestExecRunner_KillsProcessAtDeadline (0.30s)
--- PASS: TestG4Drill_MissingCompilerBinFailsClosedThenRecovers (0.01s)
```

## 6. 卡死 job 人工收口

触发条件（满足任一）：

- `GET` 长时间停在 `queued` / `running`，年龄 > 进程上界 + 2min；
- `error` 侧日志出现 `backfill` / `read build job state` / `report` 失败；
- 运维确认编译进程已不存在（`Get-Process compiler` 无结果）但 job 未终态。

收口 = 用内部端点把 job 打到 `failed`（**重复上报同一终态幂等 200**；把终态改回 `running` 等非法转移才
`409 invalid_transition` —— 这条口径是 2026-11-10 真 PG 演练实测订正的，旧文写「已是终态会 409」不准确）：

```powershell
$report = '{"state":"failed","error_code":"compile_failed","error":"manual closure: process exceeded -compiler-process-timeout, no terminal report"}'
Set-Content -Path "$dir\close.json" -Value $report -Encoding ascii -NoNewline
curl.exe -s -X POST "http://127.0.0.1:18090/internal/strategies/<strategy_id>/builds/<job_id>" `
  -H "Content-Type: application/json" -H "Authorization: Bearer <service-token>" `
  --data-binary "@$dir\close.json"
```

- 无 `JWT_SECRET` 的回环部署（第 1 态）**不能**省 `Authorization`：内部面自 2026-11-12 起只认 service token
  （无 secret 时 503 `auth_unavailable`，见 §1.2）。只有显式声明第 3 态
  （`AUTH_INSECURE_EXPOSURE=loopback-published`）时才免 token；生产必须带 service token。
- 归因码建议：进程挂死用 `compile_failed`（若确认是超时上界触发但回写丢失，可人工记
  `compile_timeout`；两者都是终态失败，区别只在报表口径）。
- 404 `job_not_found`：job_id 或 strategy_id 不对（服务不跨策略枚举）。
- 收口后重新 `POST .../build`：终态不复用，会排新 job；同键在排队中才复用。

## 6.1 真 PG + 真停服演练（`[已执行 2026-11-10]`）

环境：真 PostgreSQL 16.15（一次性库 `tellurion_strategy_drill`）+ `cmd/assetservice` 真二进制 `:18090` +
替身编译进程（只回写 `running`，随后正常退出、永不回写终态；用来稳定复现「进程已消失但 job 未终态」）。
四条腿与真实输出：

1. **非零退出自动收口**：替身因不认识的位置参数 `exit status 2` ⇒ 服务补报 `failed / compile_failed`，
   `GET .../builds/{job_id}` 立即 200 `failed`。
2. **正常退出但无终态回写**：`POST .../build` **202**；轮询 `GET .../builds/{job_id}` → **200 `running`**；
   停服务（回写面停机）并等替身退出（`Test-NetConnection 18090` = False、`Get-Process stubcompiler` 空）。
3. **重启后仍 running**：同一 DSN 重启服务，`GET .../builds/{job_id}` 仍 **200 `running`**（说明卡在库里）。
4. **人工收口**：内部端点 `failed / compile_failed` → **200**；重复 → **200 幂等**（同 `finished_at`）；
   再报 `running` → **409 `invalid_transition`**；psql 直读
   `strategy.strategy_build_jobs` → `failed | compile_failed | 2026-11-09 22:04:47`。

清场：停服务、`DROP DATABASE … WITH (FORCE)`、临时目录全删；本仓 0 代码改动。
**本腿不含真 TinyGo 编译**（Docker 引擎不可用）；`succeeded` 终态仍以 §4 的真工具链取证为准。

`[已执行 2026-11-10]` 上一版「未执行」的那条已由本节替代。
