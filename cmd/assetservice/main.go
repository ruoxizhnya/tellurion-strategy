// Command assetservice 是策略资产服务的进程入口。
//
// 生产装配是 PG Store：启动时先连库并执行 strategy schema 的幂等 DDL，再开始
// 监听。DSN 为空时退到内存 Store（只用于本地演示/测试），日志里明确标出——
// 不把「重启即丢」伪装成持久化。编译端口不在此进程内（D 裁决：cmd/compiler
// 独立进程，经内部端点登记）。
package main

import (
	"context"
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
	dsn := flag.String("dsn", os.Getenv("STRATEGY_DATABASE_URL"),
		"PostgreSQL DSN; empty selects the in-memory store (non-persistent)")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var (
		store   asset.Store
		storage string
		closeDB func()
	)
	if *dsn == "" {
		store = asset.NewMemory()
		storage = "memory (non-persistent)"
		slog.Warn("STRATEGY_DATABASE_URL is empty; using the in-memory store")
	} else {
		pg, err := asset.OpenPG(ctx, *dsn)
		if err != nil {
			return err
		}
		if err := pg.Migrate(ctx); err != nil {
			pg.Close()
			return fmt.Errorf("migrate: %w", err)
		}
		store = pg
		storage = "postgres"
		closeDB = pg.Close
	}
	if closeDB != nil {
		defer closeDB()
	}

	svc, err := asset.New(asset.Options{Store: store})
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
		"storage", storage, "compiler", "external (cmd/compiler)")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
