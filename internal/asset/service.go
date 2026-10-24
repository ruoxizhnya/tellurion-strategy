package asset

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	contracts "github.com/ruoxizhnya/tellurion-contracts/engine"
)

// CompileRequest 是交给编译端口的输入（源码 + 载体元数据）。
type CompileRequest struct {
	StrategyID       string
	VersionHash      string
	Carrier          Carrier
	ABIVersion       string
	Entry            string
	Source           string
	MemoryLimitBytes int64
}

// CompileResult 是编译端口的产出；Toolchain 由端口自报（CompileRequest 不带，
// 避免调用方拿一个不存在的工具链名字去骗服务）。
type CompileResult struct {
	Bytes   []byte
	Imports []string
	Log     string
}

// Compiler 是 QT-41b 的编译端口。
//
// 生产实现是 cmd/compiler（独立进程 / 容器，见 D 裁决）；测试注入 fake。
// ToolchainID 必须是稳定标识（如 `tinygo 0.34.0`），artifact 行按它区分产物。
type Compiler interface {
	ToolchainID() string
	Compile(ctx context.Context, req CompileRequest) (CompileResult, error)
}

// BuildDispatcher 是编译端口的**可选**能力：实现了它的 Compiler 不再在资产
// 服务进程内执行 Compile，而是把作业派发给独立的编译进程，由对方经内部端点
// 回写 running / 终态。不实现它的 Compiler 仍走原来的本地 runBuild 路径。
type BuildDispatcher interface {
	DispatchBuild(ctx context.Context, req DispatchRequest) error
}

// DispatchRequest 是一次跨进程派发：JobID 是已落库的 queued 作业 id，
// CompileRequest 是编译所需的全部输入。
type DispatchRequest struct {
	JobID string
	CompileRequest
}

// Options 是服务的装配面。
type Options struct {
	Store            Store
	Compiler         Compiler // nil = build 端点 fail-closed（ErrCompileUnavailable）
	Cache            *Cache   // nil = 直接回源（无 L1/L2、无 latest TTL）
	IDGen            func() string
	Now              func() time.Time
	MemoryLimitBytes int64
	CompileTimeout   time.Duration
}

// Service 实现 9 个端点背后的全部服务语义。
type Service struct {
	store            Store
	compiler         Compiler
	cache            *Cache
	idGen            func() string
	now              func() time.Time
	memoryLimitBytes int64
	compileTimeout   time.Duration
}

// New 装配服务；Store 必填。
func New(opts Options) (*Service, error) {
	if opts.Store == nil {
		return nil, fmt.Errorf("%w: store is required", ErrInvalidParameter)
	}
	s := &Service{
		store:            opts.Store,
		compiler:         opts.Compiler,
		cache:            opts.Cache,
		idGen:            opts.IDGen,
		now:              opts.Now,
		memoryLimitBytes: opts.MemoryLimitBytes,
		compileTimeout:   opts.CompileTimeout,
	}
	if s.idGen == nil {
		s.idGen = randomID
	}
	if s.now == nil {
		s.now = func() time.Time { return time.Now().UTC() }
	}
	if s.memoryLimitBytes == 0 {
		s.memoryLimitBytes = MaxMemoryLimitBytes
	}
	if s.memoryLimitBytes < 0 || s.memoryLimitBytes > MaxMemoryLimitBytes {
		return nil, fmt.Errorf("%w: memory limit %d out of (0, %d]",
			ErrInvalidParameter, s.memoryLimitBytes, MaxMemoryLimitBytes)
	}
	if s.compileTimeout == 0 {
		s.compileTimeout = 5 * time.Minute
	}
	return s, nil
}

// CreateDefinitionRequest 是 POST /api/strategies 的输入。
type CreateDefinitionRequest struct {
	Carrier    Carrier
	ABIVersion string
	Scope      contracts.StrategyScope
	Capability Capability
	Entry      string
}

// CreateDefinition 建定义并返回 id（服务端生成）。
func (s *Service) CreateDefinition(ctx context.Context, req CreateDefinitionRequest) (Definition, error) {
	now := s.now()
	def := Definition{
		ID:         s.idGen(),
		Carrier:    req.Carrier,
		ABIVersion: strings.TrimSpace(req.ABIVersion),
		Scope:      req.Scope,
		Capability: req.Capability.Clone(),
		Entry:      strings.TrimSpace(req.Entry),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if strings.TrimSpace(def.ID) == "" {
		return Definition{}, fmt.Errorf("%w: id generator returned an empty id", ErrInvalidDefinition)
	}
	if err := def.Validate(); err != nil {
		return Definition{}, err
	}
	if err := s.store.CreateDefinition(ctx, def); err != nil {
		return Definition{}, err
	}
	return def, nil
}

// PutSource 登记源码版本；同源码重复上传是幂等 200。
func (s *Service) PutSource(ctx context.Context, id, source string) (Version, error) {
	def, err := s.store.GetDefinition(ctx, id)
	if err != nil {
		return Version{}, err
	}
	if err := def.Validate(); err != nil {
		return Version{}, err
	}
	hash, err := VersionHash(contracts.StrategyVersionInput{
		StrategyID: def.ID,
		Carrier:    string(def.Carrier),
		ABIVersion: def.ABIVersion,
		Scope:      def.Scope,
		Entry:      def.Entry,
		Source:     source,
	})
	if err != nil {
		return Version{}, err
	}
	return s.store.SaveVersion(ctx, Version{
		StrategyID:  def.ID,
		VersionHash: hash,
		Source:      source,
		Carrier:     def.Carrier,
		ABIVersion:  def.ABIVersion,
		Scope:       def.Scope,
		Entry:       def.Entry,
		CreatedAt:   s.now(),
	})
}

// BuildJob 是可检索的编译作业状态（落 strategy.strategy_build_jobs，跨重启可查）。
type BuildJob struct {
	ID           string     `json:"job_id"`
	StrategyID   string     `json:"strategy_id"`
	VersionHash  string     `json:"version_hash"`
	Toolchain    string     `json:"toolchain"`
	State        string     `json:"state"`
	ArtifactHash string     `json:"artifact_hash,omitempty"`
	ErrorCode    string     `json:"error_code,omitempty"`
	Error        string     `json:"error,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
}

// 作业状态取值。
const (
	JobQueued    = "queued"
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
)

// 作业失败归因码（wire `error_code`）：机器可读，调用方按它分支，不必解析 error 文案。
const (
	ErrorCodeCompileFailed      = "compile_failed"
	ErrorCodeCompileUnavailable = "compile_unavailable"
	ErrorCodePayloadTooLarge    = "payload_too_large"
	ErrorCodeArtifactInvalid    = "artifact_invalid"
)

// BuildJobReport 是编译进程经内部端点回写的一次状态变更。
type BuildJobReport struct {
	State        string `json:"state"`
	ArtifactHash string `json:"artifact_hash"`
	ErrorCode    string `json:"error_code"`
	Error        string `json:"error"`
}

// SubmitBuild 触发异步编译并返回 job_id（202）。
func (s *Service) SubmitBuild(ctx context.Context, id, versionHash, toolchain string) (BuildJob, error) {
	def, err := s.store.GetDefinition(ctx, id)
	if err != nil {
		return BuildJob{}, err
	}
	if !def.Carrier.NeedsArtifact() {
		return BuildJob{}, fmt.Errorf("%w: carrier %s produces no artifact", ErrUnsupportedCarrier, def.Carrier)
	}
	version, err := s.store.GetVersion(ctx, id, versionHash)
	if err != nil {
		return BuildJob{}, err
	}
	if s.compiler == nil {
		return BuildJob{}, ErrCompileUnavailable
	}
	if toolchain != "" && toolchain != s.compiler.ToolchainID() {
		return BuildJob{}, fmt.Errorf("%w: %q (configured %q)",
			ErrUnsupportedToolchain, toolchain, s.compiler.ToolchainID())
	}

	job := BuildJob{
		ID:          s.idGen(),
		StrategyID:  id,
		VersionHash: versionHash,
		Toolchain:   s.compiler.ToolchainID(),
		State:       JobQueued,
		CreatedAt:   s.now(),
	}
	// 先落库再启动 goroutine：202 一旦返回，job_id 就必须可查（含进程重启后）。
	if err := s.store.SaveJob(ctx, job); err != nil {
		return BuildJob{}, err
	}

	req := CompileRequest{
		StrategyID:       id,
		VersionHash:      versionHash,
		Carrier:          version.Carrier,
		ABIVersion:       version.ABIVersion,
		Entry:            version.Entry,
		Source:           version.Source,
		MemoryLimitBytes: s.memoryLimitBytes,
	}
	// 可选派发面：编译器能跨进程派发时，作业状态由对方经内部端点回写；
	// 派发不出去（找不到可执行文件等）就把刚落的 queued 作业收口成 failed，
	// 端点仍按「编译服务不可用」返回 503，与「编译失败」可区分。
	if dispatcher, ok := s.compiler.(BuildDispatcher); ok {
		if err := dispatcher.DispatchBuild(ctx, DispatchRequest{JobID: job.ID, CompileRequest: req}); err != nil {
			s.setJobState(job.ID, JobFailed, "", ErrorCodeCompileUnavailable, err.Error())
			return BuildJob{}, fmt.Errorf("%w: dispatch build: %v", ErrCompileUnavailable, err)
		}
		return job, nil
	}

	go s.runBuild(job.ID, req)

	return job, nil
}

// runBuild 执行一次编译并落 artifact 行；失败写回 job 状态。
func (s *Service) runBuild(jobID string, req CompileRequest) {
	s.setJobState(jobID, JobRunning, "", "", "")

	ctx, cancel := context.WithTimeout(context.Background(), s.compileTimeout)
	defer cancel()

	res, err := s.compiler.Compile(ctx, req)
	if err != nil {
		s.setJobState(jobID, JobFailed, "", ErrorCodeCompileFailed, fmt.Sprintf("%v", err))
		return
	}
	artifact := Artifact{
		StrategyID:       req.StrategyID,
		VersionHash:      req.VersionHash,
		ArtifactHash:     ArtifactHash(res.Bytes),
		Carrier:          req.Carrier,
		ABIVersion:       req.ABIVersion,
		Toolchain:        s.compiler.ToolchainID(),
		SizeBytes:        int64(len(res.Bytes)),
		MemoryLimitBytes: req.MemoryLimitBytes,
		Imports:          append([]string(nil), res.Imports...),
		Status:           StatusBuilt,
		Storage:          StorageInline,
		CompileLog:       res.Log,
		BuiltBy:          "compiler:" + s.compiler.ToolchainID(),
		CreatedAt:        s.now(),
		Bytes:            append([]byte(nil), res.Bytes...),
	}
	if err := Verify(artifact, req.Entry); err != nil {
		// 编译产出的字节过不了自己的门 ⇒ 不落库，作业失败可检索。
		s.setJobState(jobID, JobFailed, "", ErrorCodeArtifactInvalid, fmt.Sprintf("%v", err))
		return
	}
	if err := normalizeImports(&artifact); err != nil {
		s.setJobState(jobID, JobFailed, "", ErrorCodeArtifactInvalid, fmt.Sprintf("%v", err))
		return
	}
	saved, err := s.store.SaveArtifact(ctx, artifact)
	if err != nil {
		s.setJobState(jobID, JobFailed, "", ErrorCodeArtifactInvalid, fmt.Sprintf("%v", err))
		return
	}
	if s.cache != nil {
		_ = s.cache.Put(saved.ArtifactHash, saved.Bytes)
	}
	s.setJobState(jobID, JobSucceeded, saved.ArtifactHash, "", "")
}

func (s *Service) setJobState(jobID, state, artifactHash, errorCode, errMsg string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		// 作业状态更新失败只能记账：调用方已经拿到 202，没有第二条回传通道。
		slog.Error("build job: load state", "job_id", jobID, "error", err)
		return
	}
	job.State = state
	if artifactHash != "" {
		job.ArtifactHash = artifactHash
	}
	job.ErrorCode = errorCode
	job.Error = errMsg
	if state == JobSucceeded || state == JobFailed {
		now := s.now()
		job.FinishedAt = &now
	}
	if err := s.store.UpdateJob(ctx, job); err != nil {
		slog.Error("build job: persist state", "job_id", jobID, "error", err)
	}
}

// ReportBuildJob 是编译进程回写作业状态的内部入口。
//
// 归属校验先于一切：job 不存在或不属于路径 id 一律 ErrJobNotFound（404），
// 不跨策略枚举。合法转移：queued→running→succeeded|failed，queued 直达终态，
// 同终态重复上报幂等；终态不可回退。succeeded 必须给出已登记且与作业
// (version, toolchain) 一致的 artifact；failed 的 error_code 缺省 compile_failed。
func (s *Service) ReportBuildJob(ctx context.Context, strategyID, jobID string, report BuildJobReport) (BuildJob, error) {
	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		return BuildJob{}, err
	}
	if job.StrategyID != strategyID {
		return BuildJob{}, fmt.Errorf("%w: %s", ErrJobNotFound, jobID)
	}
	state := strings.TrimSpace(report.State)
	switch state {
	case JobRunning, JobSucceeded, JobFailed:
	default:
		return BuildJob{}, fmt.Errorf("%w: unknown build state %q", ErrInvalidParameter, report.State)
	}
	if state == job.State && (state == JobSucceeded || state == JobFailed) {
		return job, nil
	}
	if !allowedJobTransition(job.State, state) {
		return BuildJob{}, fmt.Errorf("%w: job %s is %s, cannot report %s",
			ErrInvalidTransition, jobID, job.State, state)
	}

	switch state {
	case JobRunning:
		job.State = state
	case JobSucceeded:
		hash := strings.TrimSpace(report.ArtifactHash)
		if !isLowerHex64(hash) {
			return BuildJob{}, fmt.Errorf("%w: succeeded job requires a 64-hex artifact_hash", ErrInvalidArtifact)
		}
		artifact, err := s.store.GetArtifact(ctx, strategyID, hash)
		if err != nil {
			if errors.Is(err, ErrArtifactNotFound) {
				return BuildJob{}, fmt.Errorf("%w: artifact %s is not registered", ErrInvalidArtifact, hash)
			}
			return BuildJob{}, err
		}
		if artifact.VersionHash != job.VersionHash || artifact.Toolchain != job.Toolchain {
			return BuildJob{}, fmt.Errorf("%w: artifact %s belongs to (%s, %s), job is (%s, %s)",
				ErrInvalidArtifact, hash, artifact.VersionHash, artifact.Toolchain, job.VersionHash, job.Toolchain)
		}
		now := s.now()
		job.State = state
		job.ArtifactHash = hash
		job.ErrorCode = ""
		job.Error = ""
		job.FinishedAt = &now
	case JobFailed:
		code := strings.TrimSpace(report.ErrorCode)
		if code == "" {
			code = ErrorCodeCompileFailed
		}
		now := s.now()
		job.State = state
		job.ArtifactHash = ""
		job.ErrorCode = code
		job.Error = report.Error
		job.FinishedAt = &now
	}
	if err := s.store.UpdateJob(ctx, job); err != nil {
		return BuildJob{}, err
	}
	return job, nil
}

// allowedJobTransition 是 build job 的状态机（queued 可直达终态，终态只读）。
func allowedJobTransition(from, to string) bool {
	switch from {
	case JobQueued:
		return to == JobRunning || to == JobSucceeded || to == JobFailed
	case JobRunning:
		return to == JobRunning || to == JobSucceeded || to == JobFailed
	default:
		return false
	}
}

// Job 返回作业快照（来自持久化 Store，跨进程重启可查）。
func (s *Service) Job(ctx context.Context, jobID string) (BuildJob, error) {
	return s.store.GetJob(ctx, jobID)
}

// ValidateVersion 是 W4：静态校验 + 状态机 built → validated/rejected。
func (s *Service) ValidateVersion(ctx context.Context, id, versionHash, toolchain string) (Artifact, error) {
	a, err := s.pickArtifact(ctx, id, versionHash, toolchain)
	if err != nil {
		return Artifact{}, err
	}
	// 元数据接口不带字节：校验门必须拿到真实字节，不能拿「有这行」当通过。
	a, err = s.artifactWithBytes(ctx, a)
	if err != nil {
		return Artifact{}, err
	}
	version, err := s.store.GetVersion(ctx, id, versionHash)
	if err != nil {
		return Artifact{}, err
	}
	if err := Verify(a, version.Entry); err != nil {
		_ = s.store.UpdateStatus(ctx, id, a.ArtifactHash, StatusBuilt, StatusRejected, err.Error())
		return Artifact{}, err
	}
	if err := s.store.UpdateStatus(ctx, id, a.ArtifactHash, StatusBuilt, StatusValidated, "validated"); err != nil {
		return Artifact{}, err
	}
	return s.store.GetArtifact(ctx, id, a.ArtifactHash)
}

// Promote 是 W5：validated → promoted，并写 definition 的 active_*。
//
// 状态与生效指针由 Store 在**一个事务**里写（PG）或一把锁内写（Memory）；
// 两步分开写会留下「artifact 已 promoted、latest 还指旧字节」且无法自愈的状态。
func (s *Service) Promote(ctx context.Context, id, versionHash, toolchain string) (Artifact, error) {
	a, err := s.pickArtifact(ctx, id, versionHash, toolchain)
	if err != nil {
		return Artifact{}, err
	}
	if a.Status != StatusValidated {
		return Artifact{}, fmt.Errorf("%w: artifact %s is %s, want validated", ErrInvalidTransition, a.ArtifactHash, a.Status)
	}
	if err := s.store.Promote(ctx, id, versionHash, a.ArtifactHash, "promoted"); err != nil {
		return Artifact{}, err
	}
	// F 裁决：promote 的响应已带新 hash；本进程必须在同一刻失效 latest，
	// 否则 300s 内 resolve?ref=latest 还指着旧字节。
	if s.cache != nil {
		s.cache.InvalidateLatest(id)
	}
	return s.store.GetArtifact(ctx, id, a.ArtifactHash)
}

// Deprecate 是 W6：promoted → deprecated；清空 active 指针（历史 hash 仍可解析）。
func (s *Service) Deprecate(ctx context.Context, id, versionHash, toolchain string) (Artifact, error) {
	a, err := s.pickArtifact(ctx, id, versionHash, toolchain)
	if err != nil {
		return Artifact{}, err
	}
	if a.Status != StatusPromoted {
		return Artifact{}, fmt.Errorf("%w: artifact %s is %s, want promoted", ErrInvalidTransition, a.ArtifactHash, a.Status)
	}
	if err := s.store.Deprecate(ctx, id, versionHash, a.ArtifactHash, "deprecated"); err != nil {
		return Artifact{}, err
	}
	if s.cache != nil {
		s.cache.InvalidateLatest(id)
	}
	return s.store.GetArtifact(ctx, id, a.ArtifactHash)
}

// VersionView 是 W7 的一行。
//
// 同一版本多工具链 → 多行（artifact_hash/toolchain 各不相同）；只有源码、还没
// 产物的版本给一行 status="source"（DB 的 status 列只管 artifact 行，这是派生值）。
type VersionView struct {
	VersionHash  string    `json:"version_hash"`
	Status       string    `json:"status"`
	ArtifactHash string    `json:"artifact_hash,omitempty"`
	Toolchain    string    `json:"toolchain,omitempty"`
	SizeBytes    int64     `json:"size_bytes"`
	CreatedAt    time.Time `json:"created_at"`
}

// ListVersions 是 W7：无版本返回空切片（不是 null）。
func (s *Service) ListVersions(ctx context.Context, id string) ([]VersionView, error) {
	versions, err := s.store.ListVersions(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]VersionView, 0, len(versions))
	for _, v := range versions {
		artifacts, err := s.store.ArtifactsForVersion(ctx, id, v.VersionHash)
		if err != nil {
			return nil, err
		}
		if len(artifacts) == 0 {
			out = append(out, VersionView{
				VersionHash: v.VersionHash,
				Status:      "source",
				CreatedAt:   v.CreatedAt,
			})
			continue
		}
		for _, a := range artifacts {
			out = append(out, VersionView{
				VersionHash:  v.VersionHash,
				Status:       string(a.Status),
				ArtifactHash: a.ArtifactHash,
				Toolchain:    a.Toolchain,
				SizeBytes:    a.SizeBytes,
				CreatedAt:    a.CreatedAt,
			})
		}
	}
	return out, nil
}

// Resolve 是 R1：ref → 元数据。**绝不回退 latest / 内置同名策略**。
//
// ref 三形态（contracts.StrategyRef 的语义）：
//   - latest（空串 / "latest"）→ definition 的 active_*；
//   - 64 hex → 先按 artifact_hash 查，查不到再按 version_hash 解析到该版本当前
//     promoted 的 artifact，并把实际 artifact_hash 回写；
//   - `id@hash` → id 必须与路径一致（否则 400），其余同上。
func (s *Service) Resolve(ctx context.Context, id, ref string) (ResolveResult, error) {
	parsed, err := parseResolveRef(id, ref)
	if err != nil {
		return ResolveResult{}, err
	}
	if parsed.IsLatest() && s.cache != nil {
		if cached, ok := s.cache.Latest(id); ok {
			return cached, nil
		}
	}
	def, err := s.store.GetDefinition(ctx, id)
	if err != nil {
		return ResolveResult{}, err
	}
	if parsed.IsLatest() {
		if def.ActiveVersionHash == "" || def.ActiveArtifactHash == "" {
			return ResolveResult{}, fmt.Errorf("%w: %s", ErrNoActiveVersion, id)
		}
		a, err := s.store.GetArtifact(ctx, id, def.ActiveArtifactHash)
		if err != nil {
			return ResolveResult{}, err
		}
		result := buildResolveResult(def, a)
		if s.cache != nil {
			s.cache.PutLatest(id, result)
		}
		return result, nil
	}

	if a, err := s.store.GetArtifact(ctx, id, parsed.Hash); err == nil {
		if a.Status == StatusRejected {
			return ResolveResult{}, fmt.Errorf("%w: artifact %s is rejected", ErrInvalidArtifact, a.ArtifactHash)
		}
		return buildResolveResult(def, a), nil
	} else if !errors.Is(err, ErrArtifactNotFound) {
		return ResolveResult{}, err
	}

	if _, err := s.store.GetVersion(ctx, id, parsed.Hash); err != nil {
		return ResolveResult{}, err
	}
	artifacts, err := s.store.ArtifactsForVersion(ctx, id, parsed.Hash)
	if err != nil {
		return ResolveResult{}, err
	}
	for _, a := range artifacts {
		if a.Status == StatusPromoted {
			return buildResolveResult(def, a), nil
		}
	}
	return ResolveResult{}, fmt.Errorf("%w: version %s has no promoted artifact", ErrNoActiveVersion, parsed.Hash)
}

// ArtifactForVersion 是 R2：按版本 hash（+ 可选 toolchain）取字节。
//
// 字节与登记 hash 不符必须 fail-closed；storage=url 分支未实现，同一条门拒收。
func (s *Service) ArtifactForVersion(ctx context.Context, id, versionHash, toolchain string) (Artifact, error) {
	a, err := s.pickArtifact(ctx, id, versionHash, toolchain)
	if err != nil {
		return Artifact{}, err
	}
	a, err = s.artifactWithBytes(ctx, a)
	if err != nil {
		return Artifact{}, err
	}
	version, err := s.store.GetVersion(ctx, id, versionHash)
	if err != nil {
		return Artifact{}, err
	}
	if err := Verify(a, version.Entry); err != nil {
		return Artifact{}, err
	}
	return a, nil
}

// artifactWithBytes 用内容寻址缓存补字节：L1 → L2 → Store。
//
// 缓存写失败不影响正确性（缓存只是副本），读路径必须继续。
func (s *Service) artifactWithBytes(ctx context.Context, meta Artifact) (Artifact, error) {
	if s.cache != nil {
		if raw, ok := s.cache.Get(meta.ArtifactHash); ok {
			meta.Bytes = raw
			return meta, nil
		}
	}
	full, err := s.store.GetArtifact(ctx, meta.StrategyID, meta.ArtifactHash)
	if err != nil {
		return Artifact{}, err
	}
	if s.cache != nil {
		_ = s.cache.Put(full.ArtifactHash, full.Bytes)
	}
	return full, nil
}

// RegisterArtifact 是编译服务回填 artifact 的内部入口。
//
// 与 W3 的异步 build 走同一个落库形状：登记即 built；校验 / promote 仍是显式
// 端点（编译服务不能自己把产物 promote）。
func (s *Service) RegisterArtifact(ctx context.Context, a Artifact) (Artifact, error) {
	if err := a.Validate(); err != nil {
		return Artifact{}, err
	}
	version, err := s.store.GetVersion(ctx, a.StrategyID, a.VersionHash)
	if err != nil {
		return Artifact{}, err
	}
	if a.Carrier != version.Carrier || a.ABIVersion != version.ABIVersion {
		return Artifact{}, fmt.Errorf("%w: artifact carrier/abi do not match version", ErrInvalidArtifact)
	}
	if err := Verify(a, version.Entry); err != nil {
		return Artifact{}, err
	}
	if err := normalizeImports(&a); err != nil {
		return Artifact{}, err
	}
	saved, err := s.store.SaveArtifact(ctx, a)
	if err != nil {
		return Artifact{}, err
	}
	if s.cache != nil {
		_ = s.cache.Put(saved.ArtifactHash, saved.Bytes)
	}
	return saved, nil
}

// pickArtifact 按 (version, toolchain) 选一行；toolchain 为空且多行 → 409。
func (s *Service) pickArtifact(ctx context.Context, id, versionHash, toolchain string) (Artifact, error) {
	artifacts, err := s.store.ArtifactsForVersion(ctx, id, versionHash)
	if err != nil {
		return Artifact{}, err
	}
	if len(artifacts) == 0 {
		return Artifact{}, fmt.Errorf("%w: version %s has no artifact", ErrArtifactNotFound, versionHash)
	}
	if toolchain == "" {
		if len(artifacts) > 1 {
			return Artifact{}, fmt.Errorf("%w: version %s", ErrAmbiguousArtifact, versionHash)
		}
		return artifacts[0], nil
	}
	for _, a := range artifacts {
		if a.Toolchain == toolchain {
			return a, nil
		}
	}
	return Artifact{}, fmt.Errorf("%w: version %s has no artifact from toolchain %q",
		ErrArtifactNotFound, versionHash, toolchain)
}

func buildResolveResult(def Definition, a Artifact) ResolveResult {
	return ResolveResult{
		StrategyID:   def.ID,
		VersionHash:  a.VersionHash,
		ArtifactHash: a.ArtifactHash,
		Carrier:      a.Carrier,
		ABIVersion:   a.ABIVersion,
		Scope:        def.Scope,
		Capability:   def.Capability.Clone(),
		SizeBytes:    a.SizeBytes,
		MemoryLimit:  a.MemoryLimitBytes,
		Imports:      append([]string(nil), a.Imports...),
		Toolchain:    a.Toolchain,
	}
}

// parseResolveRef 接受 latest / 空 / 64 hex / id@hash（id 必须等于路径 id）。
//
// 裸 64 hex 必须先于 ParseStrategyRef 判掉：contracts.ParseStrategyRef 把不带
// `@` 的字符串一律当 id，`{hash|latest}` 里的 hash 会被误读成策略 id，于是
// 「按 hash 解析」全部退化成 id 不匹配。
func parseResolveRef(id, ref string) (contracts.StrategyRef, error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" || trimmed == "latest" {
		return contracts.StrategyRef{ID: id}, nil
	}
	if isLowerHex64(trimmed) {
		return contracts.StrategyRef{ID: id, Hash: trimmed}, nil
	}
	parsed, err := contracts.ParseStrategyRef(trimmed)
	if err != nil {
		return contracts.StrategyRef{}, fmt.Errorf("%w: ref %q: %v", ErrInvalidParameter, ref, err)
	}
	if parsed.ID != id {
		return contracts.StrategyRef{}, fmt.Errorf("%w: ref id %q does not match path id %q",
			ErrInvalidParameter, parsed.ID, id)
	}
	return parsed, nil
}

// randomID 生成 `st_` + 16 字节随机 hex（定义与 job 共用前缀形态）。
func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand 在受支持平台上不会失败；失败意味着系统熵源坏了，
		// 不能退化成时间戳 id（那会破坏「同一次创建可被审计对齐」）。
		panic(fmt.Sprintf("strategy: id generation failed: %v", err))
	}
	return "st_" + hex.EncodeToString(b[:])
}
