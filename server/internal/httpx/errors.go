// Package httpx 是所有 HTTP 出口的唯一契约层：
// 错误 envelope、稳定错误码、公共响应头、通用响应结构。
//
// 任何模块不得绕过本包手写 JSON 错误响应，否则 CLI/GUI 将无法依赖
// 稳定的 error.code 语义（见 docs/protocol.md §4）。
package httpx

import (
	"encoding/json"
	"net/http"
)

// 稳定错误码。此列表必须与 api/openapi.yaml 中 Error.code 的 enum 保持一致；
// 变更错误码属于协议变更，需走 api/README.md 的协议发布流程。
//
// TODO(phase-2): 增加 contract test，校验本文件常量与 openapi.yaml enum 一致，
// 防止两端漂移（当前靠人工同步）。
const (
	CodeAuthRequired             = "AUTH_REQUIRED"
	CodeTokenExpired             = "TOKEN_EXPIRED"
	CodeTokenRevoked             = "TOKEN_REVOKED"
	CodeInsufficientScope        = "INSUFFICIENT_SCOPE"
	CodeServerNotFound           = "SERVER_NOT_FOUND"
	CodeWorkspaceNotFound        = "WORKSPACE_NOT_FOUND"
	CodeWorkspaceAlreadyBound    = "WORKSPACE_ALREADY_BOUND"
	CodeTaskNotFound             = "TASK_NOT_FOUND"
	CodeTaskAlreadyClaimed       = "TASK_ALREADY_CLAIMED"
	CodeTaskLeaseExpired         = "TASK_LEASE_EXPIRED"
	CodeTagProposalExpired       = "TAG_PROPOSAL_EXPIRED"
	CodeTagAlreadyExists         = "TAG_ALREADY_EXISTS"
	CodeRevisionConflict         = "REVISION_CONFLICT"
	CodeDocumentConflict         = "DOCUMENT_CONFLICT"
	CodeRateLimited              = "RATE_LIMITED"
	CodeClientVersionUnsupported = "CLIENT_VERSION_UNSUPPORTED"
	CodeValidationFailed         = "VALIDATION_FAILED"
	CodeInternalError            = "INTERNAL_ERROR"

	// Device Flow 轮询语义（A1，RFC 8628）：均返回 HTTP 400 + retryable=true。
	CodeAuthorizationPending = "AUTHORIZATION_PENDING"
	CodeSlowDown             = "SLOW_DOWN"

	// CodeWorkspaceNameTaken：workspace name/slug 唯一约束冲突（409）。
	CodeWorkspaceNameTaken = "WORKSPACE_NAME_TAKEN"

	// CodeNotImplemented 仅用于脚手架阶段的未实现端点（HTTP 501）。
	// 它不属于稳定公网错误码集合；各 Phase 完成后对应端点必须移除此响应。
	// CLI/GUI 不得依赖此 code 做正式逻辑。
	CodeNotImplemented = "NOT_IMPLEMENTED"
)

// Error 是 docs/protocol.md §4 定义的错误 envelope。
// 字段名与顺序均为公网契约，禁止增删重命名（新增字段需评估旧客户端兼容）。
type Error struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details,omitempty"`
	RequestID string         `json:"request_id"`
}

// ErrorEnvelope 是所有非 2xx JSON 响应的顶层形状。
type ErrorEnvelope struct {
	Error Error `json:"error"`
}

// APIError 携带 HTTP 状态码，便于 handler 用 errors.Is / errors.As 判断。
type APIError struct {
	Status  int
	Code    string
	Message string
	// Retryable 默认按 code 推断；显式设置可覆盖。
	Retryable *bool
	Details   map[string]any
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

// IsRetryable 返回该错误是否建议客户端重试。
func (e *APIError) IsRetryable() bool {
	if e.Retryable != nil {
		return *e.Retryable
	}
	return retryableByDefault(e.Code)
}

func retryableByDefault(code string) bool {
	switch code {
	case CodeRateLimited, CodeInternalError:
		return true
	default:
		return false
	}
}

// WriteError 以统一 envelope 写出错误。requestID 优先取 context（经
// RequestIDMiddleware），兜底读请求头（允许 handler 在测试中直调）。
func WriteError(w http.ResponseWriter, r *http.Request, apiErr *APIError) {
	reqID := RequestIDFrom(r.Context())
	if reqID == "" {
		reqID = r.Header.Get(HeaderRequestID)
	}
	if reqID != "" {
		// 正常路径由中间件提前写入；这里兜底，保证错误响应永远带 request id。
		w.Header().Set(HeaderRequestID, reqID)
	}
	if apiErr.Details == nil {
		apiErr.Details = map[string]any{}
	}
	env := ErrorEnvelope{Error: Error{
		Code:      apiErr.Code,
		Message:   apiErr.Message,
		Retryable: apiErr.IsRetryable(),
		Details:   apiErr.Details,
		RequestID: reqID,
	}}
	writeJSON(w, r, apiErr.Status, env)
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
