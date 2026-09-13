package store

import (
	"errors"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
)

// First 按 query 查一行到 dest 的单一出口，收拢各模块重复的
// 「First → ErrRecordNotFound→404 → 其余→500」样板（round 24 遗留 ②）。
// 命中返回 nil；查无返回 notFound（由调用方给出资源语义正确的错误：
// 404 NOT_FOUND / 专用 *_NOT_FOUND / 401 / 400 皆可）；其余 DB 故障统一
// INTERNAL_ERROR——原始错误已由 gorm logger 记录，公网 envelope 不携带细节。
// 「查无属正常分支」的语义（如 lease 读路径）不适用本函数，保持显式 switch。
func First[T any](db *gorm.DB, dest *T, notFound *httpx.APIError, query any, args ...any) *httpx.APIError {
	conds := append([]any{query}, args...)
	err := db.First(dest, conds...).Error
	switch {
	case err == nil:
		return nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		return notFound
	default:
		return httpx.Internal("lookup failed")
	}
}
