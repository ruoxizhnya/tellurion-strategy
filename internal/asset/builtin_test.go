package asset

import (
	"context"
	"errors"
	"testing"

	contracts "github.com/ruoxizhnya/tellurion-contracts/engine"
)

// 这 14 个名字是 G 裁决的登记范围（core `pkg/strategy/plugins/registration_test.go`）。
// 数量或名字变了必须同步改这里与 docs/TASKS.md，不允许「大概齐」。
func TestBuiltinCatalog_IsPinned(t *testing.T) {
	want := []string{
		"convertible_bond", "event_driven", "mean_reversion", "momentum",
		"multi_factor", "quality", "risk_parity", "sentiment",
		"td_sequential", "bollinger_mr", "volume_price_trend", "volatility_breakout",
		"value", "value_screening",
	}
	got := BuiltinNames()
	if len(got) != len(want) {
		t.Fatalf("builtin count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("builtin[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	catalog := BuiltinCatalog()
	if len(catalog) != 14 {
		t.Fatalf("catalog len = %d", len(catalog))
	}
	for _, def := range catalog {
		if def.Carrier != CarrierBuiltin {
			t.Fatalf("%s carrier = %s", def.ID, def.Carrier)
		}
		if def.ABIVersion != "" {
			t.Fatalf("%s must not declare abi_version (artifact-less carrier), got %q", def.ID, def.ABIVersion)
		}
		if def.Scope != contracts.ScopeMarket {
			t.Fatalf("%s scope = %s, want market", def.ID, def.Scope)
		}
		if def.Entry != def.ID {
			t.Fatalf("%s entry = %q, want the binary symbol name", def.ID, def.Entry)
		}
		if err := def.Validate(); err != nil {
			t.Fatalf("%s does not validate: %v", def.ID, err)
		}
	}
}

func TestService_RegisterBuiltinsIsIdempotent(t *testing.T) {
	ctx := context.Background()
	svc, store := newHarness(t, nil)
	first, err := svc.RegisterBuiltins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first != 14 {
		t.Fatalf("first registration = %d, want 14", first)
	}
	// F6 回归：出厂种子必须留下「版本 + active 指针」，否则 resolve?ref=latest
	// 永远 404 no_active_version（内置登记只建定义是缺陷形态）。
	firstHashes := make(map[string]string, len(BuiltinNames()))
	for _, name := range BuiltinNames() {
		def, err := store.GetDefinition(ctx, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if def.Carrier != CarrierBuiltin {
			t.Fatalf("%s carrier = %s", name, def.Carrier)
		}
		if def.ActiveVersionHash == "" || def.ActiveArtifactHash != "" {
			t.Fatalf("%s active = (%q, %q), want (version, \"\")", name,
				def.ActiveVersionHash, def.ActiveArtifactHash)
		}
		versions, err := store.ListVersions(ctx, name)
		if err != nil || len(versions) != 1 {
			t.Fatalf("%s versions = %v, %v; want the factory version", name, versions, err)
		}
		if versions[0].VersionHash != def.ActiveVersionHash {
			t.Fatalf("%s active %q is not the factory version %q", name,
				def.ActiveVersionHash, versions[0].VersionHash)
		}
		resolved, err := svc.Resolve(ctx, name, "latest")
		if err != nil {
			t.Fatalf("%s resolve latest: %v", name, err)
		}
		if resolved.Carrier != CarrierBuiltin || resolved.Entry != name ||
			resolved.ArtifactHash != "" || resolved.Status != StatusPromoted {
			t.Fatalf("%s resolved to %+v, want carrier=builtin entry=%s artifact=\"\" promoted",
				name, resolved, name)
		}
		firstHashes[name] = def.ActiveVersionHash
	}

	second, err := svc.RegisterBuiltins(ctx)
	if err != nil {
		t.Fatalf("second registration must be idempotent: %v", err)
	}
	if second != 0 {
		t.Fatalf("second registration = %d, want 0 new rows", second)
	}
	for name, want := range firstHashes {
		def, err := store.GetDefinition(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		if def.ActiveVersionHash != want {
			t.Fatalf("%s active hash changed across restarts: %q → %q", name, want, def.ActiveVersionHash)
		}
		versions, err := store.ListVersions(ctx, name)
		if err != nil || len(versions) != 1 {
			t.Fatalf("%s versions after restart = %v, %v; want no duplicate writes", name, versions, err)
		}
	}
}

// F6 的旧库形态：定义在、但从未登记版本与 active（旧 RegisterBuiltins 只建定义）。
// 种子必须补齐出厂激活，而不是因为「定义已存在」就跳过。
func TestService_RegisterBuiltinsCompletesLegacyDefinition(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	svc, err := New(Options{Store: store, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	for _, def := range BuiltinCatalog() {
		def.CreatedAt, def.UpdatedAt = fixedNow(), fixedNow()
		if err := store.CreateDefinition(ctx, def); err != nil {
			t.Fatal(err)
		}
	}
	seeded, err := svc.RegisterBuiltins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if seeded != 14 {
		t.Fatalf("legacy completion = %d, want 14", seeded)
	}
	resolved, err := svc.Resolve(ctx, "momentum", "latest")
	if err != nil {
		t.Fatalf("legacy builtin must resolve after seed: %v", err)
	}
	if resolved.Status != StatusPromoted || resolved.Carrier != CarrierBuiltin {
		t.Fatalf("resolved = %+v", resolved)
	}
}

// 出厂种子绝不覆盖人工改动：人工写源并 promote 了另一个版本后，重启不能把
// active 指针改回出厂版本，也不能删/改人工版本行。
func TestService_RegisterBuiltinsDoesNotOverwriteHumanPromotion(t *testing.T) {
	ctx := context.Background()
	svc, store := newHarness(t, nil)
	if _, err := svc.RegisterBuiltins(ctx); err != nil {
		t.Fatal(err)
	}
	human, err := svc.PutSource(ctx, "momentum", "momentum@builtin (ops override)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Promote(ctx, "momentum", human.VersionHash, ""); err != nil {
		t.Fatal(err)
	}

	seeded, err := svc.RegisterBuiltins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if seeded != 0 {
		t.Fatalf("restart seeded %d rows, want 0 (human changes must win)", seeded)
	}
	def, err := store.GetDefinition(ctx, "momentum")
	if err != nil {
		t.Fatal(err)
	}
	if def.ActiveVersionHash != human.VersionHash {
		t.Fatalf("active hash = %q, want the human-promoted %q", def.ActiveVersionHash, human.VersionHash)
	}
	versions, err := store.ListVersions(ctx, "momentum")
	if err != nil || len(versions) != 2 {
		t.Fatalf("versions = %v, %v; want the factory version + the human one", versions, err)
	}
}

// 出厂种子写审计：actor 固定 factory-seed，artifact_hash 为空；重启不重复写。
func TestService_RegisterBuiltinsRecordsFactoryAudit(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	log := NewMemoryPromotionLog()
	svc, err := New(Options{Store: store, PromotionLog: log, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RegisterBuiltins(ctx); err != nil {
		t.Fatal(err)
	}
	events := log.Events()
	if len(events) != 14 {
		t.Fatalf("audit rows = %d, want 14", len(events))
	}
	for _, ev := range events {
		if ev.Actor != FactorySeedActor || ev.Outcome != string(StatusPromoted) || ev.ArtifactHash != "" {
			t.Fatalf("factory audit row = %+v", ev)
		}
	}
	if _, err := svc.RegisterBuiltins(ctx); err != nil {
		t.Fatal(err)
	}
	if got := len(log.Events()); got != 14 {
		t.Fatalf("restart appended duplicate audit rows: %d", got)
	}
}

func TestService_RegisterBuiltinsRejectsShapeConflict(t *testing.T) {
	ctx := context.Background()
	svc, store := newHarness(t, nil)
	// 先把 momentum 占成 wasm：同名不同载体是命名空间冲突，必须 fail-loud，
	// 且不能覆盖已有的定义。
	squatter := Definition{ID: "momentum", Carrier: CarrierWASM, ABIVersion: "on_bar/2",
		Scope: contracts.ScopeMarket, Entry: "OnBar"}
	if err := store.CreateDefinition(ctx, squatter); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RegisterBuiltins(ctx); !errors.Is(err, ErrArtifactConflict) {
		t.Fatalf("got %v, want ErrArtifactConflict", err)
	}
	got, err := store.GetDefinition(ctx, "momentum")
	if err != nil {
		t.Fatal(err)
	}
	if got.Carrier != CarrierWASM || got.Entry != "OnBar" {
		t.Fatalf("the conflicting definition was overwritten: %+v", got)
	}
}

func TestPG_RegisterBuiltins(t *testing.T) {
	pg := openTestPG(t)
	ctx := context.Background()
	svc, err := New(Options{Store: pg})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.RegisterBuiltins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first != 14 {
		t.Fatalf("first registration = %d", first)
	}
	second, err := svc.RegisterBuiltins(ctx)
	if err != nil || second != 0 {
		t.Fatalf("second registration = %d, %v", second, err)
	}
	def, err := pg.GetDefinition(ctx, "risk_parity")
	if err != nil {
		t.Fatal(err)
	}
	if def.Carrier != CarrierBuiltin || def.Scope != contracts.ScopeMarket || def.Entry != "risk_parity" {
		t.Fatalf("builtin did not round-trip through PG: %+v", def)
	}
	// F6：PG 里也必须落下版本与 active 指针，resolve latest 才能解析。
	if def.ActiveVersionHash == "" || def.ActiveArtifactHash != "" {
		t.Fatalf("PG active = (%q, %q)", def.ActiveVersionHash, def.ActiveArtifactHash)
	}
	versions, err := pg.ListVersions(ctx, "risk_parity")
	if err != nil || len(versions) != 1 || versions[0].VersionHash != def.ActiveVersionHash {
		t.Fatalf("PG versions = %v, %v", versions, err)
	}
	resolved, err := svc.Resolve(ctx, "risk_parity", "latest")
	if err != nil {
		t.Fatalf("PG resolve latest: %v", err)
	}
	if resolved.Carrier != CarrierBuiltin || resolved.ArtifactHash != "" || resolved.Status != StatusPromoted {
		t.Fatalf("PG resolved = %+v", resolved)
	}
}

// PG 旧库形态：定义行已在但无版本 / active（F6 现场），种子必须补齐。
func TestPG_RegisterBuiltinsCompletesLegacyDefinition(t *testing.T) {
	pg := openTestPG(t)
	ctx := context.Background()
	for _, def := range BuiltinCatalog() {
		def.CreatedAt, def.UpdatedAt = fixedNow(), fixedNow()
		if err := pg.CreateDefinition(ctx, def); err != nil {
			t.Fatal(err)
		}
	}
	svc, err := New(Options{Store: pg})
	if err != nil {
		t.Fatal(err)
	}
	seeded, err := svc.RegisterBuiltins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if seeded != 14 {
		t.Fatalf("legacy completion = %d, want 14", seeded)
	}
	resolved, err := svc.Resolve(ctx, "momentum", "latest")
	if err != nil {
		t.Fatalf("legacy PG builtin must resolve after seed: %v", err)
	}
	if resolved.Entry != "momentum" || resolved.Status != StatusPromoted {
		t.Fatalf("resolved = %+v", resolved)
	}
	if got := resolved.ArtifactHash; got != "" {
		t.Fatalf("artifact_hash = %q, want empty for builtin", got)
	}
}
