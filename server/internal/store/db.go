// Package store 负责数据库连接与 migration 执行。
// GORM 只做运行时 ORM；生产 schema 管理是 goose，禁止 AutoMigrate（ADR/architecture §18）。
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ids"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
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

// EnsureServerID 读取/固化稳定 server_id（architecture §7：一经生成永久不变）。
// 优先级：库中已有值 > env 注入（首启） > 生成新 ID。返回值即为运行期身份。
func EnsureServerID(ctx context.Context, db *gorm.DB, envID string) (string, error) {
	const key = "server_id"
	var row model.ServerMeta
	err := db.WithContext(ctx).Where("key = ?", key).First(&row).Error
	if err == nil {
		return row.Value, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	id := envID
	if id == "" {
		id = ids.New(ids.Server)
	}
	// 只按 key 查（struct 条件会把 Value 也算进去）；并发首启主键冲突时重读。
	row = model.ServerMeta{Key: key, Value: id}
	if err := db.WithContext(ctx).Where("key = ?", key).FirstOrCreate(&row).Error; err != nil {
		var fresh model.ServerMeta
		if e := db.WithContext(ctx).Where("key = ?", key).First(&fresh).Error; e != nil {
			return "", err
		}
		return fresh.Value, nil
	}
	return row.Value, nil
}
