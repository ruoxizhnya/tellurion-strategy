package asset

import (
	"errors"
)

// 哨兵族。名字与语义对齐 tellurion-strategy:docs/api-schema.md §2 与
// tellurion:docs/design/qt-decoupling/strategy-asset-service-api.md §4。
//
// 与 contracts 既有哨兵语义重叠的两条**不另造**（对表偏差 #5）：
//   - abi 不兼容 → 包装 contracts.ErrAbiVersionMismatch
//   - import 越界 → 包装 contracts.ErrImportNotWhitelisted
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
	// ErrSourceTooLarge 是源码超过服务上限。
	ErrSourceTooLarge = errors.New("strategy: source exceeds the size limit")

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
	// ErrArtifactTooLarge 是 artifact 超过 8 MiB（A 裁决：fail-loud，不静默截断）。
	ErrArtifactTooLarge = errors.New("strategy: artifact exceeds the 8 MiB blob limit")
	// ErrStorageUnsupported 是 storage=url 分支（只保留列、不实现）。
	ErrStorageUnsupported = errors.New("strategy: storage=url is not implemented")

	// ErrInvalidTransition 是状态机不允许的跃迁（如未 validate 就 promote）。
	ErrInvalidTransition = errors.New("strategy: invalid status transition")

	// ErrCompileUnavailable 是服务未配置编译端口。
	ErrCompileUnavailable = errors.New("strategy: compiler is not configured")
	// ErrCompileFailed 是编译失败（错误须可检索，带 toolchain 与日志）。
	ErrCompileFailed = errors.New("strategy: compile failed")
	// ErrUnsupportedToolchain 是请求的 toolchain 与服务配置不符。
	ErrUnsupportedToolchain = errors.New("strategy: unsupported toolchain")
	// ErrJobNotFound 是 build job 不存在。
	ErrJobNotFound = errors.New("strategy: build job not found")

	// ErrInvalidParameter 是 HTTP 层参数形状错误（缺字段 / 未知字段 / 非法枚举值）。
	ErrInvalidParameter = errors.New("strategy: invalid parameter")
)
