package event

import (
	"context"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// RetentionWindow 是 outbox 行（含已投递）的保留时长（TODO.md S1 裁决 = 24h）。
// 它同时是 SSE resume 能补发的最大回溯窗口：游标超出窗口 → snapshot.required。
// 修改此值属于协议行为变更（影响客户端 resume 假设），需登记 TODO.md。
const RetentionWindow = 24 * time.Hour

// StartRetention 周期清理已投递且超出保留窗口的 outbox 行。
// 由 app 装配启动（main）；与 SSE handler 共享 RetentionWindow 常量。
func StartRetention(ctx context.Context, db *gorm.DB, log *slog.Logger, every time.Duration) {
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				res := db.WithContext(ctx).
					Where("sent_at IS NOT NULL AND sent_at < ?", time.Now().Add(-RetentionWindow)).
					Delete(&model.OutboxEvent{})
				if res.Error != nil {
					if ctx.Err() == nil {
						log.Error("outbox retention cleanup failed", "err", res.Error)
					}
					continue
				}
				if res.RowsAffected > 0 {
					log.Info("outbox retention cleaned", "rows", res.RowsAffected)
				}
			}
		}
	}()
}
