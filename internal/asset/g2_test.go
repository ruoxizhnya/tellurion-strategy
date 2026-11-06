package asset

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// 本仓允许写的 schema 只有 strategy（db-schema.md §1 / ADR-036 §5）。
var allowedWriteSchema = map[string]bool{"strategy": true}

// writeStatement 只认「写语句 + 带 schema 限定的表名」。
// 不带 schema 的写语句（依赖 search_path）同样危险，但它在本仓不存在；
// 出现时应先改代码再补一条正则，而不是让护栏默默放行。
var writeStatement = regexp.MustCompile(
	`(?is)\b(?:insert\s+into|update|delete\s+from|create\s+table|create\s+index|alter\s+table|drop\s+table|truncate\s+table)\s+(?:if\s+not\s+exists\s+)?([a-z_][a-z0-9_]*)\.`)

// TestNoCrossSchemaWrites 是 SVC-STRAT-G2 ② 的护栏：静态扫本仓全部 .go 文件，
// 出现写 `quant.` / `public.` / `market.` / `run.` 等非 strategy schema 的语句即红。
//
// 扫源码而不是扫运行时 SQL：本仓 SQL 全在常量/字面量里，静态扫能挡住「顺手加一句
// UPDATE 别的库」这种评审最容易漏的改动；运行时拼接反而会被漏掉——所以真正要守的
// 纪律是「不拼 SQL」，这条用例同时是它的看门人。
func TestNoCrossSchemaWrites(t *testing.T) {
	root := repoRoot(t)
	var violations []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", "dist", "deploy":
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		for _, m := range writeStatement.FindAllStringSubmatch(string(src), -1) {
			schema := strings.ToLower(m[1])
			if !allowedWriteSchema[schema] {
				violations = append(violations, rel+": "+strings.Join(strings.Fields(m[0]), " "))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
	for _, v := range violations {
		t.Errorf("cross-schema write: %s", v)
	}
}

// TestStrategyWritesAreFullyQualified 反向自证：本仓自己的写语句必须带 strategy.，
// 否则护栏扫不到它们（不带 schema = 依赖 search_path = 换库就写错地方）。
func TestStrategyWritesAreFullyQualified(t *testing.T) {
	root := repoRoot(t)
	unqualified := regexp.MustCompile(`(?is)\b(?:insert\s+into|update|delete\s+from)\s+([a-z_][a-z0-9_]*)\b`)
	allowed := map[string]bool{"strategy": true}
	var violations []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, m := range unqualified.FindAllStringSubmatch(string(src), -1) {
			target := strings.ToLower(m[1])
			if allowed[target] {
				continue // `INSERT INTO strategy.xxx`：m[1] 恰好是 schema
			}
			// 只关心「写本仓的表」：表名以 strategy_ 开头却没带 schema。
			if strings.HasPrefix(target, "strategy_") {
				violations = append(violations, rel+": "+strings.Join(strings.Fields(m[0]), " "))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
	for _, v := range violations {
		t.Errorf("unqualified strategy write: %s", v)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
