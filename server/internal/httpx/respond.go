package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

// Page 是 cursor 分页 envelope（docs/protocol.md §3）。
// next_cursor 为 null 表示没有更多数据；cursor 对客户端不透明。
// 用 NewPage 组装（空串归一为 null）。
type Page[T any] struct {
	Items      []T `json:"items"`
	NextCursor any `json:"next_cursor"`
}

// NewPage 组装分页 envelope；nextCursor 为空串时序列化为 null。
func NewPage[T any](items []T, nextCursor string) Page[T] {
	p := Page[T]{Items: items}
	if nextCursor != "" {
		p.NextCursor = nextCursor
	}
	return p
}

// TimeString 是 DTO 可选时间字段的单一转换点：*time.Time → RFC3339 字符串
// 指针，nil 进 nil 出（消除 `if t != nil { s := ...; p = &s }` 手工样板）。
func TimeString(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
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

// DecodeJSON 解析请求体到 dst。空 body 视为合法（全可选字段的端点）；
// 语法错误 → VALIDATION_FAILED。
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		WriteError(w, r, &APIError{
			Status:  http.StatusBadRequest,
			Code:    CodeValidationFailed,
			Message: "read body failed: " + err.Error(),
		})
		return false
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return true
	}
	if err := json.Unmarshal(raw, dst); err != nil {
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
