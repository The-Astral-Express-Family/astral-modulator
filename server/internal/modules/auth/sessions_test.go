package auth

import (
	"context"
	"testing"
	"time"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// 设备管理（sessions.go）的 service 层测试：列表过滤/排序/当前标记、
// 单会话注销的属主与幂等边界、last_used_at 节流更新。

func TestListSessions(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	actorA, _, err := s.Register(ctx, RegisterInput{Email: "a@example.com", Password: "hunter2safe"}, "ip-a", "ua-web")
	if err != nil {
		t.Fatalf("register A: %v", err)
	}
	// 第二个 web 会话 + 一个 cli 会话（设备）。
	if _, _, err := s.Login(ctx, "a@example.com", "hunter2safe", "ip-a2", "ua-web2"); err != nil {
		t.Fatalf("login A: %v", err)
	}
	_, _, cliSess, err := s.createSession(ctx, actorA.ID, "cli", "ip-cli", "astral-cli/0.1.0")
	if err != nil {
		t.Fatalf("create cli session: %v", err)
	}
	// 他人的会话不得出现在列表里（bootstrap 只收首个 human，第二个 actor 直插表）。
	actorB := &model.Actor{ID: "usr_other", Kind: "human", PlatformRole: "user", DisplayName: "Other"}
	if err := s.DB.Create(actorB).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.createSession(ctx, actorB.ID, "web", "ip-b", "ua-b"); err != nil {
		t.Fatalf("create B session: %v", err)
	}

	views, err := s.ListSessions(ctx, actorA.ID, cliSess.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(views) != 3 {
		t.Fatalf("want 3 sessions for A, got %d: %+v", len(views), views)
	}
	var cliView *SessionView
	for i := range views {
		if views[i].ID == cliSess.ID {
			cliView = &views[i]
		} else if views[i].Current {
			t.Fatalf("non-current session marked current: %+v", views[i])
		}
		if views[i].UserAgent == "" {
			t.Fatalf("user_agent should be echoed verbatim: %+v", views[i])
		}
	}
	if cliView == nil || !cliView.Current || cliView.ClientType != "cli" {
		t.Fatalf("cli session view: %+v", cliView)
	}

	// 已撤销 / 已过期不入列。
	revokedAt := time.Now()
	if err := s.DB.Model(&model.Session{}).Where("actor_id = ? AND client_type = 'web'", actorA.ID).
		Update("revoked_at", revokedAt).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&model.Session{}).Where("actor_id = ? AND id <> ?", actorA.ID, cliSess.ID).
		Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	views, err = s.ListSessions(ctx, actorA.ID, cliSess.ID)
	if err != nil {
		t.Fatalf("list after revoke/expire: %v", err)
	}
	if len(views) != 1 || views[0].ID != cliSess.ID {
		t.Fatalf("want only cli session, got %+v", views)
	}

	// 排序：最近使用优先（last_used_at DESC）。
	past := time.Now().Add(-time.Hour)
	if err := s.DB.Model(&model.Session{}).Where("id = ?", cliSess.ID).Update("last_used_at", past).Error; err != nil {
		t.Fatal(err)
	}
	recent := time.Now()
	if _, _, _, err := s.createSession(ctx, actorA.ID, "cli", "ip-cli2", "astral-cli/0.2.0"); err != nil {
		t.Fatal(err)
	}
	var fresh model.Session
	if err := s.DB.Where("actor_id = ? AND remote_addr = 'ip-cli2'", actorA.ID).First(&fresh).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&model.Session{}).Where("id = ?", fresh.ID).Update("last_used_at", recent).Error; err != nil {
		t.Fatal(err)
	}
	views, err = s.ListSessions(ctx, actorA.ID, "")
	if err != nil {
		t.Fatalf("list ordering: %v", err)
	}
	if len(views) != 2 || views[0].ID != fresh.ID {
		t.Fatalf("want recently-used first, got %+v", views)
	}
}

func TestRevokeSession(t *testing.T) {
	s := newSvc(t)
	var notified []string
	s.OnRevoke = func(actorID string) { notified = append(notified, actorID) }
	ctx := context.Background()
	actorA, _, err := s.Register(ctx, RegisterInput{Email: "a@example.com", Password: "hunter2safe"}, "ip", "ua")
	if err != nil {
		t.Fatalf("register A: %v", err)
	}
	// bootstrap 只收首个 human，第二个 actor 直插表（仅需 id 参与属主判定）。
	actorB := &model.Actor{ID: "usr_other", Kind: "human", PlatformRole: "user", DisplayName: "Other"}
	if err := s.DB.Create(actorB).Error; err != nil {
		t.Fatal(err)
	}
	_, cliRefresh, cliSess, err := s.createSession(ctx, actorA.ID, "cli", "ip", "astral-cli/0.1.0")
	if err != nil {
		t.Fatal(err)
	}

	// 正常注销：断流回调 + 列表移除 + 该会话凭证即刻失效。
	if err := s.RevokeSession(ctx, actorA.ID, cliSess.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if len(notified) != 1 || notified[0] != actorA.ID {
		t.Fatalf("notified = %v, want [%s]", notified, actorA.ID)
	}
	views, err := s.ListSessions(ctx, actorA.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range views {
		if v.ID == cliSess.ID {
			t.Fatalf("revoked session still listed: %+v", v)
		}
	}
	if _, err := s.Refresh(ctx, cliRefresh, "ip", "ua"); err == nil {
		t.Fatal("refresh with revoked session should fail")
	}

	// 重复注销 / 未知 id / 非本人：一律 404（防枚举，同 RevokeCredential 口径）。
	for _, tc := range []struct {
		name      string
		actorID   string
		sessionID string
	}{
		{"already revoked", actorA.ID, cliSess.ID},
		{"unknown id", actorA.ID, "ses_unknown"},
		{"other actor's session", actorB.ID, cliSess.ID},
	} {
		err := s.RevokeSession(ctx, tc.actorID, tc.sessionID)
		if apiErr, ok := err.(*httpx.APIError); !ok || apiErr.Status != 404 {
			t.Fatalf("%s: want 404, got %v", tc.name, err)
		}
	}
}

func TestTouchSessionLastUsed(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	actor, _, err := s.Register(ctx, RegisterInput{Email: "a@example.com", Password: "hunter2safe"}, "ip", "ua")
	if err != nil {
		t.Fatal(err)
	}
	refresh, access, sess, err := s.createSession(ctx, actor.ID, "web", "ip", "ua")
	if err != nil {
		t.Fatal(err)
	}
	_ = refresh

	// access 认证触发节流更新（同步）：resolve 返回即已落库。
	if _, apiErr := s.ResolvePrincipal(ctx, access, ""); apiErr != nil {
		t.Fatalf("resolve access: %v", apiErr)
	}
	var touched model.Session
	if err := s.DB.First(&touched, "id = ?", sess.ID).Error; err != nil {
		t.Fatal(err)
	}
	if touched.LastUsedAt == nil {
		t.Fatal("last_used_at should be touched after access auth")
	}

	// 一分钟内重复认证不再写（节流窗口）。
	if _, apiErr := s.ResolvePrincipal(ctx, access, ""); apiErr != nil {
		t.Fatalf("resolve access again: %v", apiErr)
	}
	var after model.Session
	if err := s.DB.First(&after, "id = ?", sess.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !touched.LastUsedAt.Equal(*after.LastUsedAt) {
		t.Fatalf("throttled touch should not update within 1min: %v -> %v", touched.LastUsedAt, after.LastUsedAt)
	}
}
