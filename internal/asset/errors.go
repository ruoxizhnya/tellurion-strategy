package asset

import (
	"errors"

	contracts "github.com/ruoxizhnya/tellurion-contracts/engine"
)

// 哨兵族。名字与语义对齐 tellurion-strategy:docs/api-schema.md §2 与
// tellurion:docs/design/qt-decoupling/strategy-asset-service-api.md §4。
//
// 与 contracts 既有哨兵语义重叠的两条**不另造**（对表偏差 #5）：
//   - abi 不兼容 → 包装 contracts.ErrAbiVersionMismatch
//   - import 越界 → 包装 contracts.ErrImportNotWhitelisted
//
// 编译 / 资产四条自 contracts v0.4.0 起定义在契约仓（QT-41b ④），本仓只留
// **同名别名**（见文件末尾），判定与 wire code 不变。
var (
	// ErrStrategyNotFound 是策略定义不存在。
	ErrStrategyNotFound = errors.New("strategy: definition not found")
	// ErrStrategyExists 是同一 id 重复创建。
	ErrStrategyExists = errors.New("strategy: definition already exists")
	// ErrInvalidDefinition 是定义字段缺失或自相矛盾。
	ErrInvalidDefinition = errors.New("strategy: invalid definition")
	// ErrUnsupportedCarrier 是 carrier 不在冻结枚举内。
	ErrUnsupportedCarrier = errors.New("strategy: unsupported carrier")
	// ErrInvalidABIVersion 是 abi_version 与 carrier 不匹配或无法解析。
	ErrInvalidABIVersion = errors.New("strategy: invalid abi_version")
	// ErrInvalidScope 是 scope 缺失 / 未知 / 与 abi 矛盾。
	ErrInvalidScope = errors.New("strategy: invalid scope")

	// ErrSourceMissing 是上传源码为空。
	ErrSourceMissing = errors.New("strategy: source is required")

	// ErrVersionNotFound 是 version_hash 不存在（不回退 latest）。
	ErrVersionNotFound = errors.New("strategy: version not found")
	// ErrNoActiveVersion 是该策略没有生效版本（fail-closed，不回退内置同名策略）。
	ErrNoActiveVersion = errors.New("strategy: no active version")
	// ErrArtifactNotFound 是 artifact_hash 不存在。
	ErrArtifactNotFound = errors.New("strategy: artifact not found")
	// ErrAmbiguousArtifact 是同一版本有多个工具链产物而未指定 toolchain。
	ErrAmbiguousArtifact = errors.New("strategy: version has multiple artifacts; toolchain is required")
	// ErrArtifactConflict 是同一 (version, toolchain) 已登记了不同字节。
	ErrArtifactConflict = errors.New("strategy: artifact conflicts with the registered row")
	// ErrInvalidArtifact 是 artifact 元数据不自洽。
	ErrInvalidArtifact = errors.New("strategy: invalid artifact")

	// ErrArtifactFetchFailed 是取字节失败（与「没有这行」区分）。
	ErrArtifactFetchFailed = errors.New("strategy: artifact fetch failed")
	// ErrArtifactHashMismatch 是字节的 sha256 与登记的 artifact_hash 不符。
	ErrArtifactHashMismatch = errors.New("strategy: artifact bytes do not match artifact_hash")
	// ErrStorageUnsupported 是 storage=url 分支（只保留列、不实现）。
	ErrStorageUnsupported = errors.New("strategy: storage=url is not implemented")

	// ErrInvalidTransition 是状态机不允许的跃迁（如未 validate 就 promote）。
	ErrInvalidTransition = errors.New("strategy: invalid status transition")

	// ErrUnsupportedToolchain 是请求的 toolchain 与服务配置不符。
	ErrUnsupportedToolchain = errors.New("strategy: unsupported toolchain")
	// ErrJobNotFound 是 build job 不存在。
	ErrJobNotFound = errors.New("strategy: build job not found")

	// ErrInvalidParameter 是 HTTP 层参数形状错误（缺字段 / 未知字段 / 非法枚举值）。
	ErrInvalidParameter = errors.New("strategy: invalid parameter")
)

// 编译 / 资产错误族（QT-41b ④）：自 contracts v0.4.0 起由契约仓定义，
// 消费方（agent 生成闭环 / core 取 artifact / 编排终态）不必 import 本仓实现包。
//
// 本仓保留同名别名，**映射逐字**（名字、文案、errors.Is 语义都不变）：
//
//	contracts.ErrCompileFailed      ⇄ asset.ErrCompileFailed      → HTTP 409 compile_failed
//	contracts.ErrCompileUnavailable ⇄ asset.ErrCompileUnavailable → HTTP 503 compile_unavailable
//	contracts.ErrSourceTooLarge     ⇄ asset.ErrSourceTooLarge     → HTTP 413 payload_too_large
//	contracts.ErrArtifactTooLarge   ⇄ asset.ErrArtifactTooLarge   → HTTP 413 payload_too_large
//
// 别名是过渡态：本仓调用点写 `asset.Err*` 保持不变，两边同名同义由一条测试钉住
// （errors_test.go 的 TestCompileErrorFamilyIsContractAlias）。
var (
	ErrCompileFailed      = contracts.ErrCompileFailed
	ErrCompileUnavailable = contracts.ErrCompileUnavailable
	ErrSourceTooLarge     = contracts.ErrSourceTooLarge
	ErrArtifactTooLarge   = contracts.ErrArtifactTooLarge
)
