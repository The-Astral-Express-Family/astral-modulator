// Package store 负责数据库连接与 migration 执行。
// GORM 只做运行时 ORM；生产 schema 管理是 goose，禁止 AutoMigrate（ADR/architecture §18）。
package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/The-Astral-Express-Family/astral-modulator/server/migrations"
)

// Open 建立连接并执行 goose up。
// 返回的 DB 为 nil 仅当 dsn 为空（无数据库开发模式，由调用方处理）。
func Open(ctx context.Context, dsn string, autoMigrate bool, log *slog.Logger) (*gorm.DB, error) {
	if dsn == "" {
		return nil, nil
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Warn),
		// TODO(phase-3): 任务搜索用参数化 SQL repository（recursive CTE / trigram），
		// 届时准备 statement_timeout 会话参数（architecture §13）。
	})
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	if autoMigrate {
		if err := Migrate(ctx, sqlDB); err != nil {
			return nil, err
		}
	}
	log.Info("postgres connected", "auto_migrate", autoMigrate)
	return db, nil
}

// Migrate 以嵌入的 goose migration 执行 up。幂等。
func Migrate(ctx context.Context, db *sql.DB) error {
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	// TODO(phase-6): 多实例部署改为部署流程显式执行 + 启动时仅校验版本兼容（deployment §14）。
	return goose.UpContext(ctx, db, "migrations")
}

// Ping 供 /readyz 判断数据库可用性。
func Ping(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return sqlDB.PingContext(pctx)
}
