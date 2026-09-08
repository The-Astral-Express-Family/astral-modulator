// migration_test.go 验证 goose SQL 在真实 PostgreSQL 上可从零跑通。
// 本地无数据库时自动跳过；CI 的 server job 提供 postgres service 并注入 DSN。
package tests

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/store"
)

func TestMigrationsUpFromScratch(t *testing.T) {
	dsn := os.Getenv("ASTRAL_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ASTRAL_TEST_POSTGRES_DSN not set; skipping postgres migration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	// 幂等：二次 up 不得报错。
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate up (idempotent): %v", err)
	}
	// 关键表存在性抽查。
	for _, table := range []string{
		"server_meta", "human_auth", "actors", "workspaces", "workspace_members",
		"device_authorizations", "sessions", "credentials",
		"tasks", "task_leases", "tags", "tag_proposals",
		"documents", "document_conflicts", "outbox", "audit_log", "presence", "messages",
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
