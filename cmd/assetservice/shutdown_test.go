package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

// 验收 ⑤（STRAT-GRACEFUL-SHUTDOWN）：信号到达后，在途慢请求必须在 drain 窗口
// 内正常完成，serveWithContext 返回 nil（进程退出码 0）。
func TestServeWithContextDrainsInFlightRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		select {
		case <-release:
			w.WriteHeader(http.StatusNoContent)
		case <-r.Context().Done():
			return
		}
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveErr := make(chan error, 1)
	go func() { serveErr <- serveWithContext(ctx, ln, srv, 5*time.Second) }()

	type result struct {
		status int
		err    error
	}
	res := make(chan result, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/slow")
		if err != nil {
			res <- result{err: err}
			return
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		res <- result{status: resp.StatusCode}
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the slow request never reached the handler")
	}

	cancel() // 等价于收到 SIGTERM
	close(release)

	select {
	case got := <-res:
		if got.err != nil {
			t.Fatalf("in-flight request was cut off after the termination signal: %v", got.err)
		}
		if got.status != http.StatusNoContent {
			t.Fatalf("in-flight request status = %d, want 204", got.status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight request did not finish within the drain window")
	}

	select {
	case err := <-serveErr:
		if err != nil {
			t.Fatalf("serveWithContext = %v after a clean drain, want nil (exit code 0)", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serveWithContext did not return after the clean drain")
	}
}

// drain 超时必须是**非零退出**：卡死的在途请求被 Close 兜底掐断，serveWithContext
// 返回错误，而不是无限等待或伪装成干净退出。
func TestServeWithContextDrainTimeoutIsNonZeroExit(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("/hang", func(_ http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		<-r.Context().Done() // 只有 srv.Close 能解除
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveErr := make(chan error, 1)
	go func() { serveErr <- serveWithContext(ctx, ln, srv, 100*time.Millisecond) }()

	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/hang")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the hanging request never reached the handler")
	}

	cancel() // 等价于收到 SIGTERM

	select {
	case err := <-serveErr:
		if err == nil {
			t.Fatal("serveWithContext returned nil on drain timeout, want a non-zero exit error")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("serveWithContext error = %v, want wrapped context.DeadlineExceeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serveWithContext did not return after the drain window elapsed")
	}
}
