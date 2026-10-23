package asset

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	contracts "github.com/ruoxizhnya/tellurion-contracts/engine"
)

// ─── 测试替身与语料 ────────────────────────────────────────────

// wasmWithImports 造一个最小的合法 wasm：magic + version + import section。
// 每个 import 写成 `module.field`。
func wasmWithImports(imports ...string) []byte {
	var payload []byte
	payload = appendULEB(payload, uint64(len(imports)))
	for _, imp := range imports {
		module, field, ok := strings.Cut(imp, ".")
		if !ok {
			module, field = imp, "fn"
		}
		payload = appendName(payload, module)
		payload = appendName(payload, field)
		payload = append(payload, 0x00)  // kind: func
		payload = appendULEB(payload, 0) // typeidx
	}
	out := []byte("\x00asm")
	var version [4]byte
	binary.LittleEndian.PutUint32(version[:], 1)
	out = append(out, version[:]...)
	out = append(out, wasmImportSectionID)
	out = appendULEB(out, uint64(len(payload)))
	return append(out, payload...)
}

func appendULEB(dst []byte, v uint64) []byte {
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		dst = append(dst, b)
		if v == 0 {
			return dst
		}
	}
}

func appendName(dst []byte, s string) []byte {
	dst = appendULEB(dst, uint64(len(s)))
	return append(dst, s...)
}

type fakeCompiler struct {
	id      string
	bytes   []byte
	imports []string
	log     string
	err     error
	delay   time.Duration
	calls   int
}

func (f *fakeCompiler) ToolchainID() string { return f.id }

func (f *fakeCompiler) Compile(ctx context.Context, req CompileRequest) (CompileResult, error) {
	f.calls++
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return CompileResult{}, ctx.Err()
		}
	}
	if f.err != nil {
		return CompileResult{}, f.err
	}
	return CompileResult{Bytes: append([]byte(nil), f.bytes...), Imports: f.imports, Log: f.log}, nil
}

func seqIDs() func() string {
	n := 0
	return func() string {
		n++
		return fmt.Sprintf("st_%04d", n)
	}
}

func fixedNow() time.Time { return time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC) }

func newHarness(t *testing.T, compiler Compiler) (*Service, *Memory) {
	t.Helper()
	store := NewMemory()
	svc, err := New(Options{
		Store:    store,
		Compiler: compiler,
		IDGen:    seqIDs(),
		Now:      fixedNow,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return svc, store
}

func mustCreateWasm(t *testing.T, svc *Service, abi string, scope contracts.StrategyScope) Definition {
	t.Helper()
	def, err := svc.CreateDefinition(context.Background(), CreateDefinitionRequest{
		Carrier:    CarrierWASM,
		ABIVersion: abi,
		Scope:      scope,
		Capability: Capability{Frequencies: []string{"1d"}, Markets: []string{"A"}},
		Entry:      "OnBar",
	})
	if err != nil {
		t.Fatalf("CreateDefinition: %v", err)
	}
	return def
}

func mustPutSource(t *testing.T, svc *Service, id, source string) Version {
	t.Helper()
	v, err := svc.PutSource(context.Background(), id, source)
	if err != nil {
		t.Fatalf("PutSource: %v", err)
	}
	return v
}

func waitJob(t *testing.T, svc *Service, jobID string) BuildJob {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := svc.Job(context.Background(), jobID)
		if err != nil {
			t.Fatalf("Job: %v", err)
		}
		if job.State == JobSucceeded || job.State == JobFailed {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s did not finish", jobID)
	return BuildJob{}
}

func mustPromote(t *testing.T, svc *Service, id, versionHash string) Artifact {
	t.Helper()
	ctx := context.Background()
	if _, err := svc.ValidateVersion(ctx, id, versionHash, ""); err != nil {
		t.Fatalf("ValidateVersion: %v", err)
	}
	a, err := svc.Promote(ctx, id, versionHash, "")
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	return a
}

// ─── version_hash ──────────────────────────────────────────────

func TestVersionHash_IsDeterministicAndFieldSensitive(t *testing.T) {
	base := VersionInput{
		StrategyID: "s1", Carrier: CarrierWASM, ABIVersion: "on_bar/2",
		Scope: contracts.ScopeMarket, Entry: "OnBar", Source: "package strategy",
	}
	h1, err := VersionHash(base)
	if err != nil {
		t.Fatal(err)
	}
	h2, _ := VersionHash(base)
	if h1 != h2 {
		t.Fatalf("same input produced %s and %s", h1, h2)
	}
	if !isLowerHex64(h1) {
		t.Fatalf("hash %q is not 64 lowercase hex", h1)
	}

	mutations := map[string]func(*VersionInput){
		"strategy_id": func(in *VersionInput) { in.StrategyID = "s2" },
		"carrier":     func(in *VersionInput) { in.Carrier = CarrierSO },
		"abi_version": func(in *VersionInput) { in.ABIVersion = "on_bar/1" },
		"scope":       func(in *VersionInput) { in.Scope = contracts.ScopeSymbol },
		"entry":       func(in *VersionInput) { in.Entry = "OnBar2" },
		"source":      func(in *VersionInput) { in.Source = "package strategy // x" },
	}
	for name, mutate := range mutations {
		changed := base
		mutate(&changed)
		other, err := VersionHash(changed)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if other == h1 {
			t.Fatalf("%s did not change the version hash", name)
		}
	}
}

func TestVersionHash_RejectsBadInput(t *testing.T) {
	base := VersionInput{
		StrategyID: "s1", Carrier: CarrierWASM, ABIVersion: "on_bar/1",
		Scope: contracts.ScopeSymbol, Entry: "OnBar", Source: "x",
	}
	cases := []struct {
		name   string
		mutate func(*VersionInput)
		want   error
	}{
		{"empty id", func(in *VersionInput) { in.StrategyID = " " }, ErrInvalidDefinition},
		{"unknown carrier", func(in *VersionInput) { in.Carrier = "rust" }, ErrUnsupportedCarrier},
		{"unknown scope", func(in *VersionInput) { in.Scope = "run" }, ErrInvalidScope},
		{"empty entry", func(in *VersionInput) { in.Entry = "" }, ErrInvalidDefinition},
		{"empty source", func(in *VersionInput) { in.Source = "  " }, ErrSourceMissing},
		{"source too large", func(in *VersionInput) { in.Source = strings.Repeat("x", int(MaxSourceBytes)+1) }, ErrSourceTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			tc.mutate(&in)
			_, err := VersionHash(in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

// ─── 定义校验 ──────────────────────────────────────────────────

func TestDefinition_Validate(t *testing.T) {
	valid := Definition{
		ID: "s1", Carrier: CarrierWASM, ABIVersion: "on_bar/2",
		Scope: contracts.ScopeMarket, Entry: "OnBar",
	}
	cases := []struct {
		name   string
		mutate func(*Definition)
		want   error
	}{
		{"valid wasm v2 market", func(*Definition) {}, nil},
		{"valid wasm v1 symbol", func(d *Definition) { d.ABIVersion = "on_bar/1"; d.Scope = contracts.ScopeSymbol }, nil},
		{"valid dsl without abi", func(d *Definition) { d.Carrier = CarrierDSL; d.ABIVersion = "" }, nil},
		{"valid builtin without abi", func(d *Definition) { d.Carrier = CarrierBuiltin; d.ABIVersion = "" }, nil},
		{"missing id", func(d *Definition) { d.ID = "" }, ErrInvalidDefinition},
		{"unknown carrier", func(d *Definition) { d.Carrier = "rust" }, ErrUnsupportedCarrier},
		{"missing scope", func(d *Definition) { d.Scope = "" }, ErrInvalidScope},
		{"missing entry", func(d *Definition) { d.Entry = " " }, ErrInvalidDefinition},
		{"wasm without abi", func(d *Definition) { d.ABIVersion = "" }, ErrInvalidABIVersion},
		{"wasm with dsl/2 abi", func(d *Definition) { d.ABIVersion = "dsl/2" }, ErrInvalidABIVersion},
		{"v1 with market scope", func(d *Definition) { d.ABIVersion = "on_bar/1" }, ErrInvalidScope},
		{"dsl with abi", func(d *Definition) { d.Carrier = CarrierDSL }, ErrInvalidABIVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := valid
			tc.mutate(&d)
			err := d.Validate()
			if tc.want == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

// ─── import 白名单与 wasm 解析 ────────────────────────────────

func TestImportPolicyFor_IsDerivedFromContracts(t *testing.T) {
	v1, err := ImportPolicyFor("on_bar/1")
	if err != nil {
		t.Fatal(err)
	}
	if err := v1.Validate([]string{"env.get_bar", "get_state"}); err != nil {
		t.Fatalf("v1 rejected an allowed import: %v", err)
	}
	if err := v1.Validate([]string{"get_bar2"}); !errors.Is(err, contracts.ErrImportNotWhitelisted) {
		t.Fatalf("v1 must reject v2-only import, got %v", err)
	}
	v2, err := ImportPolicyFor("on_bar/2")
	if err != nil {
		t.Fatal(err)
	}
	if err := v2.Validate([]string{"env.get_bar2", "emit_target2"}); err != nil {
		t.Fatalf("v2 rejected an allowed import: %v", err)
	}
	if err := v2.Validate([]string{"env.malloc"}); !errors.Is(err, contracts.ErrImportNotWhitelisted) {
		t.Fatalf("v2 must reject unknown import, got %v", err)
	}
	if _, err := ImportPolicyFor("go-plugin/1"); err == nil {
		t.Fatal("unknown abi must fail-closed")
	}
}

func TestWasmImports_ParsesImportSection(t *testing.T) {
	b := wasmWithImports("env.get_bar", "env.emit_signal", "wasi_snapshot_preview1.fd_write")
	got, err := WasmImports(b)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"env.emit_signal", "env.get_bar", "wasi_snapshot_preview1.fd_write"}
	if !sameStringSet(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	empty, err := WasmImports(wasmWithImports())
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty import section: %v %v", empty, err)
	}
}

func TestWasmImports_FailsClosedOnMalformedBytes(t *testing.T) {
	cases := map[string][]byte{
		"empty":        nil,
		"bad magic":    []byte("NOPE\x01\x00\x00\x00"),
		"bad version":  append(append([]byte(nil), wasmWithImports()...)[:4], 9, 0, 0, 0, 2, 0),
		"truncated":    []byte("\x00asm\x01\x00\x00\x00\x02\xff"),
		"unknown kind": func() []byte { b := wasmWithImports("env.get_bar"); b[len(b)-2] = 0x09; return b }(),
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := WasmImports(b); !errors.Is(err, ErrInvalidArtifact) {
				t.Fatalf("got %v, want ErrInvalidArtifact", err)
			}
		})
	}
}

// ─── Verify 门 ─────────────────────────────────────────────────

func TestVerify_AcceptsValidArtifact(t *testing.T) {
	bytes := wasmWithImports("env.get_bar", "env.emit_signal")
	a := Artifact{
		StrategyID: "s1", VersionHash: strings.Repeat("a", 64), ArtifactHash: ArtifactHash(bytes),
		Carrier: CarrierWASM, ABIVersion: "on_bar/1", Toolchain: "tinygo 0.34",
		SizeBytes: int64(len(bytes)), MemoryLimitBytes: MaxMemoryLimitBytes,
		Imports: []string{"env.get_bar", "env.emit_signal"}, Status: StatusBuilt,
		Storage: StorageInline, Bytes: bytes,
	}
	if err := Verify(a, "OnBar"); err != nil {
		t.Fatalf("valid artifact rejected: %v", err)
	}
}

func TestVerify_FailsClosed(t *testing.T) {
	bytes := wasmWithImports("env.get_bar")
	base := Artifact{
		StrategyID: "s1", VersionHash: strings.Repeat("a", 64), ArtifactHash: ArtifactHash(bytes),
		Carrier: CarrierWASM, ABIVersion: "on_bar/1", Toolchain: "tinygo 0.34",
		SizeBytes: int64(len(bytes)), MemoryLimitBytes: MaxMemoryLimitBytes,
		Imports: []string{"env.get_bar"}, Status: StatusBuilt, Storage: StorageInline, Bytes: bytes,
	}
	cases := []struct {
		name   string
		mutate func(*Artifact)
		want   error
	}{
		{"hash mismatch", func(a *Artifact) { a.ArtifactHash = strings.Repeat("b", 64) }, ErrArtifactHashMismatch},
		{"size mismatch", func(a *Artifact) { a.SizeBytes = 1 }, ErrInvalidArtifact},
		{"no bytes", func(a *Artifact) { a.Bytes = nil }, ErrArtifactFetchFailed},
		{"storage url", func(a *Artifact) { a.Storage = StorageURL }, ErrStorageUnsupported},
		{"bad abi", func(a *Artifact) { a.ABIVersion = "dsl/2" }, ErrInvalidABIVersion},
		{"zero memory", func(a *Artifact) { a.MemoryLimitBytes = 0 }, ErrInvalidArtifact},
		{"memory over cap", func(a *Artifact) { a.MemoryLimitBytes = MaxMemoryLimitBytes + 1 }, ErrInvalidArtifact},
		{"declared imports lie", func(a *Artifact) { a.Imports = []string{"env.emit_signal"} }, ErrInvalidArtifact},
		{"not wasm", func(a *Artifact) {
			a.Bytes = []byte("plain text")
			a.ArtifactHash = ArtifactHash(a.Bytes)
			a.SizeBytes = int64(len(a.Bytes))
		}, ErrInvalidArtifact},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := base
			a.Imports = append([]string(nil), base.Imports...)
			a.Bytes = append([]byte(nil), base.Bytes...)
			tc.mutate(&a)
			if err := Verify(a, "OnBar"); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}

	t.Run("import not whitelisted", func(t *testing.T) {
		bad := wasmWithImports("env.fd_write")
		a := base
		a.Bytes = bad
		a.ArtifactHash = ArtifactHash(bad)
		a.SizeBytes = int64(len(bad))
		a.Imports = nil
		if err := Verify(a, "OnBar"); !errors.Is(err, contracts.ErrImportNotWhitelisted) {
			t.Fatalf("got %v, want ErrImportNotWhitelisted", err)
		}
	})

	t.Run("too large", func(t *testing.T) {
		big := make([]byte, MaxArtifactBytes+1)
		copy(big, "\x00asm")
		a := base
		a.Bytes = big
		a.SizeBytes = int64(len(big))
		a.ArtifactHash = ArtifactHash(big)
		if err := Verify(a, "OnBar"); !errors.Is(err, ErrArtifactTooLarge) {
			t.Fatalf("got %v, want ErrArtifactTooLarge", err)
		}
	})
}

// ─── Store ─────────────────────────────────────────────────────

func TestMemoryStore_Definitions(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	def := Definition{ID: "s1", Carrier: CarrierWASM, ABIVersion: "on_bar/2",
		Scope: contracts.ScopeMarket, Entry: "OnBar", Capability: Capability{Markets: []string{"A"}}}
	if err := store.CreateDefinition(ctx, def); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateDefinition(ctx, def); !errors.Is(err, ErrStrategyExists) {
		t.Fatalf("got %v, want ErrStrategyExists", err)
	}
	got, err := store.GetDefinition(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	got.Capability.Markets[0] = "HK" // 出参必须是副本
	again, _ := store.GetDefinition(ctx, "s1")
	if again.Capability.Markets[0] != "A" {
		t.Fatal("GetDefinition leaked its internal slice")
	}
	if _, err := store.GetDefinition(ctx, "nope"); !errors.Is(err, ErrStrategyNotFound) {
		t.Fatalf("got %v, want ErrStrategyNotFound", err)
	}
	if err := store.SetActive(ctx, "s1", "abc", ""); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("half-set active pointer must fail, got %v", err)
	}
	if err := store.SetActive(ctx, "s1", strings.Repeat("a", 64), strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetDefinition(ctx, "s1")
	if got.ActiveVersionHash == "" || got.ActiveArtifactHash == "" {
		t.Fatal("active pointers were not persisted")
	}
}

func TestMemoryStore_VersionsAreContentAddressed(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	def := Definition{ID: "s1", Carrier: CarrierWASM, ABIVersion: "on_bar/1",
		Scope: contracts.ScopeSymbol, Entry: "OnBar"}
	if err := store.CreateDefinition(ctx, def); err != nil {
		t.Fatal(err)
	}
	v := Version{StrategyID: "s1", VersionHash: strings.Repeat("a", 64), Source: "src",
		Carrier: CarrierWASM, ABIVersion: "on_bar/1", Scope: contracts.ScopeSymbol, Entry: "OnBar"}
	if _, err := store.SaveVersion(ctx, v); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveVersion(ctx, v); err != nil {
		t.Fatalf("same version must be idempotent: %v", err)
	}
	conflict := v
	conflict.Source = "other"
	if _, err := store.SaveVersion(ctx, conflict); !errors.Is(err, ErrArtifactConflict) {
		t.Fatalf("got %v, want ErrArtifactConflict", err)
	}
	if _, err := store.GetVersion(ctx, "s1", strings.Repeat("c", 64)); !errors.Is(err, ErrVersionNotFound) {
		t.Fatalf("got %v, want ErrVersionNotFound", err)
	}
	list, err := store.ListVersions(ctx, "s1")
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}
}

func TestMemoryStore_ArtifactsKeyedByArtifactHash(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	def := Definition{ID: "s1", Carrier: CarrierWASM, ABIVersion: "on_bar/2",
		Scope: contracts.ScopeMarket, Entry: "OnBar"}
	_ = store.CreateDefinition(ctx, def)
	versionHash := strings.Repeat("a", 64)
	_, _ = store.SaveVersion(ctx, Version{StrategyID: "s1", VersionHash: versionHash, Source: "src",
		Carrier: CarrierWASM, ABIVersion: "on_bar/2", Scope: contracts.ScopeMarket, Entry: "OnBar"})

	b1 := wasmWithImports("env.get_bar2")
	b2 := wasmWithImports("env.get_bar2", "env.emit_target2")
	mk := func(toolchain string, bytes []byte) Artifact {
		return Artifact{StrategyID: "s1", VersionHash: versionHash, ArtifactHash: ArtifactHash(bytes),
			Carrier: CarrierWASM, ABIVersion: "on_bar/2", Toolchain: toolchain,
			SizeBytes: int64(len(bytes)), MemoryLimitBytes: 64 << 20, Status: StatusBuilt,
			Storage: StorageInline, Bytes: bytes}
	}
	if _, err := store.SaveArtifact(ctx, mk("tinygo 0.34", b1)); err != nil {
		t.Fatal(err)
	}
	// 同源码 + 另一工具链 → 第二个 artifact 行（QT-41b ③-③）。
	if _, err := store.SaveArtifact(ctx, mk("go1.25 wasip1", b2)); err != nil {
		t.Fatalf("second toolchain must be allowed: %v", err)
	}
	arts, err := store.ArtifactsForVersion(ctx, "s1", versionHash)
	if err != nil || len(arts) != 2 {
		t.Fatalf("got %d artifacts (%v)", len(arts), err)
	}
	// 同 (version, toolchain) 的第二个字节 → 冲突。
	third := mk("tinygo 0.34", b2)
	if _, err := store.SaveArtifact(ctx, third); !errors.Is(err, ErrArtifactConflict) {
		t.Fatalf("got %v, want ErrArtifactConflict", err)
	}
	// 同 artifact_hash 重复登记是幂等。
	if _, err := store.SaveArtifact(ctx, mk("tinygo 0.34", b1)); err != nil {
		t.Fatalf("idempotent save failed: %v", err)
	}
	if got, err := store.GetArtifact(ctx, "s1", ArtifactHash(b1)); err != nil || got.ArtifactHash != ArtifactHash(b1) {
		t.Fatalf("get: %v %v", got, err)
	}
	// blob 超限 fail-loud。
	big := make([]byte, MaxArtifactBytes+1)
	if _, err := store.SaveArtifact(ctx, mk("tinygo 0.34", big)); !errors.Is(err, ErrArtifactTooLarge) {
		t.Fatalf("got %v, want ErrArtifactTooLarge", err)
	}
}

func TestMemoryStore_StatusMachine(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	def := Definition{ID: "s1", Carrier: CarrierWASM, ABIVersion: "on_bar/1",
		Scope: contracts.ScopeSymbol, Entry: "OnBar"}
	_ = store.CreateDefinition(ctx, def)
	versionHash := strings.Repeat("a", 64)
	_, _ = store.SaveVersion(ctx, Version{StrategyID: "s1", VersionHash: versionHash, Source: "src",
		Carrier: CarrierWASM, ABIVersion: "on_bar/1", Scope: contracts.ScopeSymbol, Entry: "OnBar"})
	bytes := wasmWithImports("env.get_bar")
	a := Artifact{StrategyID: "s1", VersionHash: versionHash, ArtifactHash: ArtifactHash(bytes),
		Carrier: CarrierWASM, ABIVersion: "on_bar/1", Toolchain: "tc", SizeBytes: int64(len(bytes)),
		MemoryLimitBytes: 64 << 20, Status: StatusBuilt, Storage: StorageInline, Bytes: bytes}
	if _, err := store.SaveArtifact(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateStatus(ctx, "s1", a.ArtifactHash, StatusValidated, StatusPromoted, ""); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("skipping validated must fail, got %v", err)
	}
	if err := store.UpdateStatus(ctx, "s1", a.ArtifactHash, StatusBuilt, StatusValidated, "ok"); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateStatus(ctx, "s1", a.ArtifactHash, StatusValidated, StatusPromoted, "ok"); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateStatus(ctx, "s1", a.ArtifactHash, StatusPromoted, StatusDeprecated, "bye"); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateStatus(ctx, "s1", a.ArtifactHash, StatusDeprecated, StatusPromoted, "no"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("deprecated is read-only, got %v", err)
	}
	got, _ := store.GetArtifact(ctx, "s1", a.ArtifactHash)
	if got.Status != StatusDeprecated || !strings.Contains(got.CompileLog, "bye") {
		t.Fatalf("status/log not persisted: %+v", got)
	}
}

// ─── 服务：主链路与 fail-closed ───────────────────────────────

func TestService_HappyPathLatestResolveAndDeprecate(t *testing.T) {
	ctx := context.Background()
	compiler := &fakeCompiler{id: "tinygo 0.34", bytes: wasmWithImports("env.get_bar2"), log: "ok"}
	svc, _ := newHarness(t, compiler)
	def := mustCreateWasm(t, svc, "on_bar/2", contracts.ScopeMarket)
	version := mustPutSource(t, svc, def.ID, "package strategy; func OnBar() {}")

	// 同源码重复上传 → 同 hash、幂等。
	if again := mustPutSource(t, svc, def.ID, version.Source); again.VersionHash != version.VersionHash {
		t.Fatalf("same source produced %s and %s", version.VersionHash, again.VersionHash)
	}

	job, err := svc.SubmitBuild(ctx, def.ID, version.VersionHash, "")
	if err != nil {
		t.Fatal(err)
	}
	done := waitJob(t, svc, job.ID)
	if done.State != JobSucceeded {
		t.Fatalf("build failed: %+v", done)
	}
	if done.ArtifactHash == "" || compiler.calls != 1 {
		t.Fatalf("job=%+v calls=%d", done, compiler.calls)
	}

	promoted := mustPromote(t, svc, def.ID, version.VersionHash)
	if promoted.Status != StatusPromoted {
		t.Fatalf("status %s", promoted.Status)
	}

	resolved, err := svc.Resolve(ctx, def.ID, "latest")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ArtifactHash != promoted.ArtifactHash || resolved.VersionHash != version.VersionHash {
		t.Fatalf("resolve returned %+v", resolved)
	}
	if resolved.Scope != contracts.ScopeMarket || resolved.Carrier != CarrierWASM {
		t.Fatalf("resolve lost definition metadata: %+v", resolved)
	}
	if len(resolved.Imports) != 1 || resolved.Imports[0] != "env.get_bar2" {
		t.Fatalf("imports must come from bytes, got %v", resolved.Imports)
	}

	// 空 ref 与 latest 等价。
	empty, err := svc.Resolve(ctx, def.ID, "")
	if err != nil || empty.ArtifactHash != promoted.ArtifactHash {
		t.Fatalf("empty ref: %+v %v", empty, err)
	}

	bytesOut, err := svc.ArtifactForVersion(ctx, def.ID, version.VersionHash, "")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytesOut.Bytes, compiler.bytes) {
		t.Fatal("artifact bytes changed in transit")
	}

	if _, err := svc.Deprecate(ctx, def.ID, version.VersionHash, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, def.ID, "latest"); !errors.Is(err, ErrNoActiveVersion) {
		t.Fatalf("latest must fail-closed after deprecate, got %v", err)
	}
	// 历史 hash 仍可解析（验收：历史回测可复现）。
	if _, err := svc.Resolve(ctx, def.ID, promoted.ArtifactHash); err != nil {
		t.Fatalf("deprecated artifact must stay resolvable by hash: %v", err)
	}
	if _, err := svc.ArtifactForVersion(ctx, def.ID, version.VersionHash, ""); err != nil {
		t.Fatalf("deprecated artifact must stay downloadable: %v", err)
	}
}

func TestService_ResolveWritesBackArtifactHashForVersionRef(t *testing.T) {
	ctx := context.Background()
	compiler := &fakeCompiler{id: "tinygo 0.34", bytes: wasmWithImports("env.get_bar2")}
	svc, _ := newHarness(t, compiler)
	def := mustCreateWasm(t, svc, "on_bar/2", contracts.ScopeMarket)
	v := mustPutSource(t, svc, def.ID, "src")
	job, _ := svc.SubmitBuild(ctx, def.ID, v.VersionHash, "")
	waitJob(t, svc, job.ID)
	promoted := mustPromote(t, svc, def.ID, v.VersionHash)

	got, err := svc.Resolve(ctx, def.ID, v.VersionHash)
	if err != nil {
		t.Fatal(err)
	}
	if got.VersionHash != v.VersionHash || got.ArtifactHash != promoted.ArtifactHash {
		t.Fatalf("version ref must write back both hashes: %+v", got)
	}
}

func TestService_ResolveFailsClosed(t *testing.T) {
	ctx := context.Background()
	compiler := &fakeCompiler{id: "tinygo 0.34", bytes: wasmWithImports("env.get_bar2")}
	svc, _ := newHarness(t, compiler)
	def := mustCreateWasm(t, svc, "on_bar/2", contracts.ScopeMarket)
	v := mustPutSource(t, svc, def.ID, "src")

	if _, err := svc.Resolve(ctx, def.ID, "latest"); !errors.Is(err, ErrNoActiveVersion) {
		t.Fatalf("no active version must fail-closed, got %v", err)
	}
	if _, err := svc.Resolve(ctx, def.ID, strings.Repeat("f", 64)); !errors.Is(err, ErrVersionNotFound) {
		t.Fatalf("unknown hash must be version_not_found, got %v", err)
	}
	if _, err := svc.Resolve(ctx, def.ID, "other@abc"); err == nil {
		t.Fatal("ref with mismatched id must fail")
	}
	if _, err := svc.Resolve(ctx, def.ID, "@"); !errors.Is(err, ErrInvalidParameter) {
		t.Fatalf("malformed ref must be invalid_parameter, got %v", err)
	}
	if _, err := svc.Resolve(ctx, "missing", "latest"); !errors.Is(err, ErrStrategyNotFound) {
		t.Fatalf("got %v, want ErrStrategyNotFound", err)
	}

	job, _ := svc.SubmitBuild(ctx, def.ID, v.VersionHash, "")
	waitJob(t, svc, job.ID)
	if _, err := svc.Resolve(ctx, def.ID, v.VersionHash); !errors.Is(err, ErrNoActiveVersion) {
		t.Fatalf("built-but-not-promoted version must not resolve, got %v", err)
	}
}

func TestService_BuildFailClosed(t *testing.T) {
	ctx := context.Background()

	t.Run("no compiler", func(t *testing.T) {
		svc, _ := newHarness(t, nil)
		def := mustCreateWasm(t, svc, "on_bar/2", contracts.ScopeMarket)
		v := mustPutSource(t, svc, def.ID, "src")
		if _, err := svc.SubmitBuild(ctx, def.ID, v.VersionHash, ""); !errors.Is(err, ErrCompileUnavailable) {
			t.Fatalf("got %v, want ErrCompileUnavailable", err)
		}
	})

	t.Run("toolchain mismatch", func(t *testing.T) {
		svc, _ := newHarness(t, &fakeCompiler{id: "tinygo 0.34", bytes: wasmWithImports("env.get_bar2")})
		def := mustCreateWasm(t, svc, "on_bar/2", contracts.ScopeMarket)
		v := mustPutSource(t, svc, def.ID, "src")
		if _, err := svc.SubmitBuild(ctx, def.ID, v.VersionHash, "go1.25"); !errors.Is(err, ErrUnsupportedToolchain) {
			t.Fatalf("got %v, want ErrUnsupportedToolchain", err)
		}
	})

	t.Run("builtin cannot build", func(t *testing.T) {
		svc, _ := newHarness(t, &fakeCompiler{id: "tinygo 0.34"})
		def, err := svc.CreateDefinition(ctx, CreateDefinitionRequest{
			Carrier: CarrierBuiltin, Scope: contracts.ScopeSymbol, Entry: "Strategy"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SubmitBuild(ctx, def.ID, strings.Repeat("a", 64), ""); !errors.Is(err, ErrUnsupportedCarrier) {
			t.Fatalf("got %v, want ErrUnsupportedCarrier", err)
		}
	})

	t.Run("unknown version", func(t *testing.T) {
		svc, _ := newHarness(t, &fakeCompiler{id: "tinygo 0.34"})
		def := mustCreateWasm(t, svc, "on_bar/2", contracts.ScopeMarket)
		if _, err := svc.SubmitBuild(ctx, def.ID, strings.Repeat("a", 64), ""); !errors.Is(err, ErrVersionNotFound) {
			t.Fatalf("got %v, want ErrVersionNotFound", err)
		}
	})

	t.Run("compile error is retrievable", func(t *testing.T) {
		svc, _ := newHarness(t, &fakeCompiler{id: "tinygo 0.34", err: errors.New("boom")})
		def := mustCreateWasm(t, svc, "on_bar/2", contracts.ScopeMarket)
		v := mustPutSource(t, svc, def.ID, "src")
		job, err := svc.SubmitBuild(ctx, def.ID, v.VersionHash, "")
		if err != nil {
			t.Fatal(err)
		}
		done := waitJob(t, svc, job.ID)
		if done.State != JobFailed || !strings.Contains(done.Error, "boom") {
			t.Fatalf("job must carry the failure: %+v", done)
		}
	})

	t.Run("compiled bytes failing verify are not stored", func(t *testing.T) {
		svc, store := newHarness(t, &fakeCompiler{id: "tinygo 0.34", bytes: []byte("not wasm")})
		def := mustCreateWasm(t, svc, "on_bar/2", contracts.ScopeMarket)
		v := mustPutSource(t, svc, def.ID, "src")
		job, _ := svc.SubmitBuild(ctx, def.ID, v.VersionHash, "")
		done := waitJob(t, svc, job.ID)
		if done.State != JobFailed {
			t.Fatalf("invalid artifact must fail the job: %+v", done)
		}
		arts, err := store.ArtifactsForVersion(ctx, def.ID, v.VersionHash)
		if err != nil || len(arts) != 0 {
			t.Fatalf("invalid artifact was stored: %v %v", arts, err)
		}
	})
}

func TestService_ValidateRejectsAndPromoteRequiresValidated(t *testing.T) {
	ctx := context.Background()
	bad := wasmWithImports("env.malloc")
	compiler := &fakeCompiler{id: "tinygo 0.34", bytes: bad}
	svc, store := newHarness(t, compiler)
	def := mustCreateWasm(t, svc, "on_bar/2", contracts.ScopeMarket)
	v := mustPutSource(t, svc, def.ID, "src")

	// 绕过 build 的门直接登记（模拟外部编译服务回填不合规产物）。
	artifact := Artifact{
		StrategyID: def.ID, VersionHash: v.VersionHash, ArtifactHash: ArtifactHash(bad),
		Carrier: CarrierWASM, ABIVersion: "on_bar/2", Toolchain: "tinygo 0.34",
		SizeBytes: int64(len(bad)), MemoryLimitBytes: 64 << 20, Status: StatusBuilt,
		Storage: StorageInline, Bytes: bad,
	}
	if _, err := svc.RegisterArtifact(ctx, artifact); !errors.Is(err, contracts.ErrImportNotWhitelisted) {
		t.Fatalf("registration must verify bytes, got %v", err)
	}

	good := wasmWithImports("env.get_bar2")
	artifact.Bytes = good
	artifact.ArtifactHash = ArtifactHash(good)
	artifact.SizeBytes = int64(len(good))
	if _, err := svc.RegisterArtifact(ctx, artifact); err != nil {
		t.Fatal(err)
	}
	// 未 validate 就 promote → 409。
	if _, err := svc.Promote(ctx, def.ID, v.VersionHash, ""); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("got %v, want ErrInvalidTransition", err)
	}
	if _, err := svc.ValidateVersion(ctx, def.ID, v.VersionHash, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ValidateVersion(ctx, def.ID, v.VersionHash, ""); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("double validate must fail, got %v", err)
	}
	got, _ := store.GetArtifact(ctx, def.ID, artifact.ArtifactHash)
	if got.Status != StatusValidated {
		t.Fatalf("status %s", got.Status)
	}
}

func TestService_MultipleToolchainsRequireExplicitChoice(t *testing.T) {
	ctx := context.Background()
	svc, _ := newHarness(t, nil)
	def := mustCreateWasm(t, svc, "on_bar/2", contracts.ScopeMarket)
	v := mustPutSource(t, svc, def.ID, "src")

	b1 := wasmWithImports("env.get_bar2")
	b2 := wasmWithImports("env.get_bar2", "env.emit_target2")
	for _, tc := range []struct {
		name, toolchain string
		bytes           []byte
	}{{"tinygo 0.34", "tinygo 0.34", b1}, {"go1.25 wasip1", "go1.25 wasip1", b2}} {
		if _, err := svc.RegisterArtifact(ctx, Artifact{
			StrategyID: def.ID, VersionHash: v.VersionHash, ArtifactHash: ArtifactHash(tc.bytes),
			Carrier: CarrierWASM, ABIVersion: "on_bar/2", Toolchain: tc.toolchain,
			SizeBytes: int64(len(tc.bytes)), MemoryLimitBytes: 64 << 20, Status: StatusBuilt,
			Storage: StorageInline, Bytes: tc.bytes,
		}); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
	if _, err := svc.ValidateVersion(ctx, def.ID, v.VersionHash, ""); !errors.Is(err, ErrAmbiguousArtifact) {
		t.Fatalf("got %v, want ErrAmbiguousArtifact", err)
	}
	if _, err := svc.ValidateVersion(ctx, def.ID, v.VersionHash, "nope"); !errors.Is(err, ErrArtifactNotFound) {
		t.Fatalf("got %v, want ErrArtifactNotFound", err)
	}
	if _, err := svc.ValidateVersion(ctx, def.ID, v.VersionHash, "tinygo 0.34"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Promote(ctx, def.ID, v.VersionHash, "tinygo 0.34"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.ArtifactForVersion(ctx, def.ID, v.VersionHash, "go1.25 wasip1")
	if err != nil || !bytes.Equal(got.Bytes, b2) {
		t.Fatalf("explicit toolchain must pick its row: %v", err)
	}
}

func TestService_ListVersions(t *testing.T) {
	ctx := context.Background()
	compiler := &fakeCompiler{id: "tinygo 0.34", bytes: wasmWithImports("env.get_bar2")}
	svc, _ := newHarness(t, compiler)
	def := mustCreateWasm(t, svc, "on_bar/2", contracts.ScopeMarket)

	views, err := svc.ListVersions(ctx, def.ID)
	if err != nil {
		t.Fatal(err)
	}
	if views == nil || len(views) != 0 {
		t.Fatalf("empty strategy must return an empty slice, got %#v", views)
	}

	v1 := mustPutSource(t, svc, def.ID, "src-1")
	views, _ = svc.ListVersions(ctx, def.ID)
	if len(views) != 1 || views[0].Status != "source" || views[0].ArtifactHash != "" {
		t.Fatalf("source-only version: %+v", views)
	}

	job, _ := svc.SubmitBuild(ctx, def.ID, v1.VersionHash, "")
	waitJob(t, svc, job.ID)
	views, _ = svc.ListVersions(ctx, def.ID)
	if len(views) != 1 || views[0].Status != string(StatusBuilt) || views[0].Toolchain == "" {
		t.Fatalf("built version: %+v", views)
	}

	v2 := mustPutSource(t, svc, def.ID, "src-2")
	views, _ = svc.ListVersions(ctx, def.ID)
	if len(views) != 2 {
		t.Fatalf("got %d rows, want 2", len(views))
	}
	if _, err := svc.ListVersions(ctx, "missing"); !errors.Is(err, ErrStrategyNotFound) {
		t.Fatalf("got %v, want ErrStrategyNotFound", err)
	}
	_ = v2
}

func TestService_ArtifactStorageURLFailsClosed(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	def := Definition{ID: "s1", Carrier: CarrierWASM, ABIVersion: "on_bar/2",
		Scope: contracts.ScopeMarket, Entry: "OnBar"}
	_ = store.CreateDefinition(ctx, def)
	versionHash := strings.Repeat("a", 64)
	_, _ = store.SaveVersion(ctx, Version{StrategyID: "s1", VersionHash: versionHash, Source: "src",
		Carrier: CarrierWASM, ABIVersion: "on_bar/2", Scope: contracts.ScopeMarket, Entry: "OnBar"})
	a := Artifact{StrategyID: "s1", VersionHash: versionHash, ArtifactHash: strings.Repeat("b", 64),
		Carrier: CarrierWASM, ABIVersion: "on_bar/2", Toolchain: "tc", SizeBytes: 10,
		MemoryLimitBytes: 64 << 20, Status: StatusBuilt, Storage: StorageURL}
	if _, err := store.SaveArtifact(ctx, a); err != nil {
		t.Fatal(err)
	}
	svc, _ := New(Options{Store: store, Now: fixedNow})
	if _, err := svc.ArtifactForVersion(ctx, "s1", versionHash, "tc"); !errors.Is(err, ErrStorageUnsupported) {
		t.Fatalf("got %v, want ErrStorageUnsupported", err)
	}
}

func TestService_BuildJobPersistsAcrossServiceInstances(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	compiler := &fakeCompiler{id: "tinygo 0.34", bytes: wasmWithImports("env.get_bar2")}
	svc1, err := New(Options{Store: store, Compiler: compiler, IDGen: seqIDs(), Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	def := mustCreateWasm(t, svc1, "on_bar/2", contracts.ScopeMarket)
	v := mustPutSource(t, svc1, def.ID, "src")
	job, err := svc1.SubmitBuild(ctx, def.ID, v.VersionHash, "")
	if err != nil {
		t.Fatal(err)
	}
	done := waitJob(t, svc1, job.ID)
	if done.State != JobSucceeded {
		t.Fatalf("build failed: %+v", done)
	}

	// 新 Service = 新进程视角：job 不在进程内存里，必须来自 Store。
	svc2, err := New(Options{Store: store, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc2.Job(ctx, job.ID)
	if err != nil {
		t.Fatalf("job must survive a service restart: %v", err)
	}
	if got.State != JobSucceeded || got.ArtifactHash != done.ArtifactHash {
		t.Fatalf("job after restart = %+v", got)
	}
	if got.FinishedAt == nil {
		t.Fatal("finished job must carry finished_at")
	}
	if _, err := svc2.Job(ctx, "st_missing"); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("got %v, want ErrJobNotFound", err)
	}
}

func TestService_SOArtifactRequiresExportedEntry(t *testing.T) {
	ctx := context.Background()
	svc, _ := newHarness(t, nil)
	def, err := svc.CreateDefinition(ctx, CreateDefinitionRequest{
		Carrier: CarrierSO, ABIVersion: "on_bar/2", Scope: contracts.ScopeMarket, Entry: "Strategy",
	})
	if err != nil {
		t.Fatal(err)
	}
	v := mustPutSource(t, svc, def.ID, "package strategy")

	wrong := buildSharedObject([]testSymbol{{"OnBar", globalFunc, 3}})
	if _, err := svc.RegisterArtifact(ctx, Artifact{
		StrategyID: def.ID, VersionHash: v.VersionHash, ArtifactHash: ArtifactHash(wrong),
		Carrier: CarrierSO, ABIVersion: "on_bar/2", Toolchain: "gcc 13",
		SizeBytes: int64(len(wrong)), MemoryLimitBytes: 64 << 20, Status: StatusBuilt,
		Storage: StorageInline, Bytes: wrong,
	}); !errors.Is(err, ErrInvalidArtifact) {
		t.Fatalf("so without the declared export must fail closed, got %v", err)
	}

	good := buildSharedObject([]testSymbol{{"Strategy", globalFunc, 3}})
	if _, err := svc.RegisterArtifact(ctx, Artifact{
		StrategyID: def.ID, VersionHash: v.VersionHash, ArtifactHash: ArtifactHash(good),
		Carrier: CarrierSO, ABIVersion: "on_bar/2", Toolchain: "gcc 13",
		SizeBytes: int64(len(good)), MemoryLimitBytes: 64 << 20, Status: StatusBuilt,
		Storage: StorageInline, Bytes: good,
	}); err != nil {
		t.Fatalf("valid so rejected: %v", err)
	}
	if _, err := svc.ValidateVersion(ctx, def.ID, v.VersionHash, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Promote(ctx, def.ID, v.VersionHash, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ArtifactForVersion(ctx, def.ID, v.VersionHash, ""); err != nil {
		t.Fatalf("so download re-checks the export table: %v", err)
	}
}

func TestService_RegisterArtifactRejectsMismatchedVersionMetadata(t *testing.T) {
	ctx := context.Background()
	svc, _ := newHarness(t, nil)
	def := mustCreateWasm(t, svc, "on_bar/1", contracts.ScopeSymbol)
	v := mustPutSource(t, svc, def.ID, "src")
	good := wasmWithImports("env.get_bar")
	_, err := svc.RegisterArtifact(ctx, Artifact{
		StrategyID: def.ID, VersionHash: v.VersionHash, ArtifactHash: ArtifactHash(good),
		Carrier: CarrierWASM, ABIVersion: "on_bar/2", Toolchain: "tc",
		SizeBytes: int64(len(good)), MemoryLimitBytes: 64 << 20, Status: StatusBuilt,
		Storage: StorageInline, Bytes: good,
	})
	if !errors.Is(err, ErrInvalidArtifact) {
		t.Fatalf("got %v, want ErrInvalidArtifact", err)
	}
}

// TestMigrations_PinTheAdjudicatedShape 把两处主键裁决（docs/adr/0002）钉在 DDL 上：
// 只要有人把 active_artifact_hash 或 artifact 主键改回去，这条就红。
func TestMigrations_PinTheAdjudicatedShape(t *testing.T) {
	stmts := Migrations()
	if len(stmts) == 0 {
		t.Fatal("no migration statements")
	}
	for i, stmt := range stmts {
		// 每条都必须可重入：空库一次跑完、重跑不报错。
		if !strings.Contains(stmt, "IF NOT EXISTS") {
			t.Fatalf("statement %d is not idempotent: %s", i+1, stmt)
		}
	}
	all := strings.Join(stmts, "\n")
	for _, want := range []string{
		"active_artifact_hash CHAR(64)",
		"CHECK ((active_version_hash IS NULL) = (active_artifact_hash IS NULL))",
		"PRIMARY KEY (strategy_id, artifact_hash)",
		"UNIQUE (strategy_id, version_hash, toolchain)",
		"octet_length(bytes) <= 8388608",
		"CHECK (status IN ('built','validated','promoted','rejected','deprecated'))",
		"CREATE TABLE IF NOT EXISTS strategy.strategy_build_jobs",
		"CHECK (state IN ('queued','running','succeeded','failed'))",
	} {
		if !strings.Contains(all, want) {
			t.Fatalf("DDL is missing %q", want)
		}
	}

	// 返回值必须是副本：调用方改不动本包的建表语句。
	first := stmts[0]
	stmts[0] = "DROP SCHEMA strategy CASCADE"
	if Migrations()[0] != first {
		t.Fatal("Migrations must return a copy")
	}
}

func TestService_CreateDefinitionFailsClosedOnInvalidShape(t *testing.T) {
	svc, _ := newHarness(t, nil)
	ctx := context.Background()

	cases := []struct {
		name string
		req  CreateDefinitionRequest
		want error
	}{
		{"unknown carrier", CreateDefinitionRequest{Carrier: "elf", Scope: contracts.ScopeMarket, Entry: "OnBar"}, ErrUnsupportedCarrier},
		{"wasm without abi", CreateDefinitionRequest{Carrier: CarrierWASM, Scope: contracts.ScopeMarket, Entry: "OnBar"}, ErrInvalidABIVersion},
		{"wasm with unknown scope", CreateDefinitionRequest{Carrier: CarrierWASM, ABIVersion: "on_bar/2", Scope: "galaxy", Entry: "OnBar"}, ErrInvalidScope},
		{"builtin with abi", CreateDefinitionRequest{Carrier: CarrierBuiltin, ABIVersion: "on_bar/2", Scope: contracts.ScopeSymbol, Entry: "OnBar"}, ErrInvalidABIVersion},
		{"missing entry", CreateDefinitionRequest{Carrier: CarrierWASM, ABIVersion: "on_bar/2", Scope: contracts.ScopeMarket}, ErrInvalidDefinition},
	}
	for _, tc := range cases {
		if _, err := svc.CreateDefinition(ctx, tc.req); !errors.Is(err, tc.want) {
			t.Fatalf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}
	// 被拒的定义不得落库：id 可能已被消费，但查询必须 404。
	if _, err := svc.ListVersions(ctx, "st_0001"); !errors.Is(err, ErrStrategyNotFound) {
		t.Fatalf("rejected definition was stored: %v", err)
	}
}
