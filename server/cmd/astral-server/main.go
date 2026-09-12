// astral-server 入口。装配见 internal/app；本文件只做配置、依赖构造与生命周期。
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

	"gorm.io/gorm"

	"github.com/joho/godotenv"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/app"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/config"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/idempotency"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/audit"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/document"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/event"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/memory"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/message"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/presence"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/tag"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/task"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/workspace"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("astral-server exited with error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	// 本地开发便利：存在 .env 时预加载（shell 已设置的真实环境变量优先，godotenv 不覆盖）。
	// .env 已被 .gitignore 忽略，严禁提交真实凭证；模板见 server/.env.example。
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to load .env; ignoring", "err", err)
	}

	cfg := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 可选数据库：DSN 为空时以无存储模式启动（healthz ok / readyz 503 / 受保护端点 401）。
	var db *gorm.DB
	var authSvc *auth.Service
	hub := event.NewHub()
	idem := &idempotency.Middleware{Log: log} // DB 在有库分支补挂
	mods := &app.Modules{
		Idempotency: idem,
		Auth:        &auth.Module{PublicURL: cfg.PublicURL},
		Tag:         &tag.Module{Auth: authSvc},
		Memory:      &memory.Module{},
		Document:    &document.Module{},
		Audit:       &audit.Module{},
		Events:      &event.SSEHandler{Hub: hub}, // DB/Auth 在有库分支补挂（重放/授权需要）
	}

	if cfg.DatabaseDSN != "" {
		gormDB, err := store.Open(ctx, cfg.DatabaseDSN, cfg.AutoMigrate, log)
		if err != nil {
			return err
		}
		db = gormDB
		// server_id 固化：库中值优先于 env（architecture §7 稳定身份）。
		serverID, err := store.EnsureServerID(ctx, gormDB, cfg.ServerID)
		if err != nil {
			return err
		}
		if serverID != cfg.ServerID {
			log.Info("server_id loaded from server_meta (env value ignored)", "server_id", serverID)
		}
		cfg.ServerID = serverID

		authSvc = auth.NewService(gormDB, log)
		// security.md：凭证/会话撤销后主动断开该 actor 的 SSE 流。
		authSvc.OnRevoke = func(actorID string) { hub.DisconnectActor(actorID) }
		wsMod := &workspace.Module{DB: gormDB, Auth: authSvc}
		taskMod := &task.Module{DB: gormDB, Auth: authSvc, Log: log}
		msgMod := &message.Module{DB: gormDB, Auth: authSvc, Tasks: taskMod}
		presMod := &presence.Module{DB: gormDB, Auth: authSvc}
		tagMod := &tag.Module{DB: gormDB, Auth: authSvc}

		mods.Auth.Svc = authSvc
		mods.Workspace = wsMod
		mods.Task = taskMod
		mods.Message = msgMod
		mods.Presence = presMod
		mods.Tag = tagMod

		// SSE 重放需要读 outbox；订阅授权需要 auth service。
		mods.Events.DB = gormDB
		mods.Events.Auth = authSvc
		// outbox → SSE dispatcher（architecture §19）。
		event.StartDispatcher(ctx, gormDB, hub, log, 500*time.Millisecond)
		// outbox 保留窗口清扫（S1 = 24h）。
		event.StartRetention(ctx, gormDB, log, time.Hour)
		// 幂等键保留窗口清理（同 24h）。
		idem.DB = gormDB
		idempotency.StartCleanup(ctx, gormDB, log, time.Hour)
		// 过期租约清扫（architecture §17）。
		taskMod.StartSweeper(ctx, 30*time.Second)
	} else {
		log.Warn("ASTRAL_DATABASE_DSN empty; running without storage (stub mode): protected endpoints will 401")
		// 桩模式也装配一个无 DB 的 service，保证路由可注册、行为可预期（查询会失败）。
		authSvc = auth.NewService(nil, log)
		mods.Auth.Svc = authSvc
		// 业务模块在桩模式不注册 DB 依赖 —— 路由仍注册（401 后才到 500），
		// 文档化的行为以有数据库模式为准。
		mods.Workspace = &workspace.Module{Auth: authSvc}
		taskMod := &task.Module{Auth: authSvc}
		mods.Task = taskMod
		mods.Message = &message.Module{Auth: authSvc, Tasks: taskMod}
		mods.Presence = &presence.Module{Auth: authSvc}
		mods.Events.Auth = authSvc // 订阅授权 fail closed：无库模式下查询失败 → 404/503
	}

	handler := app.NewRouter(cfg, log, db, mods)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// SSE 长连接：不设 WriteTimeout，IdleTimeout 兜底（见 config 注释）。
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
