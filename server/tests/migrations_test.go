// migration_test.go 验证 goose SQL 在真实 PostgreSQL 上可从零跑通，且建出的
// 列与 GORM 模型期望一致（model.go 头注释的不变量）。本地无数据库时自动跳过；
// CI 的 server job 提供 postgres service 并注入 DSN。
package tests

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/store"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/testsupport"
)

// migratedPostgres 打开 DSN 指向的库并从零跑一遍 goose up，返回 database/sql
// 与 gorm 两个句柄（同一实例）。DSN 缺失时跳过。
func migratedPostgres(t *testing.T) (*sql.DB, *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("ASTRAL_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ASTRAL_TEST_POSTGRES_DSN not set; skipping postgres migration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	if sqlDB, err := gdb.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	return db, gdb
}

func TestMigrationsUpFromScratch(t *testing.T) {
	db, _ := migratedPostgres(t)
	// 幂等：二次 up 不得报错。
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate up (idempotent): %v", err)
	}
	// 关键表存在性抽查。
	for _, table := range []string{
		"server_meta", "human_auth", "actors", "workspaces", "workspace_members",
		"device_authorizations", "sessions", "credentials",
		"tasks", "task_leases", "tags", "tag_proposals",
		"documents", "document_conflicts", "document_versions", "outbox", "audit_log", "presence", "messages",
	} {
		var exists bool
		if err := db.QueryRowContext(ctx,
			"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)", table,
		).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("table %q missing after migrate", table)
		}
	}
}

// TestModelColumnsMatchMigrations 钉死 model↔migration 列名一致性。sqlite
// 单测的 AutoMigrate 按模型建列（方向相反），永远发现不了「迁移列名 ≠ 模型
// 列名」的漂移（2d75f6b：idempotency_keys 的 response_body vs body，生产 PG
// 上 INSERT 静默 42703、幂等重放完全不生效）。这里在真实迁移建出的 schema
// 上逐模型逐列核对，表名与列名一并对齐。
func TestModelColumnsMatchMigrations(t *testing.T) {
	_, gdb := migratedPostgres(t)
	for _, m := range testsupport.AllModels() {
		stmt := &gorm.Statement{DB: gdb}
		if err := stmt.Parse(m); err != nil {
			t.Fatalf("parse %T: %v", m, err)
		}
		for _, field := range stmt.Schema.Fields {
			if field.DBName == "" {
				continue
			}
			if !gdb.Migrator().HasColumn(m, field.DBName) {
				t.Errorf("%s: model column %q (field %s) missing in migrated schema - model/migration drift",
					stmt.Schema.Table, field.DBName, field.Name)
			}
		}
	}
}
