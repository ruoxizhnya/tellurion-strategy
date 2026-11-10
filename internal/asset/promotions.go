package asset

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// PromotionEvent 是一次 promote 的审计留痕（SVC-STRAT-G1 ②）。
//
// 三件套是硬要求：**人**（Actor，来自 core 鉴权后转发的受信 `X-Actor`）、
// **时间**（DecidedAt）、**结论**（Outcome）。只追加、不更新、不删除——
// 审计行被改写等于没有审计。
type PromotionEvent struct {
	StrategyID   string    `json:"strategy_id"`
	VersionHash  string    `json:"version_hash"`
	ArtifactHash string    `json:"artifact_hash"`
	Actor        string    `json:"actor"`
	Outcome      string    `json:"outcome"`
	DecidedAt    time.Time `json:"decided_at"`
}

// PromotionFilter 是审计读面的筛选条件。
//
// StrategyID 必填；VersionHash 可选且按精确字符串匹配，用于回答「这个版本的
// 审批记录是谁、什么时候、结论是什么」。Limit/Offset 由 HTTP 层按分页上界
// 校验后传入（<=0 表示不分页，仅供内部调用）。
type PromotionFilter struct {
	StrategyID  string
	VersionHash string
	Limit       int
	Offset      int
}

// PromotionLog 是 promote 审计的读写端口。未装配时 HTTP 层不写审计行
// （只有测试/内存演示形态会这样；`cmd/assetservice` 两种存储形态都装配）。
type PromotionLog interface {
	RecordPromotion(ctx context.Context, ev PromotionEvent) error
	// ListPromotions 按 filter 返回审计行，**新的在前**（append-only 表按
	// 插入序倒排）。无行返回空切片而不是 nil。
	ListPromotions(ctx context.Context, filter PromotionFilter) ([]PromotionEvent, error)
}

// validatePromotionFilter 校验读面入参：策略 id 必填；版本 hash 按**精确
// 字符串**匹配（与写入侧同口径，不在这里另造一套 hash 形状规则——写入侧
// 允许的任一非空 hash 都必须可被读回，否则审计会出现「写进去查不到」）。
func validatePromotionFilter(filter PromotionFilter) error {
	if strings.TrimSpace(filter.StrategyID) == "" {
		return fmt.Errorf("%w: promotion audit requires a strategy id", ErrInvalidParameter)
	}
	return nil
}

// pagePromotions 对已按「新的在前」排好的切片做分页。
func pagePromotions(events []PromotionEvent, limit, offset int) []PromotionEvent {
	if offset > len(events) {
		offset = len(events)
	}
	end := len(events)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return events[offset:end]
}

// MemoryPromotionLog 是进程内审计（单仓测试与内存 Store 形态）。
// 进程退出即丢；生产用 PG 实现。
type MemoryPromotionLog struct {
	mu     sync.Mutex
	events []PromotionEvent
}

// NewMemoryPromotionLog 返回空审计。
func NewMemoryPromotionLog() *MemoryPromotionLog { return &MemoryPromotionLog{} }

// RecordPromotion 追加一行（Actor / Outcome 为空 fail-closed：半截审计行
// 比没有更糟——它会让「谁批的」看起来有答案）。
func (m *MemoryPromotionLog) RecordPromotion(ctx context.Context, ev PromotionEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(ev.Actor) == "" {
		return fmt.Errorf("%w: promotion audit requires a non-empty actor", ErrInvalidParameter)
	}
	if strings.TrimSpace(ev.Outcome) == "" {
		return fmt.Errorf("%w: promotion audit requires a non-empty outcome", ErrInvalidParameter)
	}
	if ev.DecidedAt.IsZero() {
		ev.DecidedAt = time.Now().UTC()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, ev)
	return nil
}

// Events 返回审计行副本（用例断言用）。
func (m *MemoryPromotionLog) Events() []PromotionEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]PromotionEvent, len(m.events))
	copy(out, m.events)
	return out
}

// ListPromotions 实现读面：按插入序倒排 + 可选版本过滤 + 分页。
func (m *MemoryPromotionLog) ListPromotions(ctx context.Context, filter PromotionFilter) ([]PromotionEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validatePromotionFilter(filter); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]PromotionEvent, 0, len(m.events))
	for i := len(m.events) - 1; i >= 0; i-- {
		ev := m.events[i]
		if ev.StrategyID != filter.StrategyID {
			continue
		}
		if filter.VersionHash != "" && ev.VersionHash != filter.VersionHash {
			continue
		}
		out = append(out, ev)
	}
	return pagePromotions(out, filter.Limit, filter.Offset), nil
}

// RecordPromotion 落 `strategy.strategy_promotions`（append-only）。
func (p *PG) RecordPromotion(ctx context.Context, ev PromotionEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(ev.Actor) == "" {
		return fmt.Errorf("%w: promotion audit requires a non-empty actor", ErrInvalidParameter)
	}
	if strings.TrimSpace(ev.Outcome) == "" {
		return fmt.Errorf("%w: promotion audit requires a non-empty outcome", ErrInvalidParameter)
	}
	if ev.DecidedAt.IsZero() {
		ev.DecidedAt = time.Now().UTC()
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO strategy.strategy_promotions
			(strategy_id, version_hash, artifact_hash, actor, outcome, decided_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		ev.StrategyID, ev.VersionHash, ev.ArtifactHash, ev.Actor, ev.Outcome, ev.DecidedAt)
	if err != nil {
		return fmt.Errorf("asset: record promotion audit: %w", err)
	}
	return nil
}

// ListPromotions 读 `strategy.strategy_promotions`（新的在前）。
//
// `version_hash` / `artifact_hash` 是 CHAR(64)，PG 读回带空格 ⇒ 必须 trim，
// 否则调用方拿到的 hash 与内容寻址口径不一致。
func (p *PG) ListPromotions(ctx context.Context, filter PromotionFilter) ([]PromotionEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validatePromotionFilter(filter); err != nil {
		return nil, err
	}
	query := `SELECT strategy_id, trim(version_hash), trim(artifact_hash), actor, outcome, decided_at
		FROM strategy.strategy_promotions WHERE strategy_id = $1`
	args := []any{filter.StrategyID}
	if filter.VersionHash != "" {
		args = append(args, filter.VersionHash)
		query += fmt.Sprintf(" AND trim(version_hash) = $%d", len(args))
	}
	// Limit<=0 = 不分页（不能写成 LIMIT 0，那会读成「一条都没有」）。
	query += " ORDER BY id DESC"
	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if filter.Offset > 0 {
		args = append(args, filter.Offset)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("asset: list promotion audit: %w", err)
	}
	defer rows.Close()

	out := make([]PromotionEvent, 0, 8)
	for rows.Next() {
		var ev PromotionEvent
		if err := rows.Scan(&ev.StrategyID, &ev.VersionHash, &ev.ArtifactHash,
			&ev.Actor, &ev.Outcome, &ev.DecidedAt); err != nil {
			return nil, fmt.Errorf("asset: scan promotion audit: %w", err)
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("asset: list promotion audit: %w", err)
	}
	return out, nil
}

// Ready 是 `/readyz` 的检查：连接池可连 + 本仓迁移已执行。
//
// 「迁移已执行」不是读一个启动期布尔值，而是问库本身（`to_regclass`）——
// 进程刚起来、迁移半途失败、或别人换了库，布尔值都会撒谎。
func (p *PG) Ready(ctx context.Context) error {
	if err := p.pool.Ping(ctx); err != nil {
		return fmt.Errorf("postgres ping: %w", err)
	}
	required := []string{
		"strategy_definitions",
		"strategy_versions",
		"strategy_artifacts",
		"strategy_artifact_blobs",
		"strategy_build_jobs",
		"strategy_promotions",
	}
	var missing []string
	for _, table := range required {
		var reg *string
		if err := p.pool.QueryRow(ctx, `SELECT to_regclass($1)::text`, "strategy."+table).Scan(&reg); err != nil {
			return fmt.Errorf("check table %s: %w", table, err)
		}
		if reg == nil {
			missing = append(missing, table)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("migrations not applied: missing %s", strings.Join(missing, ", "))
	}
	return nil
}
