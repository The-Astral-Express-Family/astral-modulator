package auth

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// TestRegisterBootstrapSeedsOrgMemory：冷启动注册（无 invite_code 的首账号
// 路径）在同一事务内种子组织记忆 workspace（M1 裁决，round 37）——
// slug=org-memory + 新 actor 的 owner membership + workspace.create audit。
func TestRegisterBootstrapSeedsOrgMemory(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()

	actor, _, err := s.Register(ctx, RegisterInput{
		Email: "human@example.com", Password: "hunter2safe", DisplayName: "Hime",
	}, "ip", "ua")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	var ws model.Workspace
	if err := s.DB.WithContext(ctx).Where("slug = ?", orgMemorySlug).First(&ws).Error; err != nil {
		t.Fatalf("org-memory workspace missing: %v", err)
	}
	if ws.Name != "Organization Memory" {
		t.Fatalf("name = %q, want %q", ws.Name, "Organization Memory")
	}
	if ws.CreatedBy != actor.ID {
		t.Fatalf("created_by = %q, want new actor %q", ws.CreatedBy, actor.ID)
	}

	var member model.WorkspaceMember
	if err := s.DB.WithContext(ctx).
		Where("workspace_id = ? AND actor_id = ?", ws.ID, actor.ID).
		First(&member).Error; err != nil {
		t.Fatalf("owner membership missing: %v", err)
	}
	if member.Role != "owner" {
		t.Fatalf("role = %q, want owner", member.Role)
	}

	var audits int64
	if err := s.DB.WithContext(ctx).Model(&model.AuditEntry{}).
		Where("workspace_id = ? AND action = ? AND outcome = ?", ws.ID, "workspace.create", "allowed").
		Count(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("workspace.create audit entries = %d, want 1", audits)
	}
}

// TestRegisterWithInviteDoesNotSeedOrgMemory：种子只在冷启动分支——
// 邀请兑换注册不得触碰 org-memory。
func TestRegisterWithInviteDoesNotSeedOrgMemory(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()

	// 冷启动建首号（种子发生在此），再为另一 workspace 造可用邀请。
	first, _, err := s.Register(ctx, RegisterInput{Email: "first@example.com", Password: "hunter2safe"}, "ip", "ua")
	if err != nil {
		t.Fatal(err)
	}
	ws := model.Workspace{ID: "ws_inv", Name: "inv", Slug: "inv", CreatedBy: first.ID}
	if err := s.DB.Create(&ws).Error; err != nil {
		t.Fatal(err)
	}
	code, err := NewInviteCode()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Create(&model.Invitation{
		ID: "inv_orgmem", WorkspaceID: ws.ID, Role: "contributor",
		CodeHash: HashToken(NormalizeInviteCode(code)), CreatedBy: first.ID,
		Status: "invited", ExpiresAt: time.Now().Add(24 * time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Register(ctx, RegisterInput{
		Email: "invitee@example.com", Password: "hunter2safe", InviteCode: code,
	}, "ip", "ua"); err != nil {
		t.Fatalf("invite register: %v", err)
	}

	var seeds int64
	if err := s.DB.WithContext(ctx).Model(&model.Workspace{}).
		Where("slug = ?", orgMemorySlug).Count(&seeds).Error; err != nil {
		t.Fatal(err)
	}
	if seeds != 1 {
		t.Fatalf("org-memory workspaces = %d, want exactly 1 (cold start only)", seeds)
	}
}

// TestSeedOrgMemorySkipsExisting：已存在同 slug workspace 时跳过（防御分支），
// 不重复建 workspace，也不给第二个 actor 造 membership。
func TestSeedOrgMemorySkipsExisting(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()

	var first model.Workspace
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := seedOrgMemory(tx, "usr_first"); err != nil {
			return err
		}
		return tx.Where("slug = ?", orgMemorySlug).First(&first).Error
	})
	if err != nil {
		t.Fatalf("first seed: %v", err)
	}

	if err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return seedOrgMemory(tx, "usr_second")
	}); err != nil {
		t.Fatalf("second seed: %v", err)
	}

	var count int64
	if err := s.DB.WithContext(ctx).Model(&model.Workspace{}).
		Where("slug = ?", orgMemorySlug).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("org-memory workspaces = %d, want 1", count)
	}
	var second int64
	if err := s.DB.WithContext(ctx).Model(&model.WorkspaceMember{}).
		Where("workspace_id = ? AND actor_id = ?", first.ID, "usr_second").Count(&second).Error; err != nil {
		t.Fatal(err)
	}
	if second != 0 {
		t.Fatal("skip branch must not create membership for second actor")
	}
}
