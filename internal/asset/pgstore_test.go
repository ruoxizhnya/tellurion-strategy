package asset

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	contracts "github.com/ruoxizhnya/tellurion-contracts/engine"
)

// PG 集成测试只在显式给出 DSN 时运行（CI 里没有库就该 skip，而不是假绿）。
// 例：STRATEGY_TEST_DSN=postgres://postgres:postgres@127.0.0.1:5432/tellurion_strategy_test?sslmode=disable
func openTestPG(t *testing.T) *PG {
	t.Helper()
	dsn := os.Getenv("STRATEGY_TEST_DSN")
	if strings.TrimSpace(dsn) == "" {
		t.Skip("STRATEGY_TEST_DSN is not set; skipping PostgreSQL integration tests")
	}
	ctx := context.Background()
	pg, err := OpenPG(ctx, dsn)
	if err != nil {
		t.Fatalf("OpenPG: %v", err)
	}
	t.Cleanup(pg.Close)
	if err := pg.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := pg.pool.Exec(ctx, `TRUNCATE strategy.strategy_definitions CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return pg
}

func pgDefinition(id string) Definition {
	return Definition{ID: id, Carrier: CarrierWASM, ABIVersion: "on_bar/2",
		Scope: contracts.ScopeMarket, Entry: "OnBar",
		Capability: Capability{Frequencies: []string{"1d"}, Markets: []string{"A"}},
		CreatedAt:  fixedNow(), UpdatedAt: fixedNow()}
}

func pgVersion(id, versionHash, source string) Version {
	return Version{StrategyID: id, VersionHash: versionHash, Source: source,
		Carrier: CarrierWASM, ABIVersion: "on_bar/2", Scope: contracts.ScopeMarket,
		Entry: "OnBar", CreatedAt: fixedNow()}
}

func pgArtifact(id, versionHash, toolchain string, bytes []byte) Artifact {
	return Artifact{StrategyID: id, VersionHash: versionHash, ArtifactHash: ArtifactHash(bytes),
		Carrier: CarrierWASM, ABIVersion: "on_bar/2", Toolchain: toolchain,
		SizeBytes: int64(len(bytes)), MemoryLimitBytes: 64 << 20, Status: StatusBuilt,
		Storage: StorageInline, Bytes: bytes, CreatedAt: fixedNow()}
}

func TestPG_MigrateIsIdempotent(t *testing.T) {
	pg := openTestPG(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	for _, table := range []string{
		"strategy_definitions", "strategy_versions",
		"strategy_artifacts", "strategy_artifact_blobs",
	} {
		var exists bool
		if err := pg.pool.QueryRow(ctx,
			`SELECT to_regclass('strategy.' || $1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("table strategy.%s was not created", table)
		}
	}
}

func TestPG_DefinitionsAndVersions(t *testing.T) {
	pg := openTestPG(t)
	ctx := context.Background()
	def := pgDefinition("s1")
	if err := pg.CreateDefinition(ctx, def); err != nil {
		t.Fatal(err)
	}
	if err := pg.CreateDefinition(ctx, def); !errors.Is(err, ErrStrategyExists) {
		t.Fatalf("got %v, want ErrStrategyExists", err)
	}
	got, err := pg.GetDefinition(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Carrier != CarrierWASM || got.Scope != contracts.ScopeMarket || got.ABIVersion != "on_bar/2" {
		t.Fatalf("round-trip lost fields: %+v", got)
	}
	if len(got.Capability.Frequencies) != 1 || got.Capability.Frequencies[0] != "1d" {
		t.Fatalf("capability did not round-trip: %+v", got.Capability)
	}
	if _, err := pg.GetDefinition(ctx, "missing"); !errors.Is(err, ErrStrategyNotFound) {
		t.Fatalf("got %v, want ErrStrategyNotFound", err)
	}
	if err := pg.SetActive(ctx, "s1", "abc", ""); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("half-set active must fail, got %v", err)
	}
	versionHash := strings.Repeat("a", 64)
	artifactHash := strings.Repeat("b", 64)
	if err := pg.SetActive(ctx, "s1", versionHash, artifactHash); err != nil {
		t.Fatal(err)
	}
	got, _ = pg.GetDefinition(ctx, "s1")
	// CHAR(64) 会补空格；读回必须是 trim 后的精确 hash，否则内容寻址比较全错。
	if got.ActiveVersionHash != versionHash || got.ActiveArtifactHash != artifactHash {
		t.Fatalf("active pointers = %q/%q (padded?)", got.ActiveVersionHash, got.ActiveArtifactHash)
	}

	v := pgVersion("s1", versionHash, "src")
	if _, err := pg.SaveVersion(ctx, v); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.SaveVersion(ctx, v); err != nil {
		t.Fatalf("same version must be idempotent: %v", err)
	}
	conflict := v
	conflict.Source = "other"
	if _, err := pg.SaveVersion(ctx, conflict); !errors.Is(err, ErrArtifactConflict) {
		t.Fatalf("got %v, want ErrArtifactConflict", err)
	}
	read, err := pg.GetVersion(ctx, "s1", versionHash)
	if err != nil || read.VersionHash != versionHash || read.Source != "src" {
		t.Fatalf("version round-trip: %+v %v", read, err)
	}
	list, err := pg.ListVersions(ctx, "s1")
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}
	if _, err := pg.SaveVersion(ctx, pgVersion("missing", versionHash, "src")); !errors.Is(err, ErrStrategyNotFound) {
		t.Fatalf("missing strategy must be ErrStrategyNotFound, got %v", err)
	}
}

func TestPG_ArtifactsKeyedByArtifactHash(t *testing.T) {
	pg := openTestPG(t)
	ctx := context.Background()
	if err := pg.CreateDefinition(ctx, pgDefinition("s1")); err != nil {
		t.Fatal(err)
	}
	versionHash := strings.Repeat("a", 64)
	if _, err := pg.SaveVersion(ctx, pgVersion("s1", versionHash, "src")); err != nil {
		t.Fatal(err)
	}

	b1 := wasmWithImports("env.get_bar2")
	b2 := wasmWithImports("env.get_bar2", "env.emit_target2")
	if _, err := pg.SaveArtifact(ctx, pgArtifact("s1", versionHash, "tinygo 0.34", b1)); err != nil {
		t.Fatal(err)
	}
	// 同源码 + 另一工具链 → 第二个 artifact 行（QT-41b ③-③）。
	if _, err := pg.SaveArtifact(ctx, pgArtifact("s1", versionHash, "go1.25 wasip1", b2)); err != nil {
		t.Fatalf("second toolchain must be allowed: %v", err)
	}
	// 同 (version, toolchain) 的第二个字节 → 冲突。
	if _, err := pg.SaveArtifact(ctx, pgArtifact("s1", versionHash, "tinygo 0.34", b2)); !errors.Is(err, ErrArtifactConflict) {
		t.Fatalf("got %v, want ErrArtifactConflict", err)
	}
	// 同 artifact_hash 重复登记是幂等。
	if _, err := pg.SaveArtifact(ctx, pgArtifact("s1", versionHash, "tinygo 0.34", b1)); err != nil {
		t.Fatalf("idempotent save failed: %v", err)
	}
	arts, err := pg.ArtifactsForVersion(ctx, "s1", versionHash)
	if err != nil || len(arts) != 2 {
		t.Fatalf("got %d artifacts (%v)", len(arts), err)
	}
	for _, a := range arts {
		if len(a.Bytes) != 0 {
			t.Fatalf("ArtifactsForVersion must not load blobs (%s has %d bytes)", a.Toolchain, len(a.Bytes))
		}
		if strings.TrimSpace(a.VersionHash) != versionHash || strings.TrimSpace(a.ArtifactHash) == "" {
			t.Fatalf("hashes must read back trimmed: %+v", a)
		}
	}
	full, err := pg.GetArtifact(ctx, "s1", ArtifactHash(b1))
	if err != nil {
		t.Fatal(err)
	}
	if string(full.Bytes) != string(b1) {
		t.Fatal("blob bytes changed in the store")
	}
	// 同字节换 toolchain：主键是 artifact_hash，不允许第二行。
	if _, err := pg.SaveArtifact(ctx, pgArtifact("s1", versionHash, "tc-second", b1)); !errors.Is(err, ErrArtifactConflict) {
		t.Fatalf("got %v, want ErrArtifactConflict", err)
	}
	// blob 超限 fail-loud。
	big := make([]byte, MaxArtifactBytes+1)
	if _, err := pg.SaveArtifact(ctx, pgArtifact("s1", versionHash, "tc-big", big)); !errors.Is(err, ErrArtifactTooLarge) {
		t.Fatalf("got %v, want ErrArtifactTooLarge", err)
	}
	// 版本不存在。
	if _, err := pg.SaveArtifact(ctx, pgArtifact("s1", strings.Repeat("f", 64), "tc", b1)); !errors.Is(err, ErrVersionNotFound) {
		t.Fatalf("got %v, want ErrVersionNotFound", err)
	}
}

func TestPG_StatusMachineAndTransactionalPromote(t *testing.T) {
	pg := openTestPG(t)
	ctx := context.Background()
	if err := pg.CreateDefinition(ctx, pgDefinition("s1")); err != nil {
		t.Fatal(err)
	}
	versionHash := strings.Repeat("a", 64)
	if _, err := pg.SaveVersion(ctx, pgVersion("s1", versionHash, "src")); err != nil {
		t.Fatal(err)
	}
	bytes := wasmWithImports("env.get_bar2")
	a := pgArtifact("s1", versionHash, "tc", bytes)
	if _, err := pg.SaveArtifact(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := pg.UpdateStatus(ctx, "s1", a.ArtifactHash, StatusValidated, StatusPromoted, ""); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("skipping validated must fail, got %v", err)
	}
	if err := pg.Promote(ctx, "s1", versionHash, a.ArtifactHash, "promoted"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("promote before validate must fail, got %v", err)
	}
	if err := pg.UpdateStatus(ctx, "s1", a.ArtifactHash, StatusBuilt, StatusValidated, "validated"); err != nil {
		t.Fatal(err)
	}

	// 破坏验证：第二步（写 active）用一个超过 CHAR(64) 的值强制失败；
	// 若两步不在同一事务，artifact 会停在 promoted 且指针为空。
	longHash := strings.Repeat("a", 65)
	if err := pg.Promote(ctx, "s1", longHash, a.ArtifactHash, "promoted"); err == nil {
		t.Fatal("promote with an over-long version hash must fail")
	}
	after, err := pg.GetArtifact(ctx, "s1", a.ArtifactHash)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != StatusValidated {
		t.Fatalf("status = %s after a failed promote; first write was not rolled back", after.Status)
	}
	def, _ := pg.GetDefinition(ctx, "s1")
	if def.ActiveArtifactHash != "" || def.ActiveVersionHash != "" {
		t.Fatalf("active pointers were written despite the failure: %+v", def)
	}

	if err := pg.Promote(ctx, "s1", versionHash, a.ArtifactHash, "promoted"); err != nil {
		t.Fatal(err)
	}
	def, _ = pg.GetDefinition(ctx, "s1")
	if def.ActiveVersionHash != versionHash || def.ActiveArtifactHash != a.ArtifactHash {
		t.Fatalf("promote did not activate: %+v", def)
	}
	after, _ = pg.GetArtifact(ctx, "s1", a.ArtifactHash)
	if after.Status != StatusPromoted || !strings.Contains(after.CompileLog, "promoted") {
		t.Fatalf("promote did not persist status/log: %+v", after)
	}

	if err := pg.Deprecate(ctx, "s1", versionHash, a.ArtifactHash, "deprecated"); err != nil {
		t.Fatal(err)
	}
	def, _ = pg.GetDefinition(ctx, "s1")
	if def.ActiveVersionHash != "" || def.ActiveArtifactHash != "" {
		t.Fatalf("deprecate did not clear the active pointers: %+v", def)
	}
	after, _ = pg.GetArtifact(ctx, "s1", a.ArtifactHash)
	if after.Status != StatusDeprecated {
		t.Fatalf("status = %s", after.Status)
	}
	if err := pg.UpdateStatus(ctx, "s1", a.ArtifactHash, StatusDeprecated, StatusPromoted, ""); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("deprecated is read-only, got %v", err)
	}
}

func TestPG_MissingBlobFailsClosed(t *testing.T) {
	pg := openTestPG(t)
	ctx := context.Background()
	if err := pg.CreateDefinition(ctx, pgDefinition("s1")); err != nil {
		t.Fatal(err)
	}
	versionHash := strings.Repeat("a", 64)
	if _, err := pg.SaveVersion(ctx, pgVersion("s1", versionHash, "src")); err != nil {
		t.Fatal(err)
	}
	bytes := wasmWithImports("env.get_bar2")
	a := pgArtifact("s1", versionHash, "tc", bytes)
	if _, err := pg.SaveArtifact(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.pool.Exec(ctx,
		`DELETE FROM strategy.strategy_artifact_blobs WHERE strategy_id = $1 AND artifact_hash = $2`,
		"s1", a.ArtifactHash); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.GetArtifact(ctx, "s1", a.ArtifactHash); !errors.Is(err, ErrArtifactFetchFailed) {
		t.Fatalf("got %v, want ErrArtifactFetchFailed", err)
	}
}

func TestPG_ServiceOnPG(t *testing.T) {
	pg := openTestPG(t)
	ctx := context.Background()
	compiler := &fakeCompiler{id: "tinygo 0.34", bytes: wasmWithImports("env.get_bar2"), log: "ok"}
	svc, err := New(Options{Store: pg, Compiler: compiler, IDGen: seqIDs(), Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	def := mustCreateWasm(t, svc, "on_bar/2", contracts.ScopeMarket)
	v := mustPutSource(t, svc, def.ID, "src")
	job, err := svc.SubmitBuild(ctx, def.ID, v.VersionHash, "")
	if err != nil {
		t.Fatal(err)
	}
	done := waitJob(t, svc, job.ID)
	if done.State != JobSucceeded {
		t.Fatalf("job failed: %+v", done)
	}
	if _, err := svc.ValidateVersion(ctx, def.ID, v.VersionHash, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Promote(ctx, def.ID, v.VersionHash, ""); err != nil {
		t.Fatal(err)
	}
	resolved, err := svc.Resolve(ctx, def.ID, "latest")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ArtifactHash != done.ArtifactHash || resolved.VersionHash != v.VersionHash {
		t.Fatalf("resolve on PG = %+v", resolved)
	}
	artifact, err := svc.ArtifactForVersion(ctx, def.ID, v.VersionHash, "")
	if err != nil {
		t.Fatal(err)
	}
	if string(artifact.Bytes) != string(compiler.bytes) {
		t.Fatal("artifact bytes changed through PG")
	}
	// 进程重启语义：新 Service 用同一 PG，仍能解析与取字节。
	svc2, err := New(Options{Store: pg, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc2.Resolve(ctx, def.ID, "latest")
	if err != nil || again.ArtifactHash != resolved.ArtifactHash {
		t.Fatalf("resolve after restart: %+v %v", again, err)
	}
	if _, err := svc2.Deprecate(ctx, def.ID, v.VersionHash, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc2.Resolve(ctx, def.ID, "latest"); !errors.Is(err, ErrNoActiveVersion) {
		t.Fatalf("latest must fail-closed after deprecate, got %v", err)
	}
}
