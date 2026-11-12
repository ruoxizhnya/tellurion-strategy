package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kithttp "github.com/ruoxizhnya/tellurion-kit/auth/httpadapter"

	"tellurion-strategy/internal/asset"
	"tellurion-strategy/internal/httpapi"
)

// 启动闸的三态（与 risk / analytics 等服务同口径）：
//
//  1. 无 secret + 回环 + 未声明 ⇒ 起，受保护路由 503（openAccess=false）；
//  2. 无 secret + 非回环 + 未声明 ⇒ **拒绝启动**（拼错/别名也不算声明）；
//  3. 无 secret + 显式 loopback-published ⇒ 任何绑定都放开（openAccess=true）；
//     有 secret ⇒ 全闸，永不进开放态。
func TestStartupGateThreeStates(t *testing.T) {
	cases := []struct {
		name       string
		secret     string
		bind       string
		exposure   string
		wantRefuse bool
		wantOpen   bool
	}{
		{"state 1: loopback without declaration", "", "127.0.0.1", "", false, false},
		{"state 2: non-loopback without declaration refuses", "", "0.0.0.0", "", true, false},
		{"state 2: misspelled declaration refuses", "", "0.0.0.0", "loopback", true, false},
		{"state 3: non-loopback with explicit declaration", "", "0.0.0.0", kithttp.InsecureExposureLoopbackPublished, false, true},
		{"state 3: loopback with explicit declaration", "", "127.0.0.1", kithttp.InsecureExposureLoopbackPublished, false, true},
		{"secret present never opens", "s3cret", "0.0.0.0", kithttp.InsecureExposureLoopbackPublished, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			startup := kithttp.DecideStartup(tc.secret, true, tc.bind, tc.exposure)
			if startup.Refuse != tc.wantRefuse {
				t.Fatalf("Refuse = %t, want %t (reason: %s)", startup.Refuse, tc.wantRefuse, startup.Reason)
			}
			if tc.wantRefuse {
				if !strings.Contains(startup.Reason, "not loopback") {
					t.Fatalf("refusal reason = %q, want the non-loopback explanation", startup.Reason)
				}
				return
			}
			if got := openAccessEnabled(startup, tc.exposure); got != tc.wantOpen {
				t.Fatalf("openAccessEnabled(%+v, %q) = %t, want %t", startup, tc.exposure, got, tc.wantOpen)
			}
		})
	}
}

// 第 3 态的端到端钉子：非回环 + 显式声明 ⇒ 装配开放闸，`POST /api/strategies`
// 真的到业务层并 201（不是 401/403/503）。装配顺序与 main 一致：
// 启动闸 → openAccessEnabled → NewOpenAccessAuthorizer → httpapi。
func TestOpenAccessNonLoopbackCreateReachesBusiness(t *testing.T) {
	startup := kithttp.DecideStartup("", true, "0.0.0.0", kithttp.InsecureExposureLoopbackPublished)
	if startup.Refuse {
		t.Fatalf("explicit exposure must allow a non-loopback bind, refused: %s", startup.Reason)
	}
	if !openAccessEnabled(startup, kithttp.InsecureExposureLoopbackPublished) {
		t.Fatal("explicit exposure must enable open access")
	}

	svc, err := asset.New(asset.Options{Store: asset.NewMemory()})
	if err != nil {
		t.Fatalf("asset.New: %v", err)
	}
	api, err := httpapi.New(svc, httpapi.WithAuthorizer(httpapi.NewOpenAccessAuthorizer()))
	if err != nil {
		t.Fatalf("httpapi.New: %v", err)
	}
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()

	const body = `{"carrier":"wasm","abi_version":"on_bar/2","scope":"market","entry":"OnBar"}`
	resp, err := http.Post(srv.URL+"/api/strategies", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/strategies: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("non-loopback open access create = %d, want 201: %s", resp.StatusCode, raw)
	}
}

// 回环但**未声明**仍不是第 3 态：启动闸允许进程起来（第 1 态），但受保护路由
// 必须继续 503。这条防止把「回环」误读成「开放」。
func TestLoopbackWithoutDeclarationStaysClosed(t *testing.T) {
	startup := kithttp.DecideStartup("", true, "127.0.0.1", "")
	if startup.Refuse || !startup.Insecure {
		t.Fatalf("loopback without declaration must start in closed mode, got %+v", startup)
	}
	if openAccessEnabled(startup, "") {
		t.Fatal("loopback without an explicit declaration must not enable open access")
	}
}
