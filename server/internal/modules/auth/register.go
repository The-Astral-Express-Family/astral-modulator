// 注册轨的兑换面（ADR-0009 双轨分离）：bootstrap 零号注册与注册码兑换
// 建号。与工作区轨对称——签发/列表/撤销在 admin/invitation.go，兑换在本
// 文件；工作区轨全流程在 workspace/invitation.go。两轨共用件（码格式/
// 归一化/哈希入口、TTL、失效哨兵、链接拼装）见 invites.go。
package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ids"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/audit"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/outbox"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/store"
)

type RegisterInput struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	// RegistrationCode 非空走注册码兑换分支（ADR-0009）；为空保持
	// bootstrap-only 守卫（服务器已有 human 即 403）。工作区邀请码不是
	// 注册资格：在本端点出现（或任何未知码）一律 ErrInviteInvalid，
	// 与失效码同文案（防探测）。
	RegistrationCode string `json:"registration_code,omitempty"`
}

// Register 注册 human 账号并建立 web 会话，返回 actor 与 refresh token
// （refresh 进 HttpOnly Cookie，由 HTTP 层写入，与 login 同管线）。
func (s *Service) Register(ctx context.Context, in RegisterInput, ip, ua string) (*model.Actor, string, error) {
	if _, dbErr := s.dbOrError(); dbErr != nil {
		return nil, "", dbErr
	}
	var ok bool
	if in.Email, ok = NormalizeEmail(in.Email); !ok {
		return nil, "", httpx.Invalid("invalid email")
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		in.DisplayName = strings.SplitN(in.Email, "@", 2)[0]
	}
	if in.RegistrationCode != "" {
		return s.registerWithRegistrationCode(ctx, in, ip, ua)
	}
	return s.registerBootstrap(ctx, in, ip, ua)
}

// registerBootstrap 是冷启动的「零号邀请」（A5）：仅当服务器还没有任何 human。
func (s *Service) registerBootstrap(ctx context.Context, in RegisterInput, ip, ua string) (*model.Actor, string, error) {
	var humans int64
	if err := s.DB.WithContext(ctx).Model(&model.Actor{}).Where("kind = ?", "human").Count(&humans).Error; err != nil {
		return nil, "", err
	}
	if humans > 0 {
		return nil, "", &httpx.APIError{Status: 403, Code: httpx.CodeInsufficientScope, Message: "registration closed: initial human already exists"}
	}
	hash, apiErr := hashPasswordOrInvalid(in.Password)
	if apiErr != nil {
		return nil, "", apiErr
	}
	// 冷启动首个 human 即平台 admin（bootstrap 向导与 web 零号邀请同管线的
	// 唯一 admin 授予点；后续提升走 admin 用户管理，round 34）。
	actor := &model.Actor{ID: ids.New(ids.User), Kind: "human", PlatformRole: "admin", DisplayName: in.DisplayName}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(actor).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.HumanAuth{ActorID: actor.ID, Email: in.Email, PasswordHash: hash}).Error; err != nil {
			return err
		}
		return seedOrgMemory(tx, actor.ID)
	})
	if err != nil {
		return nil, "", err
	}
	refresh, _, _, err := s.createSession(ctx, actor.ID, "web", ip, ua)
	if err != nil {
		return nil, "", err
	}
	s.Log.Info("bootstrap human registered", "actor_id", actor.ID)
	return actor, refresh, nil
}

// orgMemorySlug 是组织记忆保留 workspace 的 slug（M1 裁决，round 37；
// memory 模块注释是该裁决的活文档锚点）。
const orgMemorySlug = "org-memory"

// seedOrgMemory 在冷启动注册事务内种子组织记忆 workspace（M1 裁决）：
// slug=org-memory 不存在则创建（name "Organization Memory"，created_by=
// 新 actor）并为其建 owner membership，照 workspace 创建惯例落 audit。
// 「已有 human 即 403」守卫在前保证无并发竞争（slug 唯一索引兜底残余窗口）；
// 刻意不进 goose migration——workspaces.created_by NOT NULL，migration 期
// 没有可引用的 actor（TODO.md §0.1-3）。
func seedOrgMemory(tx *gorm.DB, actorID string) error {
	var existing model.Workspace
	err := tx.Where("slug = ?", orgMemorySlug).First(&existing).Error
	if err == nil {
		return nil // 已种子：防御分支（守卫保证正常只走到这里一次）
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	ws := model.Workspace{ID: ids.New(ids.Workspace), Name: "Organization Memory", Slug: orgMemorySlug, CreatedBy: actorID}
	if err := tx.Create(&ws).Error; err != nil {
		return err
	}
	if err := tx.Create(&model.WorkspaceMember{WorkspaceID: ws.ID, ActorID: actorID, Role: "owner"}).Error; err != nil {
		return err
	}
	return audit.RecordInTx(tx, audit.Entry{
		WorkspaceID: ws.ID, ActorID: actorID,
		Action: "workspace.create", Outcome: "allowed",
		TargetType: "workspace", TargetID: ws.ID,
	})
}

// registerWithRegistrationCode 是注册码兑换注册（ADR-0009 双轨分离）：
// 建号 + 条件更新抢状态 + 审计/事件同事务，**不建任何 WorkspaceMember**——
// 注册资格与入伙资格是两条轨道，入伙走 workspace 模块的
// POST /invitations/redeem。只查 registration_invitations：工作区码在
// 注册端点出现与未知码同待遇，统一 ErrInviteInvalid（防探测，同码同文案）。
// 码校验先于口令策略；email 撞车由唯一索引兜底（此时注册码不消耗，
// 可换邮箱重试）。
func (s *Service) registerWithRegistrationCode(ctx context.Context, in RegisterInput, ip, ua string) (*model.Actor, string, error) {
	codeHash := InviteCodeHash(in.RegistrationCode)
	var inv model.RegistrationInvitation
	err := s.DB.WithContext(ctx).Where("code_hash = ?", codeHash).First(&inv).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", ErrInviteInvalid
	}
	if err != nil {
		return nil, "", err
	}
	if inv.Status != "invited" || time.Now().After(inv.ExpiresAt) {
		return nil, "", ErrInviteInvalid
	}
	hash, apiErr := hashPasswordOrInvalid(in.Password)
	if apiErr != nil {
		return nil, "", apiErr
	}

	actor := &model.Actor{ID: ids.New(ids.User), Kind: "human", PlatformRole: "user", DisplayName: in.DisplayName}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(actor).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.HumanAuth{ActorID: actor.ID, Email: in.Email, PasswordHash: hash}).Error; err != nil {
			return err
		}
		// 条件更新抢状态（tag confirm 验证过的模式）：并发同码只有一个
		// 事务成功，输家整体回滚（actor 不残留）。
		res := tx.Model(&model.RegistrationInvitation{}).
			Where("id = ? AND status = 'invited'", inv.ID).
			Updates(map[string]any{"status": "redeemed", "redeemed_by": actor.ID, "redeemed_at": time.Now()})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrInviteInvalid
		}
		// 服务器级审计：workspace_id 留空（/admin/audit 可见）。
		if err := audit.RecordInTx(tx, audit.Entry{
			ActorID: actor.ID,
			Action:  "invite.redeem", Outcome: "allowed",
			TargetType: "invitation", TargetID: inv.ID,
			Details: map[string]any{"scope": "registration"},
		}); err != nil {
			return err
		}
		if err := audit.RecordInTx(tx, audit.Entry{
			ActorID: actor.ID,
			Action:  "auth.register", Outcome: "allowed",
			TargetType: "actor", TargetID: actor.ID,
		}); err != nil {
			return err
		}
		return outbox.EmitTx(tx, outbox.TypeSecurityInviteRedeemed, "", actor.ID, 0, map[string]any{
			"invitation_id": inv.ID, "scope": "registration",
		})
	})
	if err != nil {
		if errors.Is(err, ErrInviteInvalid) {
			return nil, "", ErrInviteInvalid
		}
		if store.IsUniqueViolation(err) {
			return nil, "", httpx.Conflict(httpx.CodeEmailTaken, "email already registered")
		}
		return nil, "", err
	}
	refresh, _, _, err := s.createSession(ctx, actor.ID, "web", ip, ua)
	if err != nil {
		return nil, "", err
	}
	s.Log.Info("human registered via registration invite", "actor_id", actor.ID)
	return actor, refresh, nil
}
