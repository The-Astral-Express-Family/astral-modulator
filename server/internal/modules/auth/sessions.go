package auth

import (
	"context"
	"time"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// ---- 设备管理（会话列表 + 单会话注销）----
//
// 一个「设备」= 一条 session（CLI 经 device flow 兑换、浏览器经 login 各建一条）。
// 设备名不做服务端字段：user_agent 原样返回，友好化展示由客户端负责
// （CLI 的 UA 即其产品标识；浏览器的 UA 由前端解析成可读名）。

// SessionView 是设备管理页可见的会话条目。
type SessionView struct {
	ID         string  `json:"id"`
	ClientType string  `json:"client_type"`
	UserAgent  string  `json:"user_agent"`
	RemoteAddr string  `json:"remote_addr"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at,omitempty"`
	ExpiresAt  string  `json:"expires_at"`
	Current    bool    `json:"current"`
}

// ListSessions 列出主体自己的活跃会话（已撤销/已过期不入列），最近使用优先。
func (s *Service) ListSessions(ctx context.Context, actorID, currentSessionID string) ([]SessionView, error) {
	if _, dbErr := s.dbOrError(); dbErr != nil {
		return nil, dbErr
	}
	var rows []model.Session
	err := s.DB.WithContext(ctx).
		Where("actor_id = ? AND revoked_at IS NULL AND expires_at > ?", actorID, time.Now()).
		Order("last_used_at DESC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]SessionView, 0, len(rows))
	for i := range rows {
		out = append(out, s.toSessionView(&rows[i], currentSessionID))
	}
	return out, nil
}

func (s *Service) toSessionView(sess *model.Session, currentSessionID string) SessionView {
	view := SessionView{
		ID:         sess.ID,
		ClientType: sess.ClientType,
		UserAgent:  sess.UserAgent,
		RemoteAddr: sess.RemoteAddr,
		CreatedAt:  sess.CreatedAt.Format(time.RFC3339),
		ExpiresAt:  sess.ExpiresAt.Format(time.RFC3339),
		Current:    sess.ID == currentSessionID,
	}
	if sess.LastUsedAt != nil {
		last := sess.LastUsedAt.Format(time.RFC3339)
		view.LastUsedAt = &last
	}
	return view
}

// RevokeSession 注销主体的一个会话（设备管理页「注销」）。
// 次级资源语义与 RevokeCredential 对齐：不存在/非本人/已撤销一律 404，
// 不向枚举泄露区分信息。
func (s *Service) RevokeSession(ctx context.Context, actorID, sessionID string) error {
	if _, dbErr := s.dbOrError(); dbErr != nil {
		return dbErr
	}
	res := s.DB.WithContext(ctx).Model(&model.Session{}).
		Where("id = ? AND actor_id = ? AND revoked_at IS NULL", sessionID, actorID).
		Update("revoked_at", time.Now())
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		// 区分「查无」与「已撤销」无展示差异；已撤销时属主可辨但无需审计
		//（R7：撤销事实由 sessions.revoked_at 自身承载）。
		return httpx.NotFound("session not found or already revoked")
	}
	s.notifyRevoked(actorID)
	return nil
}

// touchSessionLastUsed 是 session 认证路径的 last_used_at 节流更新：每分钟至多
// 一次、失败不影响请求。CLI 的「最近使用」即来自此列——refresh 时精确更新，
// access/cookie 每请求节流更新。刻意同步（区别于 credential 的异步 goroutine）：
// 该写发生在 Authenticate 阶段、handler 事务开启之前，同请求内天然串行；
// 异步写会与业务事务在 sqlite 上形成 upgrade 死锁（testsupport G1 的教训）。
func (s *Service) touchSessionLastUsed(sess *model.Session) {
	if sess.LastUsedAt != nil && time.Since(*sess.LastUsedAt) < time.Minute {
		return
	}
	_ = s.DB.WithContext(context.Background()).Model(&model.Session{}).
		Where("id = ? AND revoked_at IS NULL", sess.ID).
		Update("last_used_at", time.Now()).Error
}
