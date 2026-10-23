// Command assetservice 是策略资产服务的进程入口。
//
// 当前装配的是内存 Store：端口（asset.Store）已经稳定，PG 实现与迁移执行属于
// QT-41 的下一批（见 docs/TASKS.md 的未落项），本命令不假装已接库。
// 编译端口不在此进程内（D 裁决：cmd/compiler 独立进程，经内部端点登记）。
package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"tellurion-strategy/internal/asset"
	"tellurion-strategy/internal/httpapi"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "assetservice: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	addr := flag.String("addr", "127.0.0.1:8085", "listen address")
	flag.Parse()

	svc, err := asset.New(asset.Options{Store: asset.NewMemory()})
	if err != nil {
		return err
	}
	api, err := httpapi.New(svc)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	slog.Info("strategy asset service listening", "addr", *addr,
		"storage", "memory (PG port pending)", "compiler", "external (cmd/compiler)")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
