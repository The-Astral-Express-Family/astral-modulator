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
		return nil, &httpx.APIError{
			Status:  http.StatusServiceUnavailable,
			Code:    httpx.CodeInternalError,
			Message: "server running without storage (ASTRAL_DATABASE_DSN not set)",
		}
	}
	return s.DB, nil
}

// ---- Principal 与授权 ----

// Principal 是请求解析出的操作者。
type Principal struct {
	ActorID string
	Kind    string // human | agent | service
	// AuthKind: access_token | credential | session_cookie
	AuthKind string
	// SessionID 仅 human 会话时有值。
	SessionID string
	// Credential 仅 credential 认证时有值。
	Credential *model.Credential
}

func (p *Principal) IsHuman() bool { return p.Kind == "human" }

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
		return nil, &httpx.APIError{Status: 500, Code: httpx.CodeInternalError, Message: "scope resolution failed"}
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

// ---- 注册 / 登录（human，web 侧；TODO.md D6）----

type RegisterInput struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

// Register 仅在服务器还没有任何 human 时开放（bootstrap）。
// 后续 human 的加入方式（邀请/审批）未定稿，见 TODO.md。
func (s *Service) Register(ctx context.Context, in RegisterInput) (*model.Actor, error) {
	if _, dbErr := s.dbOrError(); dbErr != nil {
		return nil, dbErr
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if in.Email == "" || !strings.Contains(in.Email, "@") {
		return nil, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "invalid email"}
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		in.DisplayName = strings.SplitN(in.Email, "@", 2)[0]
	}

	var humans int64
	if err := s.DB.WithContext(ctx).Model(&model.Actor{}).Where("kind = ?", "human").Count(&humans).Error; err != nil {
		return nil, err
	}
	if humans > 0 {
		return nil, &httpx.APIError{Status: 403, Code: httpx.CodeInsufficientScope, Message: "registration closed: initial human already exists"}
	}

	hash, err := HashPassword(in.Password)
	if err != nil {
		return nil, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: err.Error()}
	}

	actor := &model.Actor{ID: ids.New(ids.User), Kind: "human", DisplayName: in.DisplayName}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(actor).Error; err != nil {
			return err
		}
		return tx.Create(&model.HumanAuth{ActorID: actor.ID, Email: in.Email, PasswordHash: hash}).Error
	})
	if err != nil {
		return nil, err
	}
	s.Log.Info("bootstrap human registered", "actor_id", actor.ID)
	return actor, nil
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
			return "", nil, &httpx.APIError{Status: 500, Code: httpx.CodeInternalError, Message: "login failed"}
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
	AccessToken  string      `json:"access_token"`
	TokenType    string      `json:"token_type"`
	ExpiresIn    int         `json:"expires_in"`
	RefreshToken string      `json:"refresh_token,omitempty"`
	ActorID      string      `json:"actor_id"`
	Me           *MeResponse `json:"me,omitempty"`
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
	// TODO(phase-1): 撤销后主动断开该 family 的 SSE 连接（security.md）。
	// TODO(phase-2): audit 记录（audit recorder 注入后）。
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

// MeResponse 是 /auth/me 的响应体。
type MeResponse struct {
	Actor   model.Actor  `json:"actor"`
	Session *SessionInfo `json:"session,omitempty"`
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

func (s *Service) authenticateAccessToken(ctx context.Context, token string) (*Principal, *httpx.APIError) {
	var sess model.Session
	err := s.DB.WithContext(ctx).Where("access_token_hash = ?", HashToken(token)).First(&sess).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeAuthRequired, Message: "invalid access token"}
	}
	if err != nil {
		return nil, &httpx.APIError{Status: 500, Code: httpx.CodeInternalError, Message: "auth lookup failed"}
	}
	if sess.RevokedAt != nil {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeTokenRevoked, Message: "session revoked"}
	}
	if time.Now().After(sess.AccessExpiresAt) {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeTokenExpired, Message: "access token expired"}
	}
	return &Principal{ActorID: sess.ActorID, Kind: "human", AuthKind: "access_token", SessionID: sess.ID}, nil
}

func (s *Service) authenticateCredential(ctx context.Context, secret string) (*Principal, *httpx.APIError) {
	var cred model.Credential
	err := s.DB.WithContext(ctx).Where("secret_hash = ?", HashToken(secret)).First(&cred).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeAuthRequired, Message: "invalid credential"}
	}
	if err != nil {
		return nil, &httpx.APIError{Status: 500, Code: httpx.CodeInternalError, Message: "auth lookup failed"}
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
	// last_used 异步更新，失败不影响请求；按分钟节流，避免每请求一条
	// UPDATE（高频 agent 场景下是纯写放大）。
	if cred.LastUsedAt == nil || time.Since(*cred.LastUsedAt) > time.Minute {
		go func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = s.DB.WithContext(bgCtx).Model(&model.Credential{}).Where("id = ?", cred.ID).Update("last_used_at", time.Now()).Error
		}()
	}
	return &Principal{ActorID: cred.ActorID, Kind: actor.Kind, AuthKind: "credential", Credential: &cred}, nil
}

// authenticateCookieSession 允许浏览器凭 HttpOnly refresh Cookie 访问（主要给 SSE ——
// EventSource 无法携带 Authorization 头）。授予权限时按 human 成员角色计算，
// 与 Bearer access 等效。
func (s *Service) authenticateCookieSession(ctx context.Context, refresh string) (*Principal, *httpx.APIError) {
	var sess model.Session
	err := s.DB.WithContext(ctx).Where("refresh_token_hash = ?", HashToken(refresh)).First(&sess).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeAuthRequired, Message: "invalid session"}
	}
	if err != nil {
		return nil, &httpx.APIError{Status: 500, Code: httpx.CodeInternalError, Message: "auth lookup failed"}
	}
	if sess.RevokedAt != nil {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeTokenRevoked, Message: "session revoked"}
	}
	if time.Now().After(sess.ExpiresAt) {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeTokenExpired, Message: "session expired"}
	}
	return &Principal{ActorID: sess.ActorID, Kind: "human", AuthKind: "session_cookie", SessionID: sess.ID}, nil
}
