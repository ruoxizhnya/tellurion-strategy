package asset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	contracts "github.com/ruoxizhnya/tellurion-contracts/engine"
)

// PG 是 Store 的 PostgreSQL 实现（strategy schema 的唯一写者）。
//
// 与 Memory 的语义逐条对齐：同 hash 幂等、同 (version, toolchain) 第二份字节
// 冲突、状态机拒绝非法跃迁。两处写（artifact 状态 + definition 生效指针）
// 必须同事务，否则会出现「已 promoted 但 latest 还指着旧字节」的半截状态。
type PG struct {
	pool *pgxpool.Pool
}

// OpenPG 建连接池并 ping；DSN 为空 fail-closed（不静默退回 Memory）。
func OpenPG(ctx context.Context, dsn string) (*PG, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("asset: postgres dsn is required")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("asset: open postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("asset: ping postgres: %w", err)
	}
	return &PG{pool: pool}, nil
}

// Close 释放连接池。
func (p *PG) Close() { p.pool.Close() }

// Migrate 按序执行 Migrations（幂等、只前滚）。服务启动时在监听前调用。
func (p *PG) Migrate(ctx context.Context) error {
	for i, stmt := range Migrations() {
		if _, err := p.pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("asset: migration %d: %w", i+1, err)
		}
	}
	return nil
}

// CreateDefinition 新建定义；重复 id 报 ErrStrategyExists。
func (p *PG) CreateDefinition(ctx context.Context, def Definition) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := def.Validate(); err != nil {
		return err
	}
	capability, err := json.Marshal(def.Capability)
	if err != nil {
		return fmt.Errorf("%w: capability: %v", ErrInvalidDefinition, err)
	}
	_, err = p.pool.Exec(ctx, `
		INSERT INTO strategy.strategy_definitions
			(id, carrier, abi_version, scope, capability, entry,
			 active_version_hash, active_artifact_hash, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		def.ID, string(def.Carrier), def.ABIVersion, string(def.Scope), capability, def.Entry,
		nullable(def.ActiveVersionHash), nullable(def.ActiveArtifactHash), def.CreatedAt, def.UpdatedAt)
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: %q", ErrStrategyExists, def.ID)
	}
	return err
}

// GetDefinition 返回定义（active_* 用 trim 读回：CHAR(64) 会补空格）。
func (p *PG) GetDefinition(ctx context.Context, id string) (Definition, error) {
	if err := ctx.Err(); err != nil {
		return Definition{}, err
	}
	var (
		def        Definition
		carrier    string
		scope      string
		capability []byte
		activeV    *string
		activeA    *string
	)
	err := p.pool.QueryRow(ctx, `
		SELECT id, carrier, abi_version, scope, capability, entry,
		       trim(active_version_hash), trim(active_artifact_hash), created_at, updated_at
		  FROM strategy.strategy_definitions WHERE id = $1`, id).
		Scan(&def.ID, &carrier, &def.ABIVersion, &scope, &capability, &def.Entry,
			&activeV, &activeA, &def.CreatedAt, &def.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Definition{}, fmt.Errorf("%w: %q", ErrStrategyNotFound, id)
	}
	if err != nil {
		return Definition{}, err
	}
	if err := json.Unmarshal(capability, &def.Capability); err != nil {
		return Definition{}, fmt.Errorf("asset: definition %s capability: %w", id, err)
	}
	def.Carrier = Carrier(carrier)
	def.Scope = strategyScope(scope)
	def.ActiveVersionHash = deref(activeV)
	def.ActiveArtifactHash = deref(activeA)
	return def, nil
}

// SetActive 写 active_version_hash / active_artifact_hash；空串 = 清空（deprecate）。
func (p *PG) SetActive(ctx context.Context, id, versionHash, artifactHash string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if (versionHash == "") != (artifactHash == "") {
		return fmt.Errorf("%w: active version and artifact must be set or cleared together", ErrInvalidDefinition)
	}
	tag, err := p.pool.Exec(ctx, `
		UPDATE strategy.strategy_definitions
		   SET active_version_hash = $2, active_artifact_hash = $3, updated_at = $4
		 WHERE id = $1`,
		id, nullable(versionHash), nullable(artifactHash), time.Now().UTC())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %q", ErrStrategyNotFound, id)
	}
	return nil
}

// SaveVersion 登记源码版本；同 hash 幂等（内容寻址）。
func (p *PG) SaveVersion(ctx context.Context, v Version) (Version, error) {
	if err := ctx.Err(); err != nil {
		return Version{}, err
	}
	if strings.TrimSpace(v.StrategyID) == "" || !isLowerHex64(v.VersionHash) {
		return Version{}, fmt.Errorf("%w: version needs a strategy id and a 64-hex hash", ErrInvalidDefinition)
	}
	tag, err := p.pool.Exec(ctx, `
		INSERT INTO strategy.strategy_versions
			(strategy_id, version_hash, source, carrier, abi_version, scope, entry, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (strategy_id, version_hash) DO NOTHING`,
		v.StrategyID, v.VersionHash, v.Source, string(v.Carrier), v.ABIVersion, string(v.Scope), v.Entry, v.CreatedAt)
	if isForeignKeyViolation(err) {
		return Version{}, fmt.Errorf("%w: %q", ErrStrategyNotFound, v.StrategyID)
	}
	if err != nil {
		return Version{}, err
	}
	if tag.RowsAffected() == 0 {
		existing, err := p.GetVersion(ctx, v.StrategyID, v.VersionHash)
		if err != nil {
			return Version{}, err
		}
		if existing.Source != v.Source || existing.Carrier != v.Carrier ||
			existing.ABIVersion != v.ABIVersion || existing.Scope != v.Scope || existing.Entry != v.Entry {
			return Version{}, fmt.Errorf("%w: version %s is registered with different content",
				ErrArtifactConflict, v.VersionHash)
		}
		return existing, nil
	}
	return v, nil
}

// GetVersion 返回版本行。
func (p *PG) GetVersion(ctx context.Context, id, versionHash string) (Version, error) {
	return p.versionRow(ctx, p.pool, id, versionHash)
}

func (p *PG) versionRow(ctx context.Context, q querier, id, versionHash string) (Version, error) {
	if err := ctx.Err(); err != nil {
		return Version{}, err
	}
	var (
		v       Version
		carrier string
		scope   string
	)
	err := q.QueryRow(ctx, `
		SELECT strategy_id, trim(version_hash), source, carrier, abi_version, scope, entry, created_at
		  FROM strategy.strategy_versions WHERE strategy_id = $1 AND version_hash = $2`,
		id, versionHash).
		Scan(&v.StrategyID, &v.VersionHash, &v.Source, &carrier, &v.ABIVersion, &scope, &v.Entry, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, fmt.Errorf("%w: %s", ErrVersionNotFound, versionHash)
	}
	if err != nil {
		return Version{}, err
	}
	v.Carrier = Carrier(carrier)
	v.Scope = strategyScope(scope)
	return v, nil
}

// ListVersions 返回版本列表（created_at，再按 hash，保证确定性）。
func (p *PG) ListVersions(ctx context.Context, id string) ([]Version, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := p.GetDefinition(ctx, id); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `
		SELECT strategy_id, trim(version_hash), source, carrier, abi_version, scope, entry, created_at
		  FROM strategy.strategy_versions WHERE strategy_id = $1
		 ORDER BY created_at, version_hash`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Version
	for rows.Next() {
		var (
			v       Version
			carrier string
			scope   string
		)
		if err := rows.Scan(&v.StrategyID, &v.VersionHash, &v.Source, &carrier, &v.ABIVersion, &scope,
			&v.Entry, &v.CreatedAt); err != nil {
			return nil, err
		}
		v.Carrier = Carrier(carrier)
		v.Scope = strategyScope(scope)
		out = append(out, v)
	}
	return out, rows.Err()
}

// SaveArtifact 登记 artifact 元数据 + 字节（blobs 行同事务写入）。
func (p *PG) SaveArtifact(ctx context.Context, a Artifact) (Artifact, error) {
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

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return Artifact{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := p.versionRow(ctx, tx, a.StrategyID, a.VersionHash); err != nil {
		return Artifact{}, err
	}

	existing, err := p.artifactRow(ctx, tx, a.StrategyID, a.ArtifactHash, true)
	switch {
	case err == nil:
		if existing.VersionHash != a.VersionHash || existing.Toolchain != a.Toolchain ||
			existing.SizeBytes != a.SizeBytes || existing.Storage != a.Storage {
			return Artifact{}, fmt.Errorf("%w: artifact %s is registered with different content",
				ErrArtifactConflict, a.ArtifactHash)
		}
		existingBytes, err := p.artifactBlob(ctx, tx, a.StrategyID, a.ArtifactHash)
		if err != nil {
			return Artifact{}, err
		}
		if sha256Hex(existingBytes) != sha256Hex(a.Bytes) {
			return Artifact{}, fmt.Errorf("%w: artifact %s is registered with different bytes",
				ErrArtifactConflict, a.ArtifactHash)
		}
		existing.Bytes = existingBytes
		return existing.Clone(), nil
	case !errors.Is(err, ErrArtifactNotFound):
		return Artifact{}, err
	}

	var otherHash string
	err = tx.QueryRow(ctx, `
		SELECT artifact_hash FROM strategy.strategy_artifacts
		 WHERE strategy_id = $1 AND version_hash = $2 AND toolchain = $3 FOR UPDATE`,
		a.StrategyID, a.VersionHash, a.Toolchain).Scan(&otherHash)
	if err == nil {
		return Artifact{}, fmt.Errorf("%w: version %s already has an artifact from toolchain %q",
			ErrArtifactConflict, a.VersionHash, a.Toolchain)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Artifact{}, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO strategy.strategy_artifacts
			(strategy_id, artifact_hash, version_hash, carrier, abi_version, toolchain, size_bytes,
			 memory_limit_bytes, imports, status, storage, compile_log, built_by, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		a.StrategyID, a.ArtifactHash, a.VersionHash, string(a.Carrier), a.ABIVersion, a.Toolchain,
		a.SizeBytes, a.MemoryLimitBytes, nonNilStrings(a.Imports), string(a.Status), a.Storage, a.CompileLog,
		a.BuiltBy, a.CreatedAt); err != nil {
		if isUniqueViolation(err) {
			return Artifact{}, fmt.Errorf("%w: artifact %s", ErrArtifactConflict, a.ArtifactHash)
		}
		if isForeignKeyViolation(err) {
			return Artifact{}, fmt.Errorf("%w: %s", ErrVersionNotFound, a.VersionHash)
		}
		return Artifact{}, err
	}
	if len(a.Bytes) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO strategy.strategy_artifact_blobs (strategy_id, artifact_hash, bytes)
			VALUES ($1,$2,$3)`, a.StrategyID, a.ArtifactHash, a.Bytes); err != nil {
			return Artifact{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Artifact{}, err
	}
	return a.Clone(), nil
}

// GetArtifact 返回 artifact 元数据 + 字节；字节行缺失 fail-closed。
func (p *PG) GetArtifact(ctx context.Context, id, artifactHash string) (Artifact, error) {
	if err := ctx.Err(); err != nil {
		return Artifact{}, err
	}
	a, err := p.artifactRow(ctx, p.pool, id, artifactHash, false)
	if err != nil {
		return Artifact{}, err
	}
	a.Bytes, err = p.artifactBlob(ctx, p.pool, id, artifactHash)
	if err != nil {
		return Artifact{}, err
	}
	return a, nil
}

// ArtifactsForVersion 只返回元数据（字节要另走 GetArtifact）：
// 版本列表与 resolve 不需要把 8 MiB 的 blob 全读一遍。
func (p *PG) ArtifactsForVersion(ctx context.Context, id, versionHash string) ([]Artifact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := p.GetVersion(ctx, id, versionHash); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `
		SELECT strategy_id, artifact_hash, version_hash, carrier, abi_version, toolchain, size_bytes,
		       memory_limit_bytes, imports, status, storage, compile_log, built_by, created_at
		  FROM strategy.strategy_artifacts
		 WHERE strategy_id = $1 AND version_hash = $2
		 ORDER BY toolchain`, id, versionHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpdateStatus 执行状态机跃迁；日志追加在 CompileLog 末尾。
func (p *PG) UpdateStatus(ctx context.Context, id, artifactHash string, from, to Status, log string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !allowedTransition(from, to) {
		return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, from, to)
	}
	return p.updateStatus(ctx, p.pool, id, artifactHash, from, to, log)
}

// Promote 在一个事务里写 artifact 状态与 definition 生效指针（未落项 #10）。
//
// 两步分开写时，第二步失败会留下「artifact=promoted 但 latest 指向旧字节」；
// 下一次 promote 又因状态机拒绝而永远修不回来。
func (p *PG) Promote(ctx context.Context, id, versionHash, artifactHash, log string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := p.updateStatus(ctx, tx, id, artifactHash, StatusValidated, StatusPromoted, log); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE strategy.strategy_definitions
		   SET active_version_hash = $2, active_artifact_hash = $3, updated_at = $4
		 WHERE id = $1`, id, versionHash, artifactHash, time.Now().UTC())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %q", ErrStrategyNotFound, id)
	}
	return tx.Commit(ctx)
}

// Deprecate 在一个事务里写 artifact 状态并按需清空生效指针。
func (p *PG) Deprecate(ctx context.Context, id, versionHash, artifactHash, log string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := p.updateStatus(ctx, tx, id, artifactHash, StatusPromoted, StatusDeprecated, log); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE strategy.strategy_definitions
		   SET active_version_hash = NULL, active_artifact_hash = NULL, updated_at = $3
		 WHERE id = $1 AND active_artifact_hash = $2`, id, artifactHash, time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SaveJob 持久化一条新作业。
func (p *PG) SaveJob(ctx context.Context, job BuildJob) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO strategy.strategy_build_jobs
			(id, strategy_id, version_hash, toolchain, state, artifact_hash, error_code, error, created_at, finished_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		job.ID, job.StrategyID, job.VersionHash, job.Toolchain, job.State,
		job.ArtifactHash, job.ErrorCode, job.Error, job.CreatedAt, job.FinishedAt)
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: job %s", ErrArtifactConflict, job.ID)
	}
	if isForeignKeyViolation(err) {
		return fmt.Errorf("%w: job %s references a missing strategy or version", ErrVersionNotFound, job.ID)
	}
	return err
}

// UpdateJob 覆盖作业的可变字段。
func (p *PG) UpdateJob(ctx context.Context, job BuildJob) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tag, err := p.pool.Exec(ctx, `
		UPDATE strategy.strategy_build_jobs
		   SET state = $2, artifact_hash = $3, error_code = $4, error = $5, finished_at = $6
		 WHERE id = $1`,
		job.ID, job.State, job.ArtifactHash, job.ErrorCode, job.Error, job.FinishedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrJobNotFound, job.ID)
	}
	return nil
}

// GetJob 返回作业快照。
func (p *PG) GetJob(ctx context.Context, id string) (BuildJob, error) {
	if err := ctx.Err(); err != nil {
		return BuildJob{}, err
	}
	var (
		job        BuildJob
		finishedAt *time.Time
	)
	err := p.pool.QueryRow(ctx, `
		SELECT id, strategy_id, trim(version_hash), toolchain, state,
		       trim(artifact_hash), error_code, error, created_at, finished_at
		  FROM strategy.strategy_build_jobs WHERE id = $1`, id).
		Scan(&job.ID, &job.StrategyID, &job.VersionHash, &job.Toolchain, &job.State,
			&job.ArtifactHash, &job.ErrorCode, &job.Error, &job.CreatedAt, &finishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return BuildJob{}, fmt.Errorf("%w: %s", ErrJobNotFound, id)
	}
	if err != nil {
		return BuildJob{}, err
	}
	job.FinishedAt = finishedAt
	return job, nil
}

// ─── 事务内与行扫描的共用件 ────────────────────────────────────

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (p *PG) artifactRow(ctx context.Context, q querier, id, artifactHash string, forUpdate bool) (Artifact, error) {
	sql := `
		SELECT strategy_id, artifact_hash, version_hash, carrier, abi_version, toolchain, size_bytes,
		       memory_limit_bytes, imports, status, storage, compile_log, built_by, created_at
		  FROM strategy.strategy_artifacts
		 WHERE strategy_id = $1 AND artifact_hash = $2`
	if forUpdate {
		sql += " FOR UPDATE"
	}
	a, err := scanArtifact(q.QueryRow(ctx, sql, id, artifactHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return Artifact{}, fmt.Errorf("%w: %s", ErrArtifactNotFound, artifactHash)
	}
	return a, err
}

func (p *PG) artifactBlob(ctx context.Context, q querier, id, artifactHash string) ([]byte, error) {
	var raw []byte
	err := q.QueryRow(ctx, `
		SELECT bytes FROM strategy.strategy_artifact_blobs
		 WHERE strategy_id = $1 AND artifact_hash = $2`, id, artifactHash).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: artifact %s has no blob row", ErrArtifactFetchFailed, artifactHash)
	}
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func (p *PG) updateStatus(ctx context.Context, q interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}, id, artifactHash string, from, to Status, log string) error {
	appendLog := "compile_log"
	args := []any{id, artifactHash, string(from), string(to)}
	if log != "" {
		appendLog = "CASE WHEN compile_log = '' THEN $5 ELSE compile_log || E'\\n' || $5 END"
		args = append(args, log)
	}
	tag, err := q.Exec(ctx, `
		UPDATE strategy.strategy_artifacts
		   SET status = $4, compile_log = `+appendLog+`
		 WHERE strategy_id = $1 AND artifact_hash = $2 AND status = $3`, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	var actual string
	err = q.QueryRow(ctx, `
		SELECT status FROM strategy.strategy_artifacts
		 WHERE strategy_id = $1 AND artifact_hash = $2`, id, artifactHash).Scan(&actual)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrArtifactNotFound, artifactHash)
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: artifact %s is %s, want %s", ErrInvalidTransition, artifactHash, actual, from)
}

func scanArtifact(row pgx.Row) (Artifact, error) {
	var (
		a       Artifact
		carrier string
		status  string
	)
	err := row.Scan(&a.StrategyID, &a.ArtifactHash, &a.VersionHash, &carrier, &a.ABIVersion,
		&a.Toolchain, &a.SizeBytes, &a.MemoryLimitBytes, &a.Imports, &status, &a.Storage,
		&a.CompileLog, &a.BuiltBy, &a.CreatedAt)
	if err != nil {
		return Artifact{}, err
	}
	a.Carrier = Carrier(carrier)
	a.Status = Status(status)
	// CHAR(64) 读回会补空格，trim 掉再交给内容寻址比较。
	a.ArtifactHash = strings.TrimSpace(a.ArtifactHash)
	a.VersionHash = strings.TrimSpace(a.VersionHash)
	return a, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nonNilStrings 把 nil 切片写成 SQL 空数组而不是 NULL（列是 NOT NULL）。
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

func strategyScope(s string) contracts.StrategyScope { return contracts.StrategyScope(s) }

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
