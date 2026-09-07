package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
)

// Page 是 cursor 分页 envelope（docs/protocol.md §5）。
// next_cursor 为 null 表示没有更多数据；cursor 对客户端不透明。
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor"`
}

// NotImplemented 是脚手架阶段的占位响应：HTTP 501 + NOT_IMPLEMENTED envelope。
// feature 用于日志定位，docRef 指向实现依据的文档章节。
//
// 每个使用此函数的 handler 都必须在 TODO.md 中有对应条目，
// 并在该功能所属 Phase 完成时删除。
func NotImplemented(w http.ResponseWriter, r *http.Request, feature, phase, docRef string) {
	WriteError(w, r, &APIError{
		Status:  http.StatusNotImplemented,
		Code:    CodeNotImplemented,
		Message: "endpoint not implemented yet (scaffold stub): " + feature,
		Details: map[string]any{
			"feature": feature,
			"phase":   phase,
			"doc":     docRef,
		},
	})
}

// DecodeJSON 严格解析请求体（不允许未知字段静默通过由各 handler 决定，
// 此处只负责大小限制与语法错误 → VALIDATION_FAILED）。
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		WriteError(w, r, &APIError{
			Status:  http.StatusBadRequest,
			Code:    CodeValidationFailed,
			Message: "invalid JSON body: " + err.Error(),
		})
		return false
	}
	return true
}

// WriteOK 写出 2xx JSON 响应。
func WriteOK(w http.ResponseWriter, r *http.Request, status int, body any) {
	writeJSON(w, r, status, body)
}

// RespondError 统一把 service 层错误映射为 envelope：
// *APIError 原样透传，其余归为 INTERNAL_ERROR（不向客户端泄露内部细节）。
func RespondError(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		WriteError(w, r, apiErr)
		return
	}
	WriteError(w, r, &APIError{
		Status:  http.StatusInternalServerError,
		Code:    CodeInternalError,
		Message: "internal error",
	})
}

const maxBodyBytes = 8 << 20 // TODO(phase-4): 文档同步可能需要更大上限，届时按端点细化。
