// Package asset 是策略资产服务的内核：定义 / 版本 / artifact / resolve。
//
// 边界（ADR-027 §2.6.1）：本包只回答「这份 artifact 是什么」，不算信号、不做运行时。
// 引擎只消费 Resolve 与 Artifact 两个读操作；其余写操作给作者 / AI / 编译服务。
package asset

import (
	"fmt"
	"strings"
	"time"

	contracts "github.com/ruoxizhnya/tellurion-contracts/engine"
)

// Carrier 是表达力分层里的载体位。未知值一律 fail-closed，不许默认 builtin。
type Carrier string

const (
	CarrierBuiltin  Carrier = "builtin"
	CarrierDSL      Carrier = "dsl"
	CarrierWASM     Carrier = "wasm"
	CarrierSO       Carrier = "so"
	CarrierExternal Carrier = "external"
)

// Valid 报告 carrier 是否在冻结枚举内。
func (c Carrier) Valid() bool {
	switch c {
	case CarrierBuiltin, CarrierDSL, CarrierWASM, CarrierSO, CarrierExternal:
		return true
	default:
		return false
	}
}

// NeedsArtifact 报告该载体是否必须有字节码 artifact。
//
// builtin 编译进二进制、dsl 是解释执行的文本、external 走信号表注入——三者没有
// 本仓产出的字节码；wasm / so 必须有。
func (c Carrier) NeedsArtifact() bool { return c == CarrierWASM || c == CarrierSO }

// Status 是 artifact 行的状态机取值：built → validated → promoted / rejected / deprecated。
type Status string

const (
	StatusBuilt      Status = "built"
	StatusValidated  Status = "validated"
	StatusPromoted   Status = "promoted"
	StatusRejected   Status = "rejected"
	StatusDeprecated Status = "deprecated"
)

// Valid 报告状态值是否在冻结枚举内。
func (s Status) Valid() bool {
	switch s {
	case StatusBuilt, StatusValidated, StatusPromoted, StatusRejected, StatusDeprecated:
		return true
	default:
		return false
	}
}

// Storage 是 artifact 字节码的落地方式。
//
// storage=url 只保留列分支（A 裁决：逃逸阀暂不实现），任何走该分支的请求
// 必须 fail-closed，不许回落到「读不到就当空」。
const (
	StorageInline = "inline"
	StorageURL    = "url"
)

// Capability 是能力声明：供 MarketRouter 按「需求 ∩ 能力」路由。
type Capability struct {
	Frequencies []string `json:"frequencies,omitempty"`
	Markets     []string `json:"markets,omitempty"`
	Features    []string `json:"features,omitempty"`
}

// Clone 返回深拷贝，避免调用方改写内部切片。
func (c Capability) Clone() Capability {
	return Capability{
		Frequencies: append([]string(nil), c.Frequencies...),
		Markets:     append([]string(nil), c.Markets...),
		Features:    append([]string(nil), c.Features...),
	}
}

// Definition 是 strategy.strategy_definitions 的一行。
type Definition struct {
	ID                 string
	Carrier            Carrier
	ABIVersion         string
	Scope              contracts.StrategyScope
	Capability         Capability
	Entry              string
	ActiveVersionHash  string
	ActiveArtifactHash string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// Validate 校验定义的可执行形状。
//
// 两条刻意规则（对表偏差 #4 的收口）：
//   - wasm / so 的 abi_version 必须能被 contracts 解析（on_bar/1|2）；
//   - 其余载体的 abi_version 必须为空——不存在的协议版本不许写一个看起来合理的值。
func (d Definition) Validate() error {
	if strings.TrimSpace(d.ID) == "" {
		return fmt.Errorf("%w: definition id is required", ErrInvalidDefinition)
	}
	if !d.Carrier.Valid() {
		return fmt.Errorf("%w: unknown carrier %q", ErrUnsupportedCarrier, d.Carrier)
	}
	if !d.Scope.Valid() {
		return fmt.Errorf("%w: scope %q is not market|symbol", ErrInvalidScope, d.Scope)
	}
	if strings.TrimSpace(d.Entry) == "" {
		return fmt.Errorf("%w: entry is required", ErrInvalidDefinition)
	}

	abi := strings.TrimSpace(d.ABIVersion)
	if d.Carrier.NeedsArtifact() {
		version, err := contracts.HostAbiVersionFromName(abi)
		if err != nil {
			return fmt.Errorf("%w: carrier %s: %v", ErrInvalidABIVersion, d.Carrier, err)
		}
		if _, err := contracts.ResolveScope(version, d.Scope); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidScope, err)
		}
		return nil
	}
	if abi != "" {
		return fmt.Errorf("%w: carrier %s must not declare abi_version %q", ErrInvalidABIVersion, d.Carrier, abi)
	}
	return nil
}

// Version 是一份源码版本（strategy.strategy_versions 的一行）。
//
// source 留在版本行里：build 端点按 (id, version_hash) 取回源码交给编译服务，
// 调用方不必携带源码重传；同时也让 version_hash 的「内容寻址」可被服务端自证。
type Version struct {
	StrategyID  string
	VersionHash string
	Source      string
	Carrier     Carrier
	ABIVersion  string
	Scope       contracts.StrategyScope
	Entry       string
	CreatedAt   time.Time
}

// Clone 返回深拷贝。
func (v Version) Clone() Version { return v }

// Artifact 是 strategy.strategy_artifacts 的一行（字节码本体在同仓 blobs 表）。
//
// 主键是 (strategy_id, artifact_hash)，而不是 (strategy_id, version_hash)：
// 同一份源码 + 两套工具链 → 两个 artifact（QT-41b ③-③），后者必须能分别追溯。
type Artifact struct {
	StrategyID       string
	VersionHash      string
	ArtifactHash     string
	Carrier          Carrier
	ABIVersion       string
	Toolchain        string
	SizeBytes        int64
	MemoryLimitBytes int64
	Imports          []string
	Status           Status
	Storage          string
	CompileLog       string
	BuiltBy          string
	CreatedAt        time.Time
	Bytes            []byte
}

// Clone 返回深拷贝（含 bytes 与 imports）。
func (a Artifact) Clone() Artifact {
	a.Imports = append([]string(nil), a.Imports...)
	a.Bytes = append([]byte(nil), a.Bytes...)
	return a
}

// Key 是 artifact 的内容寻址坐标。
func (a Artifact) Key() string { return a.StrategyID + "@" + a.ArtifactHash }

// Validate 校验 artifact 行的自洽性（不校验字节，字节校验在 Verify 门）。
func (a Artifact) Validate() error {
	if strings.TrimSpace(a.StrategyID) == "" {
		return fmt.Errorf("%w: artifact strategy_id is required", ErrInvalidDefinition)
	}
	if !isLowerHex64(a.VersionHash) {
		return fmt.Errorf("%w: version_hash %q is not 64 lowercase hex", ErrInvalidArtifact, a.VersionHash)
	}
	if !isLowerHex64(a.ArtifactHash) {
		return fmt.Errorf("%w: artifact_hash %q is not 64 lowercase hex", ErrInvalidArtifact, a.ArtifactHash)
	}
	if !a.Carrier.Valid() || !a.Carrier.NeedsArtifact() {
		return fmt.Errorf("%w: artifact carrier %q is not wasm|so", ErrUnsupportedCarrier, a.Carrier)
	}
	if strings.TrimSpace(a.Toolchain) == "" {
		return fmt.Errorf("%w: toolchain is required", ErrInvalidArtifact)
	}
	if a.Storage == "" {
		a.Storage = StorageInline
	}
	if a.Storage != StorageInline && a.Storage != StorageURL {
		return fmt.Errorf("%w: unknown storage %q", ErrInvalidArtifact, a.Storage)
	}
	if !a.Status.Valid() {
		return fmt.Errorf("%w: unknown status %q", ErrInvalidArtifact, a.Status)
	}
	if a.MemoryLimitBytes <= 0 || a.MemoryLimitBytes > MaxMemoryLimitBytes {
		return fmt.Errorf("%w: memory_limit_bytes %d out of (0, %d]",
			ErrInvalidArtifact, a.MemoryLimitBytes, MaxMemoryLimitBytes)
	}
	if a.SizeBytes < 0 {
		return fmt.Errorf("%w: size_bytes %d is negative", ErrInvalidArtifact, a.SizeBytes)
	}
	return nil
}

// ResolveResult 是 resolve 端点的返回值（引擎看到的元数据）。
//
// 两种 hash 同时回写（N7-5）：调用方传入的引用形态无法自证，只有服务端查表
// 才能回答「这个 hash 是 artifact 还是 version」。
type ResolveResult struct {
	StrategyID   string                  `json:"strategy_id"`
	VersionHash  string                  `json:"version_hash"`
	ArtifactHash string                  `json:"artifact_hash"`
	Carrier      Carrier                 `json:"carrier"`
	ABIVersion   string                  `json:"abi_version"`
	Scope        contracts.StrategyScope `json:"scope"`
	Capability   Capability              `json:"capability"`
	SizeBytes    int64                   `json:"size_bytes"`
	MemoryLimit  int64                   `json:"memory_limit_bytes"`
	Imports      []string                `json:"imports"`
	Toolchain    string                  `json:"toolchain"`
}

// isLowerHex64 判断是否是 64 位小写 hex（与 contracts.ParseStrategyRef 同口径）。
func isLowerHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			continue
		}
		return false
	}
	return true
}
