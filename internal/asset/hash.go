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

// version_hash 的输入形状与域标签**都归 contracts**（QT-22 裁死的六项，G5 ③ 收编）。
//
// 本仓不再定义第二份输入类型或域标签常量：
//   - 域标签 `tellurion.strategy_version_hash.v1` 与收编前逐字节相同，历史
//     version_hash 全部仍然有效（黄金向量 `TestVersionHash_MatchesContractGoldenVector`）；
//   - 改域标签或改六项 = 所有历史 version_hash 失效（它是资产四张表的键，还折进
//     `run_spec_hash`），只能升 v2 并走双标签过渡，不能就地改。
//
// capability 不在六项里（设计如此）：它只参与路由，不改变源码语义；这条取舍见
// contracts.StrategyVersionInput 的注释与 docs/TASKS.md 的 QT-41 风险条。

// VersionHash 用 contracts 的唯一编码器算源码版本 hash。
//
// 校验留在本仓（错误语义 / HTTP wire code 不变），编码与域标签一律委托
// `contracts.StrategyVersionInput.Hash()`——不允许本仓自己 `json.Marshal + sha256`，
// 也不允许照抄一份 canonical 段：两份编码迟早漂，而同源码必须只有一个 hash。
func VersionHash(in contracts.StrategyVersionInput) (string, error) {
	if strings.TrimSpace(in.StrategyID) == "" {
		return "", fmt.Errorf("%w: strategy_id is required", ErrInvalidDefinition)
	}
	if !Carrier(in.Carrier).Valid() {
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

	return in.Hash()
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
