package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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

// Service 承载全部认证/授权业务（device flow、session、credential、scope）。
// Handler 层薄；测试直接打 Service（sqlite 内存库）。
// 只承载 auth 领域的时长配置；task lease / presence TTL 归各自模块。
type Service struct {
	DB  *gorm.DB
	Log *slog.Logger

	AccessTTL  time.Duration // 默认 15m
	RefreshTTL time.Duration // 默认 30d（轮换滑动窗口）
	// MaxSessionLife 是 session 的绝对寿命上限（创建起算，D10）。
	// 必须大于 RefreshTTL 才有意义：否则轮换可无限续命，上限永不生效。
	MaxSessionLife time.Duration

	// OnRevoke 在凭证/会话被撤销后以 actorID 回调（装配层把它接到
	// Hub.DisconnectActor，实现 security.md 的「撤销后主动断流」）。
	// 可为 nil。
	OnRevoke        func(actorID string)
	DeviceTTL       time.Duration // 默认 10m
	DevicePollEvery time.Duration // CLI 轮询间隔约定，默认 3s
}

func NewService(db *gorm.DB, log *slog.Logger) *Service {
	return &Service{
		DB:              db,
		Log:             log,
		AccessTTL:       15 * time.Minute,
		RefreshTTL:      30 * 24 * time.Hour,
		MaxSessionLife:  90 * 24 * time.Hour, // D10：封顶被盗 refresh 的最长寿命
		DeviceTTL:       10 * time.Minute,
		DevicePollEvery: 3 * time.Second,
	}
}

// dbOrError：无数据库（桩模式）时返回 503，避免 nil 指针 panic。
func (s *Service) dbOrError() (*gorm.DB, *httpx.APIError) {
	if s.DB == nil {
		return nil, httpx.Unavailable("server running without storage (ASTRAL_DATABASE_DSN not set)")
	}
	return s.DB, nil
}

// ---- Principal 与授权 ----

// Principal 是请求解析出的操作者。
type Principal struct {
	ActorID string
	Kind    string // human | agent | service
	// PlatformRole 是平台全局角色（admin/user/agent/service），认证时从
	// actors.platform_role 解析；授权一律经 RequireGlobal 按最终 scope 计算。
	PlatformRole string
	// AuthKind: access_token | credential | session_cookie
	AuthKind string
	// SessionID 仅 human 会话时有值。
	SessionID string
	// Credential 仅 credential 认证时有值。
	Credential *model.Credential
}

func (p *Principal) IsHuman() bool { return p.Kind == "human" }

// IsPlatformAdmin 是展示/日志用的便捷判断；授权判定请走 RequireGlobal，
// 与「按最终 scope 计算」的哲学一致。
func (p *Principal) IsPlatformAdmin() bool { return p.PlatformRole == "admin" }

// WorkspaceScopes 计算主体在某 workspace 的生效 scope 集合：
//   - human：其成员角色的 scope bundle；
//   - agent/service：credential scopes（workspace 绑定为空则全服务器生效）。
func (s *Service) WorkspaceScopes(ctx context.Context, p *Principal, workspaceID string) (map[string]bool, error) {
	out := map[string]bool{}
	switch p.Kind {
	case "human":
		var member model.WorkspaceMember
		err := s.DB.WithContext(ctx).Where("workspace_id = ? AND actor_id = ?", workspaceID, p.ActorID).First(&member).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return out, nil // 非成员：空集合（调用方按 404/403 语义处理）
		}
		if err != nil {
			return nil, err
		}
		for _, sc := range RoleToScopes[member.Role] {
			out[sc] = true
		}
	default:
		if p.Credential == nil {
			return out, nil
		}
		if p.Credential.WorkspaceID != nil && *p.Credential.WorkspaceID != workspaceID {
			return out, nil // credential 绑定了其他 workspace
		}
		var scopes []string
		if err := json.Unmarshal([]byte(p.Credential.Scopes), &scopes); err != nil {
			return nil, fmt.Errorf("decode credential scopes: %w", err)
		}
		for _, sc := range scopes {
			out[sc] = true
		}
	}
	return out, nil
}

// HasScope 授权判断入口。missing 时返回 INSUFFICIENT_SCOPE。
func HasScope(scopes map[string]bool, scope string) *httpx.APIError {
	if scopes[scope] {
		return nil
	}
	return &httpx.APIError{Status: 403, Code: httpx.CodeInsufficientScope, Message: "missing scope: " + scope}
}

// RequireWorkspaceScopes 是 workspace 级端点的标准授权前置：
// 解析主体在该 workspace 的生效 scope，并依次给出统一语义——
// 非成员 / credential 未绑定 → 404 WORKSPACE_NOT_FOUND（不泄露存在性）；
// 成员但 scope 不足 → 403 INSUFFICIENT_SCOPE。
// 通过后返回 scope 集合（handler 可复用于次级判断）。
func (s *Service) RequireWorkspaceScopes(ctx context.Context, p *Principal, workspaceID string, need ...string) (map[string]bool, *httpx.APIError) {
	scopes, err := s.WorkspaceScopes(ctx, p, workspaceID)
	if err != nil {
		s.Log.Error("scope resolution failed", "actor_id", p.ActorID, "workspace_id", workspaceID, "err", err)
		return nil, httpx.Internal("scope resolution failed")
	}
	if len(scopes) == 0 {
		return nil, &httpx.APIError{Status: 404, Code: httpx.CodeWorkspaceNotFound, Message: "workspace not found"}
	}
	for _, sc := range need {
		if apiErr := HasScope(scopes, sc); apiErr != nil {
			return nil, apiErr
		}
	}
	return scopes, nil
}

// RequireWorkspace 是 workspace 级 HTTP 端点的授权前置便捷封装：
// 从请求上下文取 Principal 后调 RequireWorkspaceScopes（非成员 404 /
// scope 不足 403）。各模块 handler 直接调用，不再各自维护同名私有包装。
func RequireWorkspace(r *http.Request, svc *Service, workspaceID string, need ...string) *httpx.APIError {
	_, apiErr := svc.RequireWorkspaceScopes(r.Context(), PrincipalFrom(r.Context()), workspaceID, need...)
	return apiErr
}

// GlobalScopesForPrincipal 返回主体的平台级 scope 集合。平台角色随 Principal
// 在认证时解析（authActor），这里零查库。
func GlobalScopesForPrincipal(p *Principal) map[string]bool {
	out := map[string]bool{}
	for _, sc := range GlobalScopesFor[p.PlatformRole] {
		out[sc] = true
	}
	return out
}

// RequireGlobal 是平台级端点的授权前置（对齐 RequireWorkspaceScopes 的
// 403 INSUFFICIENT_SCOPE 语义）。平台资源不因无权而隐藏存在性，故无 404 分支；
// Principal 无全局 scope（如 user/agent/service）与缺具体 scope 同判。
func RequireGlobal(r *http.Request, need ...string) *httpx.APIError {
	scopes := GlobalScopesForPrincipal(PrincipalFrom(r.Context()))
	for _, sc := range need {
		if apiErr := HasScope(scopes, sc); apiErr != nil {
			return apiErr
		}
	}
	return nil
}

// ---- 注册 / 登录（human，web 侧；TODO.md D6/A5）----

type RegisterInput struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	// InviteCode 非空走邀请兑换分支（docs/registration.md §4）；为空保持
	// bootstrap-only 守卫（服务器已有 human 即 403）。
	InviteCode string `json:"invite_code,omitempty"`
}

// errInviteInvalid 是邀请码失效的统一错误：不存在/已兑换/已撤销/已过期
// 同码同文案，不给区分（防探测，docs/registration.md §3）。
var errInviteInvalid = &httpx.APIError{
	Status:  http.StatusBadRequest,
	Code:    httpx.CodeInviteInvalid,
	Message: "invite code is invalid or expired",
}

// Register 注册 human 账号并建立 web 会话，返回 actor 与 refresh token
// （refresh 进 HttpOnly Cookie，由 HTTP 层写入，与 login 同管线）。
func (s *Service) Register(ctx context.Context, in RegisterInput, ip, ua string) (*model.Actor, string, error) {
	if _, dbErr := s.dbOrError(); dbErr != nil {
		return nil, "", dbErr
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if in.Email == "" || !strings.Contains(in.Email, "@") {
		return nil, "", httpx.Invalid("invalid email")
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		in.DisplayName = strings.SplitN(in.Email, "@", 2)[0]
	}
	if in.InviteCode != "" {
		return s.registerWithInvite(ctx, in, ip, ua)
	}
	return s.registerBootstrap(ctx, in, ip, ua)
}

// hashPasswordOrInvalid 是注册两分支共用的口令策略闸：策略失败统一
// 400 VALIDATION_FAILED（err.Error 即人类可读原因）。
func hashPasswordOrInvalid(password string) (string, *httpx.APIError) {
	hash, err := HashPassword(password)
	if err != nil {
		return "", &httpx.APIError{Status: http.StatusBadRequest, Code: httpx.CodeValidationFailed, Message: err.Error()}
	}
	return hash, nil
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

// registerWithInvite 是邀请兑换注册（docs/registration.md §4）：
// 建号 + 条件更新邀请 + 入伙 + 审计/事件同事务。邀请码校验先于口令策略
// （失效码一律 errInviteInvalid，不泄露具体原因）；email 撞车由唯一索引
// 兜底（此时邀请不消耗，可换邮箱重试）。
func (s *Service) registerWithInvite(ctx context.Context, in RegisterInput, ip, ua string) (*model.Actor, string, error) {
	var inv model.Invitation
	err := s.DB.WithContext(ctx).
		Where("code_hash = ?", HashToken(NormalizeInviteCode(in.InviteCode))).
		First(&inv).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", errInviteInvalid
	}
	if err != nil {
		return nil, "", err
	}
	if inv.Status != "invited" || time.Now().After(inv.ExpiresAt) {
		return nil, "", errInviteInvalid
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
		res := tx.Model(&model.Invitation{}).
			Where("id = ? AND status = 'invited'", inv.ID).
			Updates(map[string]any{"status": "redeemed", "redeemed_by": actor.ID, "redeemed_at": time.Now()})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errInviteInvalid
		}
		if err := tx.Create(&model.WorkspaceMember{WorkspaceID: inv.WorkspaceID, ActorID: actor.ID, Role: inv.Role}).Error; err != nil {
			return err
		}
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: inv.WorkspaceID, ActorID: actor.ID,
			Action: "invite.redeem", Outcome: "allowed",
			TargetType: "invitation", TargetID: inv.ID,
			Details: map[string]any{"role": inv.Role},
		}); err != nil {
			return err
		}
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: inv.WorkspaceID, ActorID: actor.ID,
			Action: "auth.register", Outcome: "allowed",
			TargetType: "actor", TargetID: actor.ID,
		}); err != nil {
			return err
		}
		return outbox.EmitTx(tx, outbox.TypeSecurityInviteRedeemed, inv.WorkspaceID, actor.ID, 0, map[string]any{
			"invitation_id": inv.ID, "role": inv.Role,
		})
	})
	if err != nil {
		if errors.Is(err, errInviteInvalid) {
			return nil, "", errInviteInvalid
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
	s.Log.Info("human registered via invite", "actor_id", actor.ID, "workspace_id", inv.WorkspaceID)
	return actor, refresh, nil
}

// Login 校验本地口令并创建 web session，返回 refresh token（进 HttpOnly Cookie）。
func (s *Service) Login(ctx context.Context, email, password, ip, ua string) (refreshToken string, actor *model.Actor, err error) {
	if _, dbErr := s.dbOrError(); dbErr != nil {
		return "", nil, dbErr
	}
	email = strings.ToLower(strings.TrimSpace(email))
	var ha model.HumanAuth
	if e := s.DB.WithContext(ctx).Where("email = ?", email).First(&ha).Error; e != nil {
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			// 查库失败不能伪装成“凭证错误”（那会引导用户反复改密码）。
			s.Log.Error("login lookup failed", "err", e)
			return "", nil, httpx.Internal("login failed")
		}
		// 不区分“无此邮箱/口令错误”，避免枚举。
		return "", nil, &httpx.APIError{Status: 401, Code: httpx.CodeAuthRequired, Message: "invalid credentials"}
	}
	if !CheckPassword(password, ha.PasswordHash) {
		return "", nil, &httpx.APIError{Status: 401, Code: httpx.CodeAuthRequired, Message: "invalid credentials"}
	}
	var actorRow model.Actor
	if e := s.DB.WithContext(ctx).First(&actorRow, "id = ?", ha.ActorID).Error; e != nil {
		return "", nil, e
	}
	// 停用账号禁止登录（admin 用户管理，round 34）；文案不区分口令对错
	// 之外的状态细节。
	if actorRow.DisabledAt != nil {
		return "", nil, &httpx.APIError{Status: 401, Code: httpx.CodeTokenRevoked, Message: "account disabled"}
	}
	refresh, _, _, e := s.createSession(ctx, actorRow.ID, "web", ip, ua)
	if e != nil {
		return "", nil, e
	}
	return refresh, &actorRow, nil
}

// ---- Session（opaque access + rotating refresh）----

func (s *Service) createSession(ctx context.Context, actorID, clientType, ip, ua string) (refresh, access string, sess *model.Session, err error) {
	refresh, err = NewRefreshToken()
	if err != nil {
		return "", "", nil, err
	}
	access, err = NewAccessToken()
	if err != nil {
		return "", "", nil, err
	}
	now := time.Now()
	sess = &model.Session{
		ID:               ids.New(ids.Session),
		ActorID:          actorID,
		ClientType:       clientType,
		RefreshTokenHash: HashToken(refresh),
		FamilyID:         ids.New(ids.Session),
		AccessTokenHash:  HashToken(access),
		AccessExpiresAt:  now.Add(s.AccessTTL),
		ExpiresAt:        now.Add(s.RefreshTTL),
		UserAgent:        ua,
		RemoteAddr:       ip,
	}
	if err := s.DB.WithContext(ctx).Create(sess).Error; err != nil {
		return "", "", nil, err
	}
	return refresh, access, sess, nil
}

// TokenPair 是发给客户端的凭证组（web 只收 access；refresh 在 HttpOnly Cookie）。
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ActorID      string `json:"actor_id"`
}

// Refresh 轮换 refresh token（A2：access 每请求查库，见 authenticate）。
// 重放检测：命中 prev hash → 撤销整个 family + audit。
func (s *Service) Refresh(ctx context.Context, refreshToken, ip, ua string) (*TokenPair, error) {
	if _, dbErr := s.dbOrError(); dbErr != nil {
		return nil, dbErr
	}
	if refreshToken == "" {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeAuthRequired, Message: "missing refresh token"}
	}
	hash := HashToken(refreshToken)

	var sess model.Session
	err := s.DB.WithContext(ctx).Where("refresh_token_hash = ?", hash).First(&sess).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 可能是重放：查 prev。
		var reused model.Session
		if e := s.DB.WithContext(ctx).Where("prev_refresh_token_hash = ?", hash).First(&reused).Error; e == nil {
			s.revokeFamily(ctx, &reused, "refresh_token_replay")
			return nil, &httpx.APIError{Status: 401, Code: httpx.CodeTokenRevoked, Message: "refresh token reuse detected; session family revoked"}
		}
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeTokenRevoked, Message: "unknown refresh token"}
	}
	if err != nil {
		return nil, err
	}
	if sess.RevokedAt != nil {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeTokenRevoked, Message: "session revoked"}
	}
	if time.Now().After(sess.ExpiresAt) {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeTokenExpired, Message: "session expired"}
	}

	// 轮换：prev=旧，current=新；生命周期上限从创建时刻算。
	newRefresh, err := NewRefreshToken()
	if err != nil {
		return nil, err
	}
	newAccess, err := NewAccessToken()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	expires := now.Add(s.RefreshTTL)
	if max := sess.CreatedAt.Add(s.MaxSessionLife); expires.After(max) {
		expires = max
	}
	// D10：整族寿命已到头 → 明确拒绝，绝不签发「出生即过期」的轮换对。
	if !expires.After(now) {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeTokenExpired,
			Message: "session reached end of life; log in again"}
	}
	updates := map[string]any{
		"prev_refresh_token_hash": sess.RefreshTokenHash,
		"refresh_token_hash":      HashToken(newRefresh),
		"access_token_hash":       HashToken(newAccess),
		"access_expires_at":       now.Add(s.AccessTTL),
		"expires_at":              expires,
		"last_used_at":            now,
	}
	// 条件轮换：WHERE 里带旧 hash。并发双刷新时只有一方成功；
	// 失败方拿到的是刚被轮换掉的 token，按无效处理（不撤族——赢的那方
	// 是合法客户端，不能被并发输家连坐）。
	res := s.DB.WithContext(ctx).Model(&model.Session{}).
		Where("id = ? AND refresh_token_hash = ?", sess.ID, hash).Updates(updates)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeTokenRevoked, Message: "refresh token superseded by a newer one"}
	}
	return &TokenPair{
		AccessToken:  newAccess,
		TokenType:    "Bearer",
		ExpiresIn:    int(s.AccessTTL.Seconds()),
		RefreshToken: newRefresh,
		ActorID:      sess.ActorID,
	}, nil
}

func (s *Service) revokeFamily(ctx context.Context, sess *model.Session, reason string) {
	now := time.Now()
	err := s.DB.WithContext(ctx).Model(&model.Session{}).
		Where("family_id = ? AND revoked_at IS NULL", sess.FamilyID).
		Update("revoked_at", now).Error
	if err != nil {
		s.Log.Error("revoke family failed", "err", err)
	}
	s.Log.Warn("session family revoked", "reason", reason, "actor_id", sess.ActorID)
	s.notifyRevoked(sess.ActorID)
	// TODO: audit 记录撤销动作（集中登记见 audit/module.go 服务器级审计条目）。
}

// Logout 撤销 refresh token 对应的 session。
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	// logout 是公共端点：桩模式（无库）下不能像受保护端点那样被
	// Authenticate 挡住，必须显式防 nil。
	if _, dbErr := s.dbOrError(); dbErr != nil {
		return dbErr
	}
	res := s.DB.WithContext(ctx).Model(&model.Session{}).
		Where("refresh_token_hash = ? AND revoked_at IS NULL", HashToken(refreshToken)).
		Update("revoked_at", time.Now())
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		var sess model.Session
		if err := s.DB.WithContext(ctx).
			Where("refresh_token_hash = ?", HashToken(refreshToken)).First(&sess).Error; err == nil {
			s.notifyRevoked(sess.ActorID)
		}
	}
	return nil
}

// notifyRevoked 触发撤销回调（nil 安全）。
func (s *Service) notifyRevoked(actorID string) {
	if s.OnRevoke != nil {
		s.OnRevoke(actorID)
	}
}

// RevokeActorSessions 撤销主体的全部未撤销会话（平台停用账号用，round 34）。
// 与 revokeFamily 同语义但不限 family；真正的准入闸门是 authActor/Login 的
// DisabledAt 检查，这里只是把在途 access/refresh 立即作废并触发断流。
func (s *Service) RevokeActorSessions(ctx context.Context, actorID string) (int64, error) {
	res := s.DB.WithContext(ctx).Model(&model.Session{}).
		Where("actor_id = ? AND revoked_at IS NULL", actorID).
		Update("revoked_at", time.Now())
	if res.Error != nil {
		return 0, res.Error
	}
	if res.RowsAffected > 0 {
		s.notifyRevoked(actorID)
	}
	return res.RowsAffected, nil
}

// ActorDTO 是 actor 的公网形状（openapi Actor schema：id/kind/display_name/
// bio/avatar_url），全仓单一来源：workspace 模块的 member/agent 响应复用本
// 类型，不手写平行 DTO。直接序列化 model.Actor 会漏出大写字段名（E2E round 20
// 发现）；bio/avatar_url 恒为 string，空串 = 未设置。
type ActorDTO struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	DisplayName string `json:"display_name"`
	Bio         string `json:"bio"`
	AvatarURL   string `json:"avatar_url"`
}

func ToActorDTO(a model.Actor) ActorDTO {
	avatar := ""
	if a.AvatarURL != nil {
		avatar = *a.AvatarURL
	}
	return ActorDTO{ID: a.ID, Kind: a.Kind, DisplayName: a.DisplayName, Bio: a.Bio, AvatarURL: avatar}
}

// MeResponse 是 /auth/me、/auth/login、/auth/register 共用的响应体。
type MeResponse struct {
	Actor ActorDTO `json:"actor"`
	Email string   `json:"email,omitempty"` // human 本地登录邮箱；agent/service 省略
	// PlatformRole 是认证主体的平台角色（admin/user/agent/service，round 33）。
	// 与 ActorDTO 分离：member/agent 列表里的 actor 形状不携带平台角色。
	PlatformRole string       `json:"platform_role"`
	Session      *SessionInfo `json:"session,omitempty"`
}

type SessionInfo struct {
	ClientType string `json:"client_type"`
	ExpiresAt  string `json:"expires_at,omitempty"`
}

// ---- resolve principal：三种凭证来源 ----

// ResolvePrincipal 解析请求身份。顺序：Bearer access → Bearer credential → Cookie session。
// 返回 nil error 表示已认证。
func (s *Service) ResolvePrincipal(ctx context.Context, bearer, cookieRefresh string) (*Principal, *httpx.APIError) {
	if _, dbErr := s.dbOrError(); dbErr != nil {
		return nil, dbErr
	}
	if bearer != "" {
		if IsCredentialToken(bearer) {
			return s.authenticateCredential(ctx, bearer)
		}
		return s.authenticateAccessToken(ctx, bearer)
	}
	if cookieRefresh != "" {
		return s.authenticateCookieSession(ctx, cookieRefresh)
	}
	return nil, &httpx.APIError{Status: 401, Code: httpx.CodeAuthRequired, Message: "authentication required"}
}

// liveSession 按 hash 列加载未撤销的 session；查无/撤销/DB 故障的语义是
// Bearer access 与 Cookie session 两个认证入口的公共管线，差异只在
// 列名、过期列与文案，由调用点显式给出。
func (s *Service) liveSession(ctx context.Context, hashColumn, hash, notFoundMsg string) (*model.Session, *httpx.APIError) {
	var sess model.Session
	err := s.DB.WithContext(ctx).Where(hashColumn+" = ?", hash).First(&sess).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeAuthRequired, Message: notFoundMsg}
	}
	if err != nil {
		return nil, httpx.Internal("auth lookup failed")
	}
	if sess.RevokedAt != nil {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeTokenRevoked, Message: "session revoked"}
	}
	return &sess, nil
}

// authActor 是 session 类认证管线的 actor 装载：access/cookie 两个入口只凭
// session.actor_id 关联，这里统一补一次主键查询取 kind 与 platform_role。
// 停用账号（admin 用户管理，round 34）在此统一拒绝：生效即时，不依赖会话撤销。
func (s *Service) authActor(ctx context.Context, actorID string) (*model.Actor, *httpx.APIError) {
	var actor model.Actor
	err := s.DB.WithContext(ctx).First(&actor, "id = ?", actorID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeAuthRequired, Message: "actor missing"}
	}
	if err != nil {
		return nil, httpx.Internal("auth lookup failed")
	}
	if actor.DisabledAt != nil {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeTokenRevoked, Message: "account disabled"}
	}
	return &actor, nil
}

func (s *Service) authenticateAccessToken(ctx context.Context, token string) (*Principal, *httpx.APIError) {
	sess, apiErr := s.liveSession(ctx, "access_token_hash", HashToken(token), "invalid access token")
	if apiErr != nil {
		return nil, apiErr
	}
	if time.Now().After(sess.AccessExpiresAt) {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeTokenExpired, Message: "access token expired"}
	}
	actor, apiErr := s.authActor(ctx, sess.ActorID)
	if apiErr != nil {
		return nil, apiErr
	}
	return &Principal{ActorID: actor.ID, Kind: actor.Kind, PlatformRole: actor.PlatformRole, AuthKind: "access_token", SessionID: sess.ID}, nil
}

func (s *Service) authenticateCredential(ctx context.Context, secret string) (*Principal, *httpx.APIError) {
	var cred model.Credential
	err := s.DB.WithContext(ctx).Where("secret_hash = ?", HashToken(secret)).First(&cred).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeAuthRequired, Message: "invalid credential"}
	}
	if err != nil {
		return nil, httpx.Internal("auth lookup failed")
	}
	if cred.RevokedAt != nil {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeTokenRevoked, Message: "credential revoked"}
	}
	if cred.ExpiresAt != nil && time.Now().After(*cred.ExpiresAt) {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeTokenExpired, Message: "credential expired"}
	}
	var actor model.Actor
	if err := s.DB.WithContext(ctx).First(&actor, "id = ?", cred.ActorID).Error; err != nil {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeAuthRequired, Message: "credential actor missing"}
	}
	if actor.DisabledAt != nil {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeTokenRevoked, Message: "account disabled"}
	}
	// last_used 异步更新，失败不影响请求；按分钟节流，避免每请求一条
	// UPDATE（高频 agent 场景下是纯写放大）。
	if cred.LastUsedAt == nil || time.Since(*cred.LastUsedAt) > time.Minute {
		go func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = s.DB.WithContext(bgCtx).Model(&model.Credential{}).Where("id = ?", cred.ID).Update("last_used_at", time.Now()).Error
		}()
	}
	return &Principal{ActorID: cred.ActorID, Kind: actor.Kind, PlatformRole: actor.PlatformRole, AuthKind: "credential", Credential: &cred}, nil
}

// authenticateCookieSession 允许浏览器凭 HttpOnly refresh Cookie 访问（主要给 SSE ——
// EventSource 无法携带 Authorization 头）。授予权限时按 human 成员角色计算，
// 与 Bearer access 等效。
func (s *Service) authenticateCookieSession(ctx context.Context, refresh string) (*Principal, *httpx.APIError) {
	sess, apiErr := s.liveSession(ctx, "refresh_token_hash", HashToken(refresh), "invalid session")
	if apiErr != nil {
		return nil, apiErr
	}
	if time.Now().After(sess.ExpiresAt) {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeTokenExpired, Message: "session expired"}
	}
	actor, apiErr := s.authActor(ctx, sess.ActorID)
	if apiErr != nil {
		return nil, apiErr
	}
	return &Principal{ActorID: actor.ID, Kind: actor.Kind, PlatformRole: actor.PlatformRole, AuthKind: "session_cookie", SessionID: sess.ID}, nil
}
