// 锁定 Migrate 使用的 (embed FS, migrationsDir) 组合：
// goose 必须能从嵌入 FS 收集到全部 SQL migration。
// CI 回归：目录名与 embed 根不一致时报 "migrations directory does not exist"，
// 该错误在收集阶段即发生，无需真实数据库即可复现，故在此用 CollectMigrations 守住。
package store

import (
	"math"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/The-Astral-Express-Family/astral-modulator/server/migrations"
)

func TestEmbeddedMigrationsDiscoverable(t *testing.T) {
	goose.SetBaseFS(migrations.FS)
	found, err := goose.CollectMigrations(migrationsDir, 0, math.MaxInt64)
	if err != nil {
		t.Fatalf("collect migrations from embed FS dir %q: %v", migrationsDir, err)
	}
	if len(found) < 10 {
		t.Errorf("expected >=10 embedded migrations, got %d", len(found))
	}
}
