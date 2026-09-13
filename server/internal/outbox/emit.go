package outbox

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ids"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ptr"
)

// EmitTx 在业务事务内写入 outbox（architecture §19：与业务变更同事务，
// 保证事件不丢、不虚发）。payload 是 envelope 的 data 字段。
//
// 落库形状是 {resource_revision, data} 包装（modules/event 的
// envelopeFromRow 负责还原）：resource_revision 是 dispatcher 拼装
// envelope 时的元数据，不单独加列。
func EmitTx(tx *gorm.DB, typ string, workspaceID, actorID string, resourceRevision int64, data any) error {
	raw, err := json.Marshal(map[string]any{
		"resource_revision": resourceRevision,
		"data":              data,
	})
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
		row.WorkspaceID = ptr.Of(workspaceID)
	}
	if actorID != "" {
		row.ActorID = ptr.Of(actorID)
	}
	return tx.Create(&row).Error
}
