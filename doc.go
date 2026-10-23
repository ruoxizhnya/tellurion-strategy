// Package strategy 是 tellurion-strategy 的仓级包文档。
//
// 本仓是策略**资产**服务：定义 / 版本 / artifact / resolve。**策略运行时不在本仓**——
// 逐 bar 执行留在 core 进程内的 WASM host。边界与依赖见 AGENTS.md 与 docs/ARCHITECTURE.md。
//
// 实现按能力拆在子包，不要堆到这里：
//
//	internal/asset         领域核心（模型 / VersionHash / Verify / Store / DDL / Service）
//	internal/httpapi       传输层（9 个公开端点 + 1 个内部登记端点）
//	internal/compile       Compile 门（toolchain 执行器）
//	internal/compilerjob   编译作业管线（队列 / 限并发 / 重试 → artifact_hash → 内部端点登记）
//	cmd/assetservice       资产服务进程入口
//	cmd/compiler           编译服务进程入口
//
// 根包保留空实现只为让 go build ./... 在空仓阶段也是绿的，以及为仓级文档留锚点。
package strategy
