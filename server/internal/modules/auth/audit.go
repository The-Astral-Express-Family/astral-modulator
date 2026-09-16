package auth

import (
	"context"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/audit"
)

// S4-3（原 §3.2）：认证失败写审计（security.md 审计要求）。R7 裁决：
// 成功路径一律不写——sessions 表自身即登录事实，本文件只承载 denied 口径。
// 覆盖的失败路径（login 口令错、refresh 重放、device 兑换 denied/expired、
// 受保护端点凭证解析失败）均无业务事务，走 GormRecorder.Record；
// 事务内审计（注册分支）沿用既有 audit.RecordInTx。写入失败仅记日志：
// 审计可用性不得反过来放大认证故障，拒绝响应照常返回。量由 S5 限流兜住。

// 认证失败审计的稳定动作名（服务器级，workspace_id 为 NULL）。
const (
	actionAuthLogin   = "auth.login"   // login 口令/账号校验失败
	actionAuthRefresh = "auth.refresh" // refresh token 重放（token family 撤销）
	actionAuthDevice  = "auth.device"  // device 兑换 denied/expired
	actionAuthBearer  = "auth.bearer"  // 受保护端点凭证解析失败（Authenticate 401）
)

// auditDenied 写一条 outcome=denied 的服务器级审计行；actor 可辨时由调用方
// 填 Entry.ActorID，否则留空（NULL）。桩模式（无库）下静默跳过，与
// dbOrError 的 503 语义一致——认证路径不会因审计而 panic。
func (s *Service) auditDenied(ctx context.Context, e audit.Entry) {
	if s.DB == nil {
		return
	}
	e.Outcome = "denied"
	if err := (&audit.GormRecorder{DB: s.DB}).Record(ctx, e); err != nil {
		s.Log.Warn("audit denied write failed", "action", e.Action, "err", err)
	}
}
