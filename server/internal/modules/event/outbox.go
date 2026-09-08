package event

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ids"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// EmitTx 在业务事务内写入 outbox（architecture §19：与业务变更同事务，
// 保证事件不丢、不虚发）。payload 是 envelope 的 data 字段。
func EmitTx(tx *gorm.DB, typ string, workspaceID, actorID string, resourceRevision int64, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	row := model.OutboxEvent{
		ID:         ids.New(ids.Event),
		Type:       typ,
		Payload:    raw,
		OccurredAt: time.Now(),
	}
	if workspaceID != "" {
		ws := workspaceID
		row.WorkspaceID = &ws
	}
	if actorID != "" {
		a := actorID
		row.ActorID = &a
	}
	// resource_revision 编码进 payload 顶部由 dispatcher 拼装 envelope 时使用；
	// 为避免 outbox 表加列，这里把 envelope 元数据整体存 payload，
	// data 内容单独编一层。
	envelope := map[string]any{
		"resource_revision": resourceRevision,
		"data":              json.RawMessage(raw),
	}
	envelopeRaw, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	row.Payload = envelopeRaw
	return tx.Create(&row).Error
}

// StartDispatcher 周期读取未投递 outbox，发布到本实例 hub 并标记 sent_at。
// MVP 单实例轮询；TODO(phase-4): LISTEN/NOTIFY 降低延迟 + Last-Event-ID 重放窗口。
func StartDispatcher(ctx context.Context, db *gorm.DB, hub *Hub, log *slog.Logger, every time.Duration) {
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pollOnce(ctx, db, hub, log)
			}
		}
	}()
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
