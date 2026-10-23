package asset

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	contracts "github.com/ruoxizhnya/tellurion-contracts/engine"
)

// 资源上限。
//
// 8 MiB 是 A 裁决的 blob 上限；源码上限与 wasm 内存上限是本仓在实现期补的
// 数值边界（设计文档未给数），数值一改即视为资产面变更，必须同步 api-schema。
const (
	// MaxArtifactBytes 是单 artifact 字节码上限（A 裁决）。
	MaxArtifactBytes int64 = 8 << 20
	// MaxSourceBytes 是单次上传源码上限。
	MaxSourceBytes int64 = 1 << 20
	// MaxMemoryLimitBytes 是 wasm 实例内存上限（与 contracts 的 64 MB 口径一致）。
	MaxMemoryLimitBytes int64 = 64 << 20
)

// DomainStrategyVersionHash 是 version_hash 的域标签。
//
// ⚠️ 收口说明（2026-10-23）：contracts 的 provenance.go 只冻结了 artifact_hash /
// dataset_snapshot_hash / run_spec_hash 三个域；version_hash 的设计口径在
// strategy-asset-service-api.md §2，但域标签还没进 contracts。本仓先在此处定义
// 并登记为跨仓风险（docs/TASKS.md QT-41 风险条），收编时以 contracts 为准并加一条
// 兼容性说明：**改域标签会让所有历史 version_hash 失效**，必须升 v1 → v2。
const DomainStrategyVersionHash = "tellurion.strategy_version_hash.v1"

// VersionInput 是 version_hash 的全部决定性输入。
//
// 与设计 §2 一致：strategy_id + carrier + abi_version + scope + entry + source。
// capability 不在其中（设计如此）：它只参与路由，不改变源码语义；这条取舍登记在
// docs/TASKS.md 的风险条里，改口径 = 让所有历史 version_hash 失效。
type VersionInput struct {
	StrategyID string
	Carrier    Carrier
	ABIVersion string
	Scope      contracts.StrategyScope
	Entry      string
	Source     string
}

// VersionHash 用 contracts 的唯一编码器算源码版本 hash。
//
// 不允许本仓自己 `json.Marshal + sha256`（编码不确定性会让同源码得到两个 hash，
// 同样内容在不同进程里无法对齐）。
func VersionHash(in VersionInput) (string, error) {
	if strings.TrimSpace(in.StrategyID) == "" {
		return "", fmt.Errorf("%w: strategy_id is required", ErrInvalidDefinition)
	}
	if !in.Carrier.Valid() {
		return "", fmt.Errorf("%w: unknown carrier %q", ErrUnsupportedCarrier, in.Carrier)
	}
	if !in.Scope.Valid() {
		return "", fmt.Errorf("%w: scope %q is not market|symbol", ErrInvalidScope, in.Scope)
	}
	if strings.TrimSpace(in.Entry) == "" {
		return "", fmt.Errorf("%w: entry is required", ErrInvalidDefinition)
	}
	if strings.TrimSpace(in.Source) == "" {
		return "", ErrSourceMissing
	}
	if int64(len(in.Source)) > MaxSourceBytes {
		return "", fmt.Errorf("%w: %d bytes > %d", ErrSourceTooLarge, len(in.Source), MaxSourceBytes)
	}

	c := contracts.NewCanonical(DomainStrategyVersionHash)
	c.Str("strategy_id", in.StrategyID)
	c.Str("carrier", string(in.Carrier))
	c.Str("abi_version", in.ABIVersion)
	c.Str("scope", string(in.Scope))
	c.Str("entry", in.Entry)
	c.Str("source", in.Source)
	return c.Hex()
}

// ArtifactHash 是字节码的 sha256。
//
// 委托 contracts.ArtifactHash：这一份**不加域标签**，目的是让编译服务、缓存层、
// 外部工具各自独立算出来逐字节相同。本仓不再写第二份实现。
func ArtifactHash(bytes []byte) string { return contracts.ArtifactHash(bytes) }

// sha256Hex 是本地校验用的摘要（与 ArtifactHash 同算法，便于不依赖 contracts 的测试）。
func sha256Hex(bytes []byte) string {
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:])
}
