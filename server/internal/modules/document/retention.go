// retention.go 是历史版本保留窗口清扫（00017；TODO §1.2 P3）。与 outbox
// 24h / 幂等键清理同款 background.RunEvery 模式。两道闸并行、先到即剪：
//   - TTL：created_at 早于窗口的版本删除（ASTRAL_DOC_HISTORY_TTL_HOURS，
//     默认 720 = 30 天）；
//   - 每文档上限：仅保留每 document 最近 N 版（ASTRAL_DOC_HISTORY_MAX_PER_DOC，
//     默认 50）。
//
// 两项均可设 0 关闭对应维度（双 0 = 无限保留，仅建议自管库使用）。清扫是
// 内务操作，不落 audit、不发事件（与 outbox 清理同口径）。
package document

import (
	"context"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/background"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// StartVersionRetention 启动保留窗口清扫循环（every 为巡检周期）。
func StartVersionRetention(ctx context.Context, db *gorm.DB, log *slog.Logger, every time.Duration, maxPerDoc int, ttl time.Duration) {
	background.RunEvery(ctx, every, func(ctx context.Context) {
		sweepVersionsOnce(ctx, db, log, maxPerDoc, ttl)
	})
}

// sweepVersionsOnce 执行一轮清扫；抽出为函数便于内务测试直调。TTL 失败时
// 不再执行每文档上限（同一轮内避免叠加半途状态，下一轮整体重试）。
func sweepVersionsOnce(ctx context.Context, db *gorm.DB, log *slog.Logger, maxPerDoc int, ttl time.Duration) {
	if ttl > 0 {
		res := db.WithContext(ctx).
			Where("created_at < ?", time.Now().Add(-ttl)).
			Delete(&model.DocumentVersion{})
		if !logSweepResult(log, res.Error, res.RowsAffected, "ttl") {
			return
		}
	}
	if maxPerDoc > 0 {
		// 每文档仅留最近 N 版：窗口函数排序后剪掉 rn > N 的尾部。版本表本身
		// 被本清扫限定规模，全表扫描无压力。
		res := db.WithContext(ctx).Exec(
			`DELETE FROM document_versions WHERE id IN (
				SELECT id FROM (
					SELECT id, ROW_NUMBER() OVER (
						PARTITION BY document_id ORDER BY revision DESC) AS rn
					FROM document_versions
				) ranked WHERE rn > ?)`, maxPerDoc)
		logSweepResult(log, res.Error, res.RowsAffected, "per-doc cap")
	}
}

// logSweepResult 统一错误与清理量日志；返回 false 表示本轮该维度失败。
func logSweepResult(log *slog.Logger, err error, rows int64, dimension string) bool {
	if err != nil {
		log.Error("document version retention cleanup failed", "dimension", dimension, "err", err)
		return false
	}
	if rows > 0 {
		log.Info("document version retention cleaned", "dimension", dimension, "rows", rows)
	}
	return true
}
