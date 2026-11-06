package asset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestPGMigrationsAreReplayable 是 SVC-STRAT-G2 ① 的重放自证：同一测试库连跑两次
// Migrate()，列集 / 索引集 / 六张表行数逐位不变。
//
// 无 STRATEGY_TEST_DSN 时 skip——这条腿必须连真 PG，Memory 替代不了（重放的对象是
// information_schema / pg_indexes 的真实状态）。
func TestPGMigrationsAreReplayable(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("STRATEGY_TEST_DSN"))
	if dsn == "" {
		t.Skip("STRATEGY_TEST_DSN is not set: real-PG migration replay leg not executed")
	}
	ctx := context.Background()
	pg, err := OpenPG(ctx, dsn)
	if err != nil {
		t.Fatalf("OpenPG: %v", err)
	}
	defer pg.Close()

	if err := pg.Migrate(ctx); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	first, err := schemaFingerprint(ctx, pg)
	if err != nil {
		t.Fatalf("fingerprint after first migrate: %v", err)
	}

	if err := pg.Migrate(ctx); err != nil {
		t.Fatalf("second migrate (replay): %v", err)
	}
	second, err := schemaFingerprint(ctx, pg)
	if err != nil {
		t.Fatalf("fingerprint after second migrate: %v", err)
	}

	if first != second {
		t.Fatalf("schema fingerprint changed on replay:\nfirst  = %s\nsecond = %s", first, second)
	}
	t.Logf("schema fingerprint stable across replay: %s", first)
}

// schemaFingerprint 把 strategy schema 的列集、索引集与六张表行数压成一个 sha256。
func schemaFingerprint(ctx context.Context, pg *PG) (string, error) {
	var b strings.Builder

	rows, err := pg.pool.Query(ctx, `
		SELECT table_name, column_name, data_type, is_nullable,
		       COALESCE(character_maximum_length, 0), COALESCE(column_default, '')
		  FROM information_schema.columns
		 WHERE table_schema = 'strategy'
		 ORDER BY table_name, ordinal_position`)
	if err != nil {
		return "", fmt.Errorf("columns: %w", err)
	}
	for rows.Next() {
		var table, column, dataType, nullable, def string
		var maxLen int
		if err := rows.Scan(&table, &column, &dataType, &nullable, &maxLen, &def); err != nil {
			rows.Close()
			return "", err
		}
		fmt.Fprintf(&b, "col:%s.%s:%s:%s:%d:%s\n", table, column, dataType, nullable, maxLen, def)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", err
	}

	indexes, err := pg.pool.Query(ctx, `
		SELECT tablename, indexname, indexdef
		  FROM pg_indexes
		 WHERE schemaname = 'strategy'
		 ORDER BY tablename, indexname`)
	if err != nil {
		return "", fmt.Errorf("indexes: %w", err)
	}
	for indexes.Next() {
		var table, name, def string
		if err := indexes.Scan(&table, &name, &def); err != nil {
			indexes.Close()
			return "", err
		}
		fmt.Fprintf(&b, "idx:%s.%s:%s\n", table, name, def)
	}
	indexes.Close()
	if err := indexes.Err(); err != nil {
		return "", err
	}

	for _, table := range []string{
		"strategy_definitions",
		"strategy_versions",
		"strategy_artifacts",
		"strategy_artifact_blobs",
		"strategy_build_jobs",
		"strategy_promotions",
	} {
		var count int64
		if err := pg.pool.QueryRow(ctx,
			`SELECT count(*) FROM strategy.`+table).Scan(&count); err != nil {
			return "", fmt.Errorf("count %s: %w", table, err)
		}
		fmt.Fprintf(&b, "rows:%s:%d\n", table, count)
	}

	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:]), nil
}
