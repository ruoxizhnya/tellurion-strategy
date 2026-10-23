---
status: active
last-verified: 2026-10-24
verified-by: QT-41b 真 TinyGo 0.34.0 镜像编译（artifact_hash 固定 + 两次编译逐字节一致 + 全链路登记/解析/下载）
---

# 证据 · QT-41b 真 TinyGo 镜像与可离线复现的 artifact_hash

> 本文件是 **QT-41b 未落项「真 TinyGo 镜像」** 的结案证据。所有值都是实测的，
> 不是从文档抄的；可复现命令在 §5。仓库里的固定产物：`internal/compile/testdata/tinygo/artifact.wasm`
> 和 `internal/compile/toolchains.lock`。

## 1. 工具链

| 项 | 值 |
|---|---|
| 镜像 | `tinygo/tinygo:0.34.0` |
| 镜像 digest | `sha256:79c73c4246a1079e648d6e8425bae4654f26d2baed24c96537a8893b41c04d26` |
| 二进制 | `/usr/local/tinygo/bin/tinygo` |
| 二进制 sha256 | `14408311f6cd58c61d406776417cb8dd07728aa4f00ededfbdd1a544405d2394` |
| 版本自报 | `tinygo version 0.34.0 linux/amd64 (using go version go1.23.2 and LLVM version 18.1.2)` |
| 锁文件 | `internal/compile/toolchains.lock`（name=`tinygo` / version=`0.34.0` / 上面的 sha256） |

`cmd/compiler` 的锁门要求 **名字+版本在清单里** 且 **二进制 sha256 与清单一致**；
锁文件在部署侧提供（本仓入库一份与官方镜像对齐的清单），调用方在命令行报版本无法绕过。

## 2. 编译配方（关键：`-no-debug`）

```
tinygo build -no-debug -target=wasm-unknown -o {out} {src}
```

`-no-debug` 不是优化选项，是**可复现性前提**：TinyGo 默认把源文件的绝对路径写进 DWARF，
而 `internal/compile.Runner` 每次在随机临时目录落 src（`os.MkdirTemp("", "tellurion-compile-*")`），
同一份源码两次编译会得到不同字节、不同 `artifact_hash`。

实测（同源码、同镜像，两个不同目录）：

| 配方 | 字节数 | sha256 |
|---|---|---|
| 不带 `-no-debug`（`a/main.go`） | 17711 | `3621e3d1fc18e4217c92a47c27828a4263b954ec25e0c82824e06fe66d84322d` |
| 不带 `-no-debug`（`b/main.go`） | 17711 | `502c88dd2b0f6feab74f790c7db99f9160519ad385296223fa68d0b0bf715629` |
| 带 `-no-debug`（`a/main.go`） | 270 | `fd0ecc264521683eaa7ba0819cb9d41a187e54fc5bcfb2332fe0e9aa1f9f7aea` |
| 带 `-no-debug`（`b/main.go`） | 270 | `fd0ecc264521683eaa7ba0819cb9d41a187e54fc5bcfb2332fe0e9aa1f9f7aea` |

因此 `compile.Config.Validate` 对 `name=tinygo` 强制要求参数里存在精确的 `-no-debug`
（`internal/compile/compile_test.go: TestConfig_TinyGoRequiresNoDebug`）。

## 3. 源码

`internal/compile/testdata/tinygo/main.go` 逐字节如下（含末尾换行）：

```go
package main

//go:wasmexport OnBar
func OnBar() int32 { return 0 }

func main() {}
```

产物 `internal/compile/testdata/tinygo/artifact.wasm`：

| 项 | 值 |
|---|---|
| 字节数 | 270 |
| `artifact_hash`（sha256，小写） | `fd0ecc264521683eaa7ba0819cb9d41a187e54fc5bcfb2332fe0e9aa1f9f7aea` |
| imports | `[]`（bare `wasm-unknown` 不引 host 函数） |
| `asset.Verify(artifact, "OnBar")` | 通过（`on_bar/2`；体积 / memory / sha256 / imports 白名单） |

## 4. 全链路实测（真镜像 + 真 `cmd/compiler` + 真资产服务）

- 资产服务：`cmd/assetservice`（内存 Store，`127.0.0.1:18085`），启动时登记 14 个内置策略。
- 编译进程：`cmd/compiler` 的 **linux/amd64 静态二进制**在 `tinygo/tinygo:0.34.0` 容器内运行，
  工具链路径 `/usr/local/tinygo/bin/tinygo`，锁文件挂载到 `/work/toolchains.lock`，
  经 `host.docker.internal:18085` 调内部登记端点（不直连 DB）。
- 编译参数：`-arg=build -arg=-no-debug -arg=-target=wasm-unknown -arg=-o -arg={out} -arg={src}`。

实测回执：

```json
{"artifact_hash":"fd0ecc264521683eaa7ba0819cb9d41a187e54fc5bcfb2332fe0e9aa1f9f7aea","status":"built","strategy_id":"st_b324a1ff08c0a5208b4c52f7902ee3a3","toolchain":"tinygo 0.34.0","version_hash":"cd488c31c04c5c09085a784ce6e77e57e03143c1a5f0d12eb18f8aa5fc815738"}
```

随后：

| 步骤 | 结果 |
|---|---|
| `validate` | `validated` |
| `promote` | `promoted`，`artifact_hash=fd0ecc…f7aea` |
| `resolve?ref=latest` | `artifact_hash=fd0ecc…f7aea` |
| `resolve?ref=<id>@<version_hash>` | `version_hash=cd488c31…5738`，**回写** `artifact_hash=fd0ecc…f7aea` |
| 下载 artifact | 270 字节，sha256 = `fd0ecc…f7aea`（与登记 hash 逐字节一致） |

**锁门实测**：把锁文件里的 sha256 改成全 0 后重跑，编译进程在启动阶段就拒绝（一个 job 都不跑）：

```
compiler: compile: toolchain binary does not match the pinned sha256:
tinygo 0.34.0 binary hashes to 14408311f6cd58c61d406776417cb8dd07728aa4f00ededfbdd1a544405d2394,
lock says 0000000000000000000000000000000000000000000000000000000000000000
```

> 注：`version_hash` 含 `strategy_id`（设计 §2 的六输入之一），因此不同的策略 id 即使源码相同也会得到不同的
> `version_hash`；`artifact_hash` 只由字节决定，与 id 无关。

## 5. 复现命令

### 5.1 离线核对产物 hash（不需要 Docker）

```bash
sha256sum internal/compile/testdata/tinygo/artifact.wasm
# fd0ecc264521683eaa7ba0819cb9d41a187e54fc5bcfb2332fe0e9aa1f9f7aea
```

或直接跑回归：

```bash
go test ./internal/compile -run 'TestPinned' -v
```

`TestPinnedTinyGoArtifactIsOfflineVerifiable` 会用 `contracts.ArtifactHash` 重算并跑 `asset.Verify`。

### 5.2 用真镜像重编（需要 Docker 与本机可拉取该镜像）

```bash
STRATEGY_TINYGO_IMAGE=tinygo/tinygo:0.34.0 \
  go test ./internal/compile -run TestCompile_RealTinyGoImage -v
```

该测试会：
1. `sha256sum /usr/local/tinygo/bin/tinygo` 并与 `toolchains.lock` 比对；
2. 用固定配方编译两次，断言两次逐字节一致且等于 `fd0ecc…f7aea`；
3. 断言 `asset.Verify` 通过、imports 为空。

### 5.3 Windows 手工重编

```powershell
docker run --rm -v ${PWD}/internal/compile/testdata/tinygo:/work -w /work `
  tinygo/tinygo:0.34.0 tinygo build -no-debug -target=wasm-unknown -o /work/artifact.wasm main.go
Get-FileHash internal/compile/testdata/tinygo/artifact.wasm -Algorithm SHA256
```

## 6. 边界

- 本证据只覆盖 **wasm 载体** 的 TinyGo 真实编译；`carrier=so` 的编译工具链未在本批接真镜像
  （SO 侧本批补的是**导出符号表校验**，用标准库 `debug/elf`）。
- 编译 job 的队列 / 重试 / 并发编排不在**本批**（当时的 `cmd/compiler` 一次只处理一个 job）；
  2026-10-24 G5 已补（`internal/compilerjob.Queue`），见 `docs/TASKS.md`。
- 改工具链版本或改编译参数（包括去掉 `-no-debug`）等于改构建配方：必须同时更新
  `toolchains.lock` 与 `toolchain` 标识，否则会与已登记的 `(version_hash, toolchain)` 行冲突（fail-closed）。