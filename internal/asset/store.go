package asset

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store 是 strategy schema 的读写端口。
//
// 本仓的 DDL 唯一 owner 是 strategy（docs/db-schema.md），但**执行机制**与连接
// 不在本包：HTTP 层、PG 实现、内存实现都走同一个端口。Memory 供单仓测试，
// PG 是生产实现（启动时由 asset.PG.Migrate 执行 DDL）。
type Store interface {
	CreateDefinition(ctx context.Context, def Definition) error
	GetDefinition(ctx context.Context, id string) (Definition, error)
	SetActive(ctx context.Context, id, versionHash, artifactHash string) error

	SaveVersion(ctx context.Context, v Version) (Version, error)
	GetVersion(ctx context.Context, id, versionHash string) (Version, error)
	ListVersions(ctx context.Context, id string) ([]Version, error)

	SaveArtifact(ctx context.Context, a Artifact) (Artifact, error)
	GetArtifact(ctx context.Context, id, artifactHash string) (Artifact, error)
	// ArtifactsForVersion 只返回元数据（Bytes 为空）：字节走 GetArtifact，
	// 避免版本列表把每个 8 MiB blob 都读一遍。
	ArtifactsForVersion(ctx context.Context, id, versionHash string) ([]Artifact, error)
	UpdateStatus(ctx context.Context, id, artifactHash string, from, to Status, log string) error

	// Promote / Deprecate 是「状态 + 生效指针」的**单事务**写入：两步分开写
	// 会留下 promoted 但 latest 指向旧字节的半截状态。
	Promote(ctx context.Context, id, versionHash, artifactHash, log string) error
	Deprecate(ctx context.Context, id, versionHash, artifactHash, log string) error
}

// Memory 是 Store 的内存实现（并发安全、深拷贝出入）。
//
// 它按 strategy.* 三张表的行形状建模：定义 / 版本 / artifact。字节码直接挂在
// artifact 行上（等价于 blobs 表的一对一关系）。
type Memory struct {
	mu   sync.RWMutex
	defs map[string]*memStrategy
}

type memStrategy struct {
	def       Definition
	versions  map[string]*Version
	artifacts map[string]*Artifact // by artifact_hash
}

// NewMemory 返回空存储。
func NewMemory() *Memory {
	return &Memory{defs: make(map[string]*memStrategy)}
}

func (m *Memory) strategy(id string) (*memStrategy, error) {
	s, ok := m.defs[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrStrategyNotFound, id)
	}
	return s, nil
}

// CreateDefinition 新建定义；重复 id 报 ErrStrategyExists。
func (m *Memory) CreateDefinition(ctx context.Context, def Definition) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := def.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.defs[def.ID]; ok {
		return fmt.Errorf("%w: %q", ErrStrategyExists, def.ID)
	}
	cp := def
	cp.Capability = def.Capability.Clone()
	m.defs[def.ID] = &memStrategy{
		def:       cp,
		versions:  make(map[string]*Version),
		artifacts: make(map[string]*Artifact),
	}
	return nil
}

// GetDefinition 返回定义副本。
func (m *Memory) GetDefinition(ctx context.Context, id string) (Definition, error) {
	if err := ctx.Err(); err != nil {
		return Definition{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, err := m.strategy(id)
	if err != nil {
		return Definition{}, err
	}
	cp := s.def
	cp.Capability = s.def.Capability.Clone()
	return cp, nil
}

// SetActive 写 active_version_hash / active_artifact_hash；空串 = 清空（deprecate）。
func (m *Memory) SetActive(ctx context.Context, id, versionHash, artifactHash string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.strategy(id)
	if err != nil {
		return err
	}
	if (versionHash == "") != (artifactHash == "") {
		return fmt.Errorf("%w: active version and artifact must be set or cleared together", ErrInvalidDefinition)
	}
	s.def.ActiveVersionHash = versionHash
	s.def.ActiveArtifactHash = artifactHash
	s.def.UpdatedAt = time.Now().UTC()
	return nil
}

// SaveVersion 登记源码版本；同 hash 幂等（内容寻址）。
func (m *Memory) SaveVersion(ctx context.Context, v Version) (Version, error) {
	if err := ctx.Err(); err != nil {
		return Version{}, err
	}
	if strings.TrimSpace(v.StrategyID) == "" || !isLowerHex64(v.VersionHash) {
		return Version{}, fmt.Errorf("%w: version needs a strategy id and a 64-hex hash", ErrInvalidDefinition)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.strategy(v.StrategyID)
	if err != nil {
		return Version{}, err
	}
	if existing, ok := s.versions[v.VersionHash]; ok {
		// 同 hash 不同源码只可能是 hash 实现被改了 —— fail-closed，不覆盖。
		if existing.Source != v.Source || existing.Carrier != v.Carrier ||
			existing.ABIVersion != v.ABIVersion || existing.Scope != v.Scope || existing.Entry != v.Entry {
			return Version{}, fmt.Errorf("%w: version %s is registered with different content",
				ErrArtifactConflict, v.VersionHash)
		}
		return *existing, nil
	}
	cp := v
	s.versions[v.VersionHash] = &cp
	return cp, nil
}

// GetVersion 返回版本副本。
func (m *Memory) GetVersion(ctx context.Context, id, versionHash string) (Version, error) {
	if err := ctx.Err(); err != nil {
		return Version{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, err := m.strategy(id)
	if err != nil {
		return Version{}, err
	}
	v, ok := s.versions[versionHash]
	if !ok {
		return Version{}, fmt.Errorf("%w: %s", ErrVersionNotFound, versionHash)
	}
	return *v, nil
}

// ListVersions 返回版本列表（按创建时间、再按 hash 排序，保证确定性）。
func (m *Memory) ListVersions(ctx context.Context, id string) ([]Version, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, err := m.strategy(id)
	if err != nil {
		return nil, err
	}
	out := make([]Version, 0, len(s.versions))
	for _, v := range s.versions {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].VersionHash < out[j].VersionHash
	})
	return out, nil
}

// SaveArtifact 登记 artifact；主键 (strategy_id, artifact_hash)，同 hash 幂等。
//
// 同一 (version_hash, toolchain) 只允许一个 artifact_hash：工具链固定版本时产物
// 必须可复现，出现第二个字节说明「同版本同工具链产出不稳定」，那是 fail-closed。
func (m *Memory) SaveArtifact(ctx context.Context, a Artifact) (Artifact, error) {
	if err := ctx.Err(); err != nil {
		return Artifact{}, err
	}
	if err := a.Validate(); err != nil {
		return Artifact{}, err
	}
	if a.Status == "" {
		a.Status = StatusBuilt
	}
	if a.Storage == "" {
		a.Storage = StorageInline
	}
	if int64(len(a.Bytes)) > MaxArtifactBytes {
		return Artifact{}, fmt.Errorf("%w: %d bytes > %d", ErrArtifactTooLarge, len(a.Bytes), MaxArtifactBytes)
	}
	if a.Storage == StorageInline && a.SizeBytes == 0 {
		a.SizeBytes = int64(len(a.Bytes))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.strategy(a.StrategyID)
	if err != nil {
		return Artifact{}, err
	}
	if _, ok := s.versions[a.VersionHash]; !ok {
		return Artifact{}, fmt.Errorf("%w: %s", ErrVersionNotFound, a.VersionHash)
	}
	if existing, ok := s.artifacts[a.ArtifactHash]; ok {
		if existing.VersionHash != a.VersionHash || existing.Toolchain != a.Toolchain ||
			existing.SizeBytes != a.SizeBytes || len(existing.Bytes) != len(a.Bytes) ||
			sha256Hex(existing.Bytes) != sha256Hex(a.Bytes) {
			return Artifact{}, fmt.Errorf("%w: artifact %s is registered with different content",
				ErrArtifactConflict, a.ArtifactHash)
		}
		return existing.Clone(), nil
	}
	for _, other := range s.artifacts {
		if other.VersionHash == a.VersionHash && other.Toolchain == a.Toolchain {
			return Artifact{}, fmt.Errorf("%w: version %s already has an artifact from toolchain %q",
				ErrArtifactConflict, a.VersionHash, a.Toolchain)
		}
	}
	cp := a.Clone()
	s.artifacts[a.ArtifactHash] = &cp
	return cp, nil
}

// GetArtifact 按 artifact_hash 返回 artifact 副本。
func (m *Memory) GetArtifact(ctx context.Context, id, artifactHash string) (Artifact, error) {
	if err := ctx.Err(); err != nil {
		return Artifact{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, err := m.strategy(id)
	if err != nil {
		return Artifact{}, err
	}
	a, ok := s.artifacts[artifactHash]
	if !ok {
		return Artifact{}, fmt.Errorf("%w: %s", ErrArtifactNotFound, artifactHash)
	}
	return a.Clone(), nil
}

// ArtifactsForVersion 返回某版本的全部工具链产物（按 toolchain 排序）。
func (m *Memory) ArtifactsForVersion(ctx context.Context, id, versionHash string) ([]Artifact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, err := m.strategy(id)
	if err != nil {
		return nil, err
	}
	if _, ok := s.versions[versionHash]; !ok {
		return nil, fmt.Errorf("%w: %s", ErrVersionNotFound, versionHash)
	}
	var out []Artifact
	for _, a := range s.artifacts {
		if a.VersionHash == versionHash {
			cp := a.Clone()
			cp.Bytes = nil // 元数据接口：字节走 GetArtifact
			out = append(out, cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Toolchain < out[j].Toolchain })
	return out, nil
}

// UpdateStatus 执行状态机跃迁；日志追加在 CompileLog 末尾。
func (m *Memory) UpdateStatus(ctx context.Context, id, artifactHash string, from, to Status, log string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.strategy(id)
	if err != nil {
		return err
	}
	a, ok := s.artifacts[artifactHash]
	if !ok {
		return fmt.Errorf("%w: %s", ErrArtifactNotFound, artifactHash)
	}
	if a.Status != from {
		return fmt.Errorf("%w: artifact %s is %s, want %s", ErrInvalidTransition, artifactHash, a.Status, from)
	}
	if !allowedTransition(from, to) {
		return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, from, to)
	}
	a.Status = to
	if log != "" {
		if a.CompileLog != "" {
			a.CompileLog += "\n"
		}
		a.CompileLog += log
	}
	return nil
}

// Promote 在单锁内完成 validated → promoted 与生效指针写入。
func (m *Memory) Promote(ctx context.Context, id, versionHash, artifactHash, log string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.strategy(id)
	if err != nil {
		return err
	}
	a, ok := s.artifacts[artifactHash]
	if !ok {
		return fmt.Errorf("%w: %s", ErrArtifactNotFound, artifactHash)
	}
	if a.Status != StatusValidated {
		return fmt.Errorf("%w: artifact %s is %s, want %s", ErrInvalidTransition, artifactHash, a.Status, StatusValidated)
	}
	a.Status = StatusPromoted
	if log != "" {
		if a.CompileLog != "" {
			a.CompileLog += "\n"
		}
		a.CompileLog += log
	}
	s.def.ActiveVersionHash = versionHash
	s.def.ActiveArtifactHash = artifactHash
	s.def.UpdatedAt = time.Now().UTC()
	return nil
}

// Deprecate 在单锁内完成 promoted → deprecated 与生效指针清空。
func (m *Memory) Deprecate(ctx context.Context, id, versionHash, artifactHash, log string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.strategy(id)
	if err != nil {
		return err
	}
	a, ok := s.artifacts[artifactHash]
	if !ok {
		return fmt.Errorf("%w: %s", ErrArtifactNotFound, artifactHash)
	}
	if a.Status != StatusPromoted {
		return fmt.Errorf("%w: artifact %s is %s, want %s", ErrInvalidTransition, artifactHash, a.Status, StatusPromoted)
	}
	a.Status = StatusDeprecated
	if log != "" {
		if a.CompileLog != "" {
			a.CompileLog += "\n"
		}
		a.CompileLog += log
	}
	if s.def.ActiveArtifactHash == artifactHash {
		s.def.ActiveVersionHash = ""
		s.def.ActiveArtifactHash = ""
		s.def.UpdatedAt = time.Now().UTC()
	}
	return nil
}

// allowedTransition 是 artifact 状态机：
//
//	built → validated | rejected
//	validated → promoted | rejected
//	promoted → deprecated
//
// 其余一律拒绝（包括「已 deprecated 再 promote」，历史版本只读）。
func allowedTransition(from, to Status) bool {
	switch from {
	case StatusBuilt:
		return to == StatusValidated || to == StatusRejected
	case StatusValidated:
		return to == StatusPromoted || to == StatusRejected
	case StatusPromoted:
		return to == StatusDeprecated
	default:
		return false
	}
}
