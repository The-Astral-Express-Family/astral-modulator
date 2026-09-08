package tag

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/event"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/testsupport"
)

type fixture struct {
	m     *Module
	db    *gorm.DB
	svc   *auth.Service
	wsID  string
	human *model.Actor
	p     *auth.Principal
}

func setup(t *testing.T) *fixture {
	t.Helper()
	db := testsupport.NewTestDB(t)
	svc := auth.NewService(db, discardLogger())
	m := &Module{DB: db, Auth: svc}

	human := &model.Actor{ID: "usr_h1", Kind: "human", DisplayName: "H"}
	if err := db.Create(human).Error; err != nil {
		t.Fatal(err)
	}
	wsID := "ws_t1"
	if err := db.Create(&model.Workspace{ID: wsID, Name: "demo", Slug: "demo", CreatedBy: human.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.WorkspaceMember{WorkspaceID: wsID, ActorID: human.ID, Role: "owner"}).Error; err != nil {
		t.Fatal(err)
	}
	return &fixture{m: m, db: db, svc: svc, wsID: wsID, human: human, p: &auth.Principal{ActorID: human.ID, Kind: "human"}}
}

func propose(t *testing.T, f *fixture, action, name string) (model.TagProposal, string) {
	t.Helper()
	normalized, ok := ValidateTagName(name)
	if !ok {
		t.Fatalf("invalid test name %q", name)
	}
	code, err := NewConfirmCode()
	if err != nil {
		t.Fatal(err)
	}
	proposal := model.TagProposal{
		ID: "tgp_" + action + "_" + name, WorkspaceID: f.wsID, ActorID: f.human.ID,
		Action: action, CanonicalName: normalized,
		ConfirmCodeHash: auth.HashToken(code), Status: "pending",
		ExpiresAt: time.Now().Add(proposalTTL), CreatedAt: time.Now(),
	}
	if err := f.db.Create(&proposal).Error; err != nil {
		t.Fatal(err)
	}
	return proposal, code
}

func TestNormalizeName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  Backend  ", "backend"},
		{"ＬａｔｅＸ", "latex"}, // 全角 → NFC 折叠后小写
		{"Café", "café"},   // NFC 组合字符稳定
	}
	for _, c := range cases {
		if got := NormalizeName(c.in); got != c.want {
			t.Errorf("NormalizeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if _, ok := ValidateTagName("   "); ok {
		t.Error("blank name should be invalid")
	}
	if _, ok := ValidateTagName("bad\x00name"); ok {
		t.Error("control chars should be invalid")
	}
}

func TestConfirmCodeShape(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		code, err := NewConfirmCode()
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 6 {
			t.Fatalf("code length: %q", code)
		}
		for _, c := range code {
			switch c {
			case '0', 'O', '1', 'I', 'L':
				t.Fatalf("ambiguous char in %q", code)
			}
		}
		if seen[code] {
			t.Fatalf("duplicate code %q", code)
		}
		seen[code] = true
	}
}

func TestCreateFlow(t *testing.T) {
	f := setup(t)
	proposal, code := propose(t, f, "create", "backend")

	// 正确确认 → tag 创建 + confirmed。
	tagRow, err := f.m.Confirm(context.Background(), f.p, proposal.ID, code, "backend")
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if tagRow.Name != "backend" || tagRow.NormalizedName != "backend" {
		t.Fatalf("tag: %+v", tagRow)
	}

	// 重放 confirm（单次使用）→ 409。
	if _, err := f.m.Confirm(context.Background(), f.p, proposal.ID, code, "backend"); err == nil {
		t.Fatal("replayed confirm should fail")
	}

	// 规范化唯一性：直接插规范化重名行必须被约束拒绝。
	dup := model.Tag{ID: "tag_x1", WorkspaceID: f.wsID, Name: "Backend", NormalizedName: "backend", CreatedBy: f.human.ID}
	if err := f.db.Create(&dup).Error; err == nil {
		t.Fatal("normalized-name uniqueness not enforced")
	}
}

func TestConfirmRejectsWrongCodeOrName(t *testing.T) {
	f := setup(t)
	proposal, code := propose(t, f, "create", "api")

	if _, err := f.m.Confirm(context.Background(), f.p, proposal.ID, "000000", "api"); err == nil {
		t.Fatal("wrong code should fail")
	}
	if _, err := f.m.Confirm(context.Background(), f.p, proposal.ID, code, "other"); err == nil {
		t.Fatal("mismatched name should fail")
	}
	// 正确 code + name 仍可确认（失败尝试不消耗单次使用）。
	if _, err := f.m.Confirm(context.Background(), f.p, proposal.ID, code, "api"); err != nil {
		t.Fatalf("confirm after failures: %v", err)
	}
}

func TestConfirmExpired(t *testing.T) {
	f := setup(t)
	proposal, code := propose(t, f, "create", "old")
	f.db.Model(&model.TagProposal{}).Where("id = ?", proposal.ID).
		Update("expires_at", time.Now().Add(-time.Second))

	if _, err := f.m.Confirm(context.Background(), f.p, proposal.ID, code, "old"); err == nil {
		t.Fatal("expired proposal should fail")
	}
}

func TestRenameAndDeleteFlow(t *testing.T) {
	f := setup(t)
	tagRow := model.Tag{ID: "tag_r1", WorkspaceID: f.wsID, Name: "old-name", NormalizedName: "old-name", CreatedBy: f.human.ID}
	if err := f.db.Create(&tagRow).Error; err != nil {
		t.Fatal(err)
	}

	// rename。
	proposal, code := propose(t, f, "rename", "new-name")
	proposal.TargetTagID = &tagRow.ID
	f.db.Save(&proposal)
	renamed, err := f.m.Confirm(context.Background(), f.p, proposal.ID, code, "new-name")
	if err != nil {
		t.Fatalf("rename confirm: %v", err)
	}
	if renamed.Name != "new-name" {
		t.Fatalf("renamed: %+v", renamed)
	}

	// delete。
	proposal2, code2 := propose(t, f, "delete", "new-name")
	proposal2.TargetTagID = &tagRow.ID
	f.db.Save(&proposal2)
	if _, err := f.m.Confirm(context.Background(), f.p, proposal2.ID, code2, "new-name"); err != nil {
		t.Fatalf("delete confirm: %v", err)
	}
	var count int64
	f.db.Model(&model.Tag{}).Where("id = ?", tagRow.ID).Count(&count)
	if count != 0 {
		t.Fatal("tag not deleted")
	}
}

func TestEventsEmittedViaOutbox(t *testing.T) {
	f := setup(t)
	hub := event.NewHub()
	events, unsub := hub.Subscribe(nil)
	defer unsub()

	proposal, code := propose(t, f, "create", "evtag")
	if _, err := f.m.Confirm(context.Background(), f.p, proposal.ID, code, "evtag"); err != nil {
		t.Fatal(err)
	}
	// 事件经 outbox 同事务写入，手动投递一次到 hub。
	event.PollOnce(context.Background(), f.db, hub, discardLogger())

	select {
	case env := <-events:
		if env.Type != event.TypeTagCreated {
			t.Fatalf("unexpected event: %s", env.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no tag.created event")
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
