// Package testsupport 提供测试用数据库。
// 仅测试使用 sqlite（纯 Go 驱动）+ AutoMigrate；生产 schema 管理仍是 goose
// （PostgreSQL），两者行为差异由 CI 的 postgres job 兜底（TODO(phase-2)）。
// 业务逻辑已按可移植约束编写（TODO.md D5）。
package testsupport

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// NewTestDB 返回独立文件型 sqlite 库（每测试隔离），已完成 AutoMigrate。
func NewTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// busy_timeout：auth 的异步 last_used_at UPDATE 与业务事务会短暂抢写锁，
	// 让等待方排队而非报 SQLITE_BUSY（与生产 PG 的锁等待语义对齐）。
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "test.db")) + "?_pragma=busy_timeout(5000)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.ServerMeta{}, &model.Actor{}, &model.HumanAuth{},
		&model.Workspace{}, &model.WorkspaceMember{},
		&model.DeviceAuthorization{}, &model.Session{}, &model.Credential{},
		&model.Task{}, &model.TaskLease{},
		&model.Tag{}, &model.TagProposal{}, &model.TaskTag{},
		&model.Document{}, &model.DocumentConflict{},
		&model.OutboxEvent{}, &model.AuditEntry{},
		&model.Presence{}, &model.Message{},
		&model.IdempotencyKey{}, &model.Approval{}, &model.Invitation{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	// Windows 下文件被连接池占用会导致 TempDir 清理失败；显式关闭。
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	return db
}
