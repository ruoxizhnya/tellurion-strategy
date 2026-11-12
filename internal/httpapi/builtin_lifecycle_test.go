package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// F6 回归：进程启动后（RegisterBuiltins 出厂种子）内置策略必须可直接按 id
// resolve，而不是 404 no_active_version。这是 W4.5 判 BLOCKED 的那条链路。
func TestBuiltinResolveIsAvailableAfterFactorySeed(t *testing.T) {
	srv, svc := newServer(t, nil)
	seeded, err := svc.RegisterBuiltins(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if seeded != 14 {
		t.Fatalf("factory seed = %d, want 14", seeded)
	}

	resolved := call(t, srv, http.MethodGet, "/api/strategies/momentum/resolve?ref=latest", nil, nil)
	wantStatus(t, resolved, http.StatusOK)
	got := decodeObject(t, resolved.Body)
	for key, want := range map[string]any{
		"artifact_hash": "",
		"carrier":       "builtin",
		"entry":         "momentum",
		"status":        "promoted",
	} {
		if got[key] != want {
			t.Fatalf("resolve[%s] = %v, want %v (body: %s)", key, got[key], want, resolved.Body)
		}
	}
	if vh, _ := got["version_hash"].(string); vh == "" {
		t.Fatalf("resolve returned no version_hash: %s", resolved.Body)
	}

	// 版本列表不再是空数组：出厂版本必须可见（F6 现场是 200 []）。
	versions := call(t, srv, http.MethodGet, "/api/strategies/momentum/versions", nil, nil)
	wantStatus(t, versions, http.StatusOK)
	if strings.TrimSpace(string(versions.Body)) == "[]" {
		t.Fatalf("versions still empty after factory seed: %s", versions.Body)
	}
}

// builtin 的完整生命周期（ADR-0006）：建 → 写源 → validate → promote →
// resolve latest 全链通过，且 artifact_hash 为空但 carrier=builtin 明确。
// build 仍拒绝，但文案要指向 validate → promote。
func TestBuiltinLifecyclePromotesWithoutArtifact(t *testing.T) {
	srv, _ := newServer(t, nil)
	id := createDefinition(t, srv, nil, map[string]any{
		"carrier": "builtin",
		"scope":   "market",
		"entry":   "momentum",
		"capability": map[string]any{
			"frequencies": []string{"1d"},
			"markets":     []string{"crypto"},
		},
	})
	if !strings.HasPrefix(id, "st_") {
		t.Fatalf("id = %q", id)
	}

	src := call(t, srv, http.MethodPut, "/api/strategies/"+id+"/source",
		map[string]string{"source": "momentum@builtin (in-binary implementation)"}, nil)
	wantStatus(t, src, http.StatusOK)
	version, _ := decodeObject(t, src.Body)["version_hash"].(string)
	if version == "" {
		t.Fatalf("source returned no version_hash: %s", src.Body)
	}

	validated := call(t, srv, http.MethodPost,
		"/api/strategies/"+id+"/versions/"+version+"/validate", nil, nil)
	wantStatus(t, validated, http.StatusOK)
	v := decodeObject(t, validated.Body)
	if v["status"] != "validated" || v["artifact_hash"] != "" || v["carrier"] != "builtin" {
		t.Fatalf("validate view = %s, want status=validated carrier=builtin artifact_hash=\"\"", validated.Body)
	}

	promoted := call(t, srv, http.MethodPost,
		"/api/strategies/"+id+"/versions/"+version+"/promote", nil,
		map[string]string{"X-Actor": "ops"})
	wantStatus(t, promoted, http.StatusOK)
	p := decodeObject(t, promoted.Body)
	if p["status"] != "promoted" || p["artifact_hash"] != "" || p["carrier"] != "builtin" {
		t.Fatalf("promote view = %s, want status=promoted carrier=builtin artifact_hash=\"\"", promoted.Body)
	}

	resolved := call(t, srv, http.MethodGet, "/api/strategies/"+id+"/resolve?ref=latest", nil, nil)
	wantStatus(t, resolved, http.StatusOK)
	got := decodeObject(t, resolved.Body)
	for key, want := range map[string]any{
		"artifact_hash": "",
		"carrier":       "builtin",
		"entry":         "momentum",
		"version_hash":  version,
		"status":        "promoted",
	} {
		if got[key] != want {
			t.Fatalf("resolve[%s] = %v, want %v (body: %s)", key, got[key], want, resolved.Body)
		}
	}

	// 同一版本按 hash 解析也必须命中（不是只有 latest 特判）。
	byHash := call(t, srv, http.MethodGet, "/api/strategies/"+id+"/resolve?ref="+version, nil, nil)
	wantStatus(t, byHash, http.StatusOK)
	if h := decodeObject(t, byHash.Body); h["artifact_hash"] != "" || h["entry"] != "momentum" {
		t.Fatalf("resolve by hash = %s", byHash.Body)
	}

	// build 仍拒绝（builtin 不产 artifact），但文案要把下一步说清楚。
	build := call(t, srv, http.MethodPost, "/api/strategies/"+id+"/build",
		map[string]string{"version_hash": version}, nil)
	wantStatus(t, build, http.StatusBadRequest)
	msg, _ := decodeObject(t, build.Body)["error"].(string)
	if !strings.Contains(msg, "needs no build") || !strings.Contains(msg, "validate then promote") {
		t.Fatalf("build error = %q, want the validate-then-promote hint", msg)
	}
}

// dsl / external 没有本仓运行时实现，不能跟 builtin 一样开放版本级激活：
// validate 仍 404 artifact_not_found、promote 仍 400 unsupported_carrier。
func TestArtifactlessNonBuiltinCarriersStayClosed(t *testing.T) {
	srv, _ := newServer(t, nil)
	for _, carrier := range []string{"dsl", "external"} {
		t.Run(carrier, func(t *testing.T) {
			id := createDefinition(t, srv, nil, map[string]any{
				"carrier": carrier,
				"scope":   "market",
				"entry":   "momentum",
			})
			src := call(t, srv, http.MethodPut, "/api/strategies/"+id+"/source",
				map[string]string{"source": "momentum@1.0.0"}, nil)
			wantStatus(t, src, http.StatusOK)
			version, _ := decodeObject(t, src.Body)["version_hash"].(string)

			wantErrorCode(t, call(t, srv, http.MethodPost,
				"/api/strategies/"+id+"/versions/"+version+"/validate", nil, nil),
				http.StatusNotFound, "artifact_not_found")
			wantErrorCode(t, call(t, srv, http.MethodPost,
				"/api/strategies/"+id+"/versions/"+version+"/promote", nil,
				map[string]string{"X-Actor": "ops"}),
				http.StatusBadRequest, "unsupported_carrier")
			wantErrorCode(t, call(t, srv, http.MethodGet,
				"/api/strategies/"+id+"/resolve?ref=latest", nil, nil),
				http.StatusNotFound, "no_active_version")
		})
	}
}
