package asset

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// 本文件补齐 SVC-STRAT-G1/G2 里「只用 Memory 验证过」的三条真库腿：
//   - Ready：迁移已执行 → nil；库里没有六张表 → fail-loud（不是「能连就算就绪」）；
//   - RecordPromotion：append-only 追加，空 actor / outcome 拒绝且不落行；
//   - ActiveJob：只认 queued/running，多行取最早，终态不再命中。
//
// 与 pgstore_test.go 同一门槛：无 STRATEGY_TEST_DSN 即 skip。

func TestPG_ReadyRequiresMigratedSchema(t *testing.T) {
	pg := openTestPG(t)
	ctx := context.Background()
	if err := pg.Ready(ctx); err != nil {
		t.Fatalf("Ready on migrated schema: %v", err)
	}

	// 同一个 PG、换一个没跑过本仓迁移的库：能连上但必须报「缺表」。
	dsn := strings.TrimSpace(getenv(t, "STRATEGY_TEST_DSN"))
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	u.Path = "/postgres"
	other, err := OpenPG(ctx, u.String())
	if err != nil {
		t.Fatalf("OpenPG(postgres): %v", err)
	}
	t.Cleanup(other.Close)
	err = other.Ready(ctx)
	if err == nil {
		t.Fatal("Ready on un-migrated database = nil, want missing-tables error")
	}
	if !strings.Contains(err.Error(), "migrations not applied") {
		t.Fatalf("Ready error = %v, want prefix \"migrations not applied\"", err)
	}
}

func TestPG_RecordPromotionIsAppendOnly(t *testing.T) {
	pg := openTestPG(t)
	ctx := context.Background()
	// strategy_promotions 是 append-only 且无外键，CASCADE truncate 不会清它。
	if _, err := pg.pool.Exec(ctx, `DELETE FROM strategy.strategy_promotions`); err != nil {
		t.Fatalf("clean promotions: %v", err)
	}
	base := time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)
	for i, outcome := range []string{"promoted", "rejected"} {
		ev := PromotionEvent{
			StrategyID: "strat-audit", VersionHash: "vh-audit", ArtifactHash: "ah-audit",
			Actor: "alice", Outcome: outcome, DecidedAt: base.Add(time.Duration(i) * time.Minute),
		}
		if err := pg.RecordPromotion(ctx, ev); err != nil {
			t.Fatalf("RecordPromotion(%s): %v", outcome, err)
		}
	}
	if n := promotionCount(t, pg); n != 2 {
		t.Fatalf("promotion rows = %d, want 2", n)
	}

	// 半截审计行必须拒绝，且不能多写一行。
	if err := pg.RecordPromotion(ctx, PromotionEvent{StrategyID: "s", Outcome: "promoted"}); !errors.Is(err, ErrInvalidParameter) {
		t.Fatalf("empty actor err = %v, want ErrInvalidParameter", err)
	}
	if err := pg.RecordPromotion(ctx, PromotionEvent{StrategyID: "s", Actor: "alice"}); !errors.Is(err, ErrInvalidParameter) {
		t.Fatalf("empty outcome err = %v, want ErrInvalidParameter", err)
	}
	if n := promotionCount(t, pg); n != 2 {
		t.Fatalf("promotion rows after rejected writes = %d, want 2", n)
	}
}

func TestPG_ActiveJobPicksEarliestOpenJob(t *testing.T) {
	pg := openTestPG(t)
	ctx := context.Background()
	if err := pg.CreateDefinition(ctx, pgDefinition("strat-active")); err != nil {
		t.Fatalf("CreateDefinition: %v", err)
	}
	v, err := pg.SaveVersion(ctx, pgVersion("strat-active", strings.Repeat("a", 64), "package strategy"))
	if err != nil {
		t.Fatalf("SaveVersion: %v", err)
	}
	base := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	mk := func(id string, at time.Time, state string) BuildJob {
		return BuildJob{ID: id, StrategyID: "strat-active", VersionHash: v.VersionHash,
			Toolchain: "tinygo-0.34.0", State: state, CreatedAt: at}
	}
	for _, job := range []BuildJob{
		mk("job-late", base.Add(time.Minute), JobQueued),
		mk("job-early", base, JobRunning),
		mk("job-done", base, JobSucceeded),
	} {
		if err := pg.SaveJob(ctx, job); err != nil {
			t.Fatalf("SaveJob(%s): %v", job.ID, err)
		}
	}

	got, ok, err := pg.ActiveJob(ctx, "strat-active", v.VersionHash, "tinygo-0.34.0")
	if err != nil {
		t.Fatalf("ActiveJob: %v", err)
	}
	if !ok || got.ID != "job-early" {
		t.Fatalf("ActiveJob = (%q, %v), want (job-early, true)", got.ID, ok)
	}

	// 最早那条落终态后，退到第二早的未终态作业；没有未终态行就不再命中。
	finished := base.Add(2 * time.Minute)
	if err := pg.UpdateJob(ctx, BuildJob{ID: "job-early", State: JobSucceeded, FinishedAt: &finished}); err != nil {
		t.Fatalf("UpdateJob(job-early): %v", err)
	}
	got, ok, err = pg.ActiveJob(ctx, "strat-active", v.VersionHash, "tinygo-0.34.0")
	if err != nil {
		t.Fatalf("ActiveJob after finish: %v", err)
	}
	if !ok || got.ID != "job-late" {
		t.Fatalf("ActiveJob after finish = (%q, %v), want (job-late, true)", got.ID, ok)
	}
	if err := pg.UpdateJob(ctx, BuildJob{ID: "job-late", State: JobFailed, ErrorCode: ErrorCodeCompileFailed, FinishedAt: &finished}); err != nil {
		t.Fatalf("UpdateJob(job-late): %v", err)
	}
	if got, ok, err := pg.ActiveJob(ctx, "strat-active", v.VersionHash, "tinygo-0.34.0"); err != nil || ok {
		t.Fatalf("ActiveJob with no open job = (%q, %v, %v), want not found", got.ID, ok, err)
	}
}

func TestPG_ListPromotionsReadsBackWhatWasWritten(t *testing.T) {
	pg := openTestPG(t)
	ctx := context.Background()
	if _, err := pg.pool.Exec(ctx, `DELETE FROM strategy.strategy_promotions`); err != nil {
		t.Fatalf("clean promotions: %v", err)
	}
	base := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	// 写入侧允许非 64-hex 的 hash（老数据/测试语料），读面必须原样读回。
	for i, outcome := range []string{"promoted", "rejected"} {
		ev := PromotionEvent{
			StrategyID: "strat-read", VersionHash: "vh-read-" + outcome, ArtifactHash: "ah-read",
			Actor: "alice", Outcome: outcome, DecidedAt: base.Add(time.Duration(i) * time.Minute),
		}
		if err := pg.RecordPromotion(ctx, ev); err != nil {
			t.Fatalf("RecordPromotion(%s): %v", outcome, err)
		}
	}

	all, err := pg.ListPromotions(ctx, PromotionFilter{StrategyID: "strat-read"})
	if err != nil {
		t.Fatalf("ListPromotions: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("rows = %d, want 2 (%+v)", len(all), all)
	}
	// 新的在前（append-only 按插入序倒排）。
	if all[0].Outcome != "rejected" || all[1].Outcome != "promoted" {
		t.Fatalf("order = [%s, %s], want [rejected, promoted]", all[0].Outcome, all[1].Outcome)
	}
	if all[0].Actor != "alice" || all[0].DecidedAt.IsZero() {
		t.Fatalf("row = %+v, want actor alice + non-zero decided_at", all[0])
	}

	one, err := pg.ListPromotions(ctx, PromotionFilter{StrategyID: "strat-read", VersionHash: "vh-read-promoted"})
	if err != nil {
		t.Fatalf("ListPromotions(filter): %v", err)
	}
	if len(one) != 1 || one[0].Outcome != "promoted" {
		t.Fatalf("filtered = %+v, want one promoted row", one)
	}

	paged, err := pg.ListPromotions(ctx, PromotionFilter{StrategyID: "strat-read", Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("ListPromotions(page): %v", err)
	}
	if len(paged) != 1 || paged[0].Outcome != "promoted" {
		t.Fatalf("paged = %+v, want [promoted]", paged)
	}

	empty, err := pg.ListPromotions(ctx, PromotionFilter{StrategyID: "strat-missing"})
	if err != nil {
		t.Fatalf("ListPromotions(empty): %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("missing strategy = %#v, want empty non-nil slice", empty)
	}
}

func promotionCount(t *testing.T, pg *PG) int {
	t.Helper()
	var n int
	if err := pg.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM strategy.strategy_promotions`).Scan(&n); err != nil {
		t.Fatalf("count promotions: %v", err)
	}
	return n
}

func getenv(t *testing.T, key string) string {
	t.Helper()
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		t.Skipf("%s is not set; skipping PostgreSQL integration tests", key)
	}
	return v
}
