// astral-server 入口。模块装配在 internal/app；本文件只做配置、生命周期与信号处理。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/app"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/config"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ids"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/event"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("astral-server exited with error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	if cfg.ServerID == "" {
		cfg.ServerID = ids.New(ids.Server)
		log.Warn("ASTRAL_SERVER_ID not set; generated ephemeral server_id (dev only). " +
			"Restarting will invalidate CLI bindings — set ASTRAL_SERVER_ID or persist via server_meta (phase-1).")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 可选数据库：DSN 为空时以无存储模式启动（healthz ok / readyz 503 / 业务端点 501）。
	var db app.DB
	if cfg.DatabaseDSN != "" {
		gormDB, err := store.Open(ctx, cfg.DatabaseDSN, cfg.AutoMigrate, log)
		if err != nil {
			return err
		}
		db = app.PingDB{Pinger: func() error { return store.Ping(context.Background(), gormDB) }}
		// TODO(phase-1): 从 server_meta 读取/固化稳定 server_id，覆盖 cfg.ServerID。
		_ = gormDB
	} else {
		log.Warn("ASTRAL_DATABASE_DSN empty; running without storage (stub mode)")
	}

	hub := event.NewHub()
	handler := app.NewRouter(cfg, log, db, hub)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// TODO(phase-4): SSE 长连接下 WriteTimeout 必须为 0 或足够大；
		// 当前桩阶段先禁用写超时，联调 SSE 时避免被掐断。
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("astral-server listening", "addr", cfg.HTTPAddr, "config", cfg.Describe())
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
