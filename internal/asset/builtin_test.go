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
	second, err := svc.RegisterBuiltins(ctx)
	if err != nil {
		t.Fatalf("second registration must be idempotent: %v", err)
	}
	if second != 0 {
		t.Fatalf("second registration = %d, want 0 new rows", second)
	}
	for _, name := range BuiltinNames() {
		def, err := store.GetDefinition(ctx, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if def.Carrier != CarrierBuiltin {
			t.Fatalf("%s carrier = %s", name, def.Carrier)
		}
		if def.ActiveVersionHash != "" || def.ActiveArtifactHash != "" {
			t.Fatalf("%s must not have an active artifact: %+v", name, def)
		}
		versions, err := store.ListVersions(ctx, name)
		if err != nil || len(versions) != 0 {
			t.Fatalf("%s must not have versions yet: %v %v", name, versions, err)
		}
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
}
