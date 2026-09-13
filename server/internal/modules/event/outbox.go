package event

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/background"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// StartDispatcher 周期读取未投递 outbox，发布到本实例 hub 并标记 sent_at。
// MVP 单实例轮询（500ms，见 main 装配）；跨实例广播需求出现后再上
// LISTEN/NOTIFY 或消息总线（architecture §19 的触发条件）。
func StartDispatcher(ctx context.Context, db *gorm.DB, hub *Hub, log *slog.Logger, every time.Duration) {
	background.RunEvery(ctx, every, func(ctx context.Context) {
		pollOnce(ctx, db, hub, log)
	})
}

// PollOnce 执行一次 outbox 投递（导出供测试与外部调度复用）。
func PollOnce(ctx context.Context, db *gorm.DB, hub *Hub, log *slog.Logger) {
	pollOnce(ctx, db, hub, log)
}

func pollOnce(ctx context.Context, db *gorm.DB, hub *Hub, log *slog.Logger) {
	var rows []model.OutboxEvent
	err := db.WithContext(ctx).
		Where("sent_at IS NULL").Order("id ASC").Limit(200).
		Find(&rows).Error
	if err != nil {
		if ctx.Err() == nil {
			log.Error("outbox poll failed", "err", err)
		}
		return
	}
	for _, row := range rows {
		env, err := envelopeFromRow(row)
		if err != nil {
			log.Error("outbox envelope decode failed", "event_id", row.ID, "err", err)
			continue
		}
		hub.Publish(env)
		if err := db.WithContext(ctx).Model(&model.OutboxEvent{}).
			Where("id = ?", row.ID).Update("sent_at", time.Now()).Error; err != nil {
			log.Error("outbox mark sent failed", "event_id", row.ID, "err", err)
		}
	}
}

// envelopeFromRow 把 outbox 行还原为契约 envelope。
// payload 形状由 EmitTx 写入：{resource_revision, data}。
func envelopeFromRow(row model.OutboxEvent) (Envelope, error) {
	env := Envelope{
		ID:            row.ID,
		Type:          row.Type,
		OccurredAt:    row.OccurredAt.UTC().Format(time.RFC3339),
		SchemaVersion: 1,
		Data:          json.RawMessage(row.Payload),
	}
	if row.WorkspaceID != nil {
		env.WorkspaceID = *row.WorkspaceID
	}
	if row.ActorID != nil {
		env.ActorID = *row.ActorID
	}
	var wrapped struct {
		ResourceRevision int64           `json:"resource_revision"`
		Data             json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(row.Payload, &wrapped); err != nil {
		return env, err
	}
	env.ResourceRevision = wrapped.ResourceRevision
	env.Data = wrapped.Data
	return env, nil
}
