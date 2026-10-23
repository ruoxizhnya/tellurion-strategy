package compilerjob

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	contracts "github.com/ruoxizhnya/tellurion-contracts/engine"

	"tellurion-strategy/internal/asset"
	"tellurion-strategy/internal/httpapi"
)

// ─── 语料与替身 ────────────────────────────────────────────────

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
		payload = append(payload, 0x00)
		payload = appendULEB(payload, 0)
	}
	out := []byte("\x00asm")
	var version [4]byte
	binary.LittleEndian.PutUint32(version[:], 1)
	out = append(out, version[:]...)
	out = append(out, 2)
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

type fakeRunner struct {
	bytes  []byte
	log    string
	err    error
	calls  int
	lastOK asset.CompileRequest
}

func (f *fakeRunner) ToolchainID() string { return "faketc 1.0.0" }

func (f *fakeRunner) Compile(ctx context.Context, req asset.CompileRequest) (asset.CompileResult, error) {
	f.calls++
	f.lastOK = req
	if f.err != nil {
		return asset.CompileResult{}, f.err
	}
	return asset.CompileResult{Bytes: append([]byte(nil), f.bytes...), Log: f.log}, nil
}

// fixture 装配一整套真 HTTP 服务（asset.Service + httpapi）与一个已备好源码的定义。
type fixture struct {
	srv      *httptest.Server
	svc      *asset.Service
	id       string
	version  string
	register string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	svc, err := asset.New(asset.Options{
		Store: asset.NewMemory(),
		IDGen: func() string { return "st_0001" },
		Now:   func() time.Time { return time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("asset.New: %v", err)
	}
	api, err := httpapi.New(svc)
	if err != nil {
		t.Fatalf("httpapi.New: %v", err)
	}
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)

	ctx := context.Background()
	def, err := svc.CreateDefinition(ctx, asset.CreateDefinitionRequest{
		Carrier:    asset.CarrierWASM,
		ABIVersion: "on_bar/2",
		Scope:      contracts.ScopeMarket,
		Entry:      "OnBar",
	})
	if err != nil {
		t.Fatalf("CreateDefinition: %v", err)
	}
	version, err := svc.PutSource(ctx, def.ID, "package strategy")
	if err != nil {
		t.Fatalf("PutSource: %v", err)
	}
	return &fixture{srv: srv, svc: svc, id: def.ID, version: version.VersionHash}
}

func (f *fixture) job() Job {
	return Job{
		StrategyID:  f.id,
		VersionHash: f.version,
		Carrier:     string(asset.CarrierWASM),
		ABIVersion:  "on_bar/2",
		Entry:       "OnBar",
		Source:      "package strategy",
	}
}

// ─── 测试 ──────────────────────────────────────────────────────

func TestJob_Validate(t *testing.T) {
	base := Job{
		StrategyID: "st_1", VersionHash: strings.Repeat("a", 64),
		Carrier: "wasm", ABIVersion: "on_bar/2", Entry: "OnBar", Source: "package strategy",
	}
	tests := []struct {
		name   string
		mutate func(*Job)
		wantOK bool
	}{
		{"valid wasm", func(j *Job) {}, true},
		{"valid so", func(j *Job) { j.Carrier = "so" }, true},
		{"missing strategy id", func(j *Job) { j.StrategyID = " " }, false},
		{"missing version hash", func(j *Job) { j.VersionHash = "" }, false},
		{"builtin carrier", func(j *Job) { j.Carrier = "builtin" }, false},
		{"missing entry", func(j *Job) { j.Entry = "" }, false},
		{"blank source", func(j *Job) { j.Source = "  " }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			job := base
			tc.mutate(&job)
			err := job.Validate()
			if tc.wantOK && err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if !tc.wantOK && err == nil {
				t.Fatalf("Validate accepted an invalid job")
			}
		})
	}
}

func TestRun_CompilesAndRegisters(t *testing.T) {
	fx := newFixture(t)
	wasm := wasmWithImports("env.get_bar", "env.emit_target2")
	runner := &fakeRunner{bytes: wasm, log: "compiled ok"}

	artifact, err := Run(context.Background(), Config{
		AssetURL:         fx.srv.URL,
		Runner:           runner,
		MemoryLimitBytes: 2 << 20,
	}, fx.job())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if artifact.Status != asset.StatusBuilt {
		t.Fatalf("status = %s, want built", artifact.Status)
	}
	if artifact.ArtifactHash != asset.ArtifactHash(wasm) {
		t.Fatalf("artifact_hash = %s", artifact.ArtifactHash)
	}
	if artifact.Toolchain != "faketc 1.0.0" || artifact.MemoryLimitBytes != 2<<20 {
		t.Fatalf("artifact = %+v", artifact)
	}
	if len(artifact.Imports) != 2 || artifact.Imports[0] != "env.emit_target2" {
		t.Fatalf("imports = %v, want the wasm import section", artifact.Imports)
	}
	if runner.calls != 1 {
		t.Fatalf("compiler calls = %d, want 1", runner.calls)
	}
	if runner.lastOK.Carrier != asset.CarrierWASM || runner.lastOK.MemoryLimitBytes != 2<<20 ||
		runner.lastOK.Source != "package strategy" {
		t.Fatalf("compile request = %+v", runner.lastOK)
	}

	// 服务端确实落了字节，且能过 Verify 门。
	stored, err := fx.svc.ArtifactForVersion(context.Background(), fx.id, fx.version, "faketc 1.0.0")
	if err != nil {
		t.Fatalf("ArtifactForVersion: %v", err)
	}
	if string(stored.Bytes) != string(wasm) {
		t.Fatalf("stored bytes differ")
	}
	if stored.CompileLog != "compiled ok" {
		t.Fatalf("compile log not stored: %q", stored.CompileLog)
	}
}

func TestRun_FailsClosed(t *testing.T) {
	fx := newFixture(t)
	wasm := wasmWithImports("env.get_bar")

	t.Run("builtin carrier", func(t *testing.T) {
		runner := &fakeRunner{bytes: wasm}
		job := fx.job()
		job.Carrier = "builtin"
		_, err := Run(context.Background(), Config{AssetURL: fx.srv.URL, Runner: runner}, job)
		if !errors.Is(err, asset.ErrUnsupportedCarrier) {
			t.Fatalf("err = %v, want ErrUnsupportedCarrier", err)
		}
		if runner.calls != 0 {
			t.Fatalf("compiler must not run for a rejected job (calls=%d)", runner.calls)
		}
	})

	t.Run("no runner", func(t *testing.T) {
		_, err := Run(context.Background(), Config{AssetURL: fx.srv.URL}, fx.job())
		if !errors.Is(err, asset.ErrCompileUnavailable) {
			t.Fatalf("err = %v, want ErrCompileUnavailable", err)
		}
	})

	t.Run("bad asset url", func(t *testing.T) {
		for _, raw := range []string{"", "://nope", "not-a-url"} {
			_, err := Run(context.Background(), Config{AssetURL: raw, Runner: &fakeRunner{bytes: wasm}}, fx.job())
			if !errors.Is(err, asset.ErrInvalidParameter) {
				t.Fatalf("AssetURL %q err = %v, want ErrInvalidParameter", raw, err)
			}
		}
	})

	t.Run("compiler error propagates", func(t *testing.T) {
		sentinel := errors.New("toolchain exploded")
		_, err := Run(context.Background(), Config{AssetURL: fx.srv.URL, Runner: &fakeRunner{err: sentinel}}, fx.job())
		if !errors.Is(err, sentinel) {
			t.Fatalf("err = %v, want the compiler error", err)
		}
	})

	t.Run("service rejects registration", func(t *testing.T) {
		job := fx.job()
		job.VersionHash = strings.Repeat("f", 64)
		_, err := Run(context.Background(), Config{AssetURL: fx.srv.URL, Runner: &fakeRunner{bytes: wasm}}, job)
		if !errors.Is(err, asset.ErrArtifactFetchFailed) {
			t.Fatalf("err = %v, want ErrArtifactFetchFailed", err)
		}
		if !strings.Contains(err.Error(), "version_not_found") {
			t.Fatalf("err %q must carry the service wire code", err)
		}
	})

	t.Run("artifact bytes rejected by service", func(t *testing.T) {
		// 字节不是合法 wasm：服务端 Verify 拒收，编译器不能自说自话。
		_, err := Run(context.Background(), Config{AssetURL: fx.srv.URL,
			Runner: &fakeRunner{bytes: []byte("not-wasm")}}, fx.job())
		if !errors.Is(err, asset.ErrArtifactFetchFailed) {
			t.Fatalf("err = %v, want ErrArtifactFetchFailed", err)
		}
		if !strings.Contains(err.Error(), "artifact_invalid") {
			t.Fatalf("err %q must carry artifact_invalid", err)
		}
	})
}

func TestRun_DetectsLyingService(t *testing.T) {
	fx := newFixture(t)
	wasm := wasmWithImports("env.get_bar")

	t.Run("empty artifact hash", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"strategy_id":"x"}`))
		}))
		defer srv.Close()
		_, err := Run(context.Background(), Config{AssetURL: srv.URL, Runner: &fakeRunner{bytes: wasm}}, fx.job())
		if !errors.Is(err, ErrEmptyServiceResponse) {
			t.Fatalf("err = %v, want ErrEmptyServiceResponse", err)
		}
	})

	t.Run("different artifact hash", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"artifact_hash": strings.Repeat("b", 64)})
		}))
		defer srv.Close()
		_, err := Run(context.Background(), Config{AssetURL: srv.URL, Runner: &fakeRunner{bytes: wasm}}, fx.job())
		if !errors.Is(err, asset.ErrArtifactHashMismatch) {
			t.Fatalf("err = %v, want ErrArtifactHashMismatch", err)
		}
	})

	t.Run("non-json response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("<html>proxy</html>"))
		}))
		defer srv.Close()
		_, err := Run(context.Background(), Config{AssetURL: srv.URL, Runner: &fakeRunner{bytes: wasm}}, fx.job())
		if !errors.Is(err, asset.ErrInvalidParameter) {
			t.Fatalf("err = %v, want ErrInvalidParameter", err)
		}
	})
}

func TestRun_UnreachableService(t *testing.T) {
	fx := newFixture(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	_, err := Run(context.Background(), Config{AssetURL: url, Runner: &fakeRunner{bytes: wasmWithImports("env.get_bar")}}, fx.job())
	if !errors.Is(err, asset.ErrArtifactFetchFailed) {
		t.Fatalf("err = %v, want ErrArtifactFetchFailed", err)
	}
}

func TestRun_RejectsInvalidJobBeforeCompiling(t *testing.T) {
	fx := newFixture(t)
	runner := &fakeRunner{bytes: wasmWithImports("env.get_bar")}
	job := fx.job()
	job.Source = ""

	_, err := Run(context.Background(), Config{AssetURL: fx.srv.URL, Runner: runner}, job)
	if !errors.Is(err, asset.ErrSourceMissing) {
		t.Fatalf("err = %v, want ErrSourceMissing", err)
	}
	if runner.calls != 0 {
		t.Fatalf("compiler must not run for an invalid job (calls=%d)", runner.calls)
	}
}
