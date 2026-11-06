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

// PromotionLog 是 promote 审计的写端口。未装配时 HTTP 层不写审计行
// （只有测试/内存演示形态会这样；`cmd/assetservice` 两种存储形态都装配）。
type PromotionLog interface {
	RecordPromotion(ctx context.Context, ev PromotionEvent) error
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
