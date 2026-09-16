package document

import (
	"encoding/json"
	"errors"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// 本文件收拢 document 模块的单行查询出口（硬约束 §1-10：单行查询走
// store.First；「查无属正常分支」的 push/delete 前置读取保持显式三态）。

// loadDocumentTx 按唯一键 (workspace_id, path) 精确加载（get/manifest/delete
// 共用口径：大小写敏感精确匹配；R1 的大小写冲突只在 push 侧拒绝）。
// 查无是 push/delete 的正常分支（创建/404 判定），故不走 store.First 而保持
// 三态显式返回。
func loadDocumentTx(tx *gorm.DB, workspaceID, path string) (*model.Document, bool, error) {
	var row model.Document
	err := tx.Where("workspace_id = ? AND path = ?", workspaceID, path).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &row, true, nil
}

// hasCaseCollisionTx 是 R1 裁决的运行时判定：同 workspace 已存在**仅大小写
// 不同**的 path（整条路径小写相等但原串不等）→ true。与 paths.go 的
// HasCaseCollision 纯函数同口径（ASCII-only LOWER，sqlite/PG 行为一致，MVP
// 可接受——非 ASCII 路径不参与折叠）。只在 push 前置检查，get/manifest/delete
// 仍精确匹配。
func hasCaseCollisionTx(tx *gorm.DB, workspaceID, path string) (bool, error) {
	var n int64
	err := tx.Model(&model.Document{}).
		Where("workspace_id = ? AND LOWER(path) = LOWER(?) AND path <> ?", workspaceID, path, path).
		Count(&n).Error
	return n > 0, err
}

// loadConflictTx 按 id + workspace 收口加载冲突行（详情/resolve；跨 workspace
// 的 conflict_id 视同不存在）。
func loadConflictTx(tx *gorm.DB, conflictID, workspaceID string) (*model.DocumentConflict, error) {
	var row model.DocumentConflict
	if err := tx.Where("id = ? AND workspace_id = ?", conflictID, workspaceID).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// decodeSides 还原 ours_json / theirs_json（宽容版）。落库 JSON 由本模块单一
// 构造，读端点（列表/详情）对损坏数据按空侧处理（仍可返回骨架）而非 500。
func decodeSides(row model.DocumentConflict) (ours, theirs conflictSide) {
	ours, _ = decodeSide(row.OursJSON)
	theirs, _ = decodeSide(row.TheirsJSON)
	return ours, theirs
}

// decodeSide 还原单侧 JSON，严格带出 unmarshal 错误。写路径（resolveCore）
// 经此对 ours 侧显式判错 → 500 INTERNAL_ERROR（数据损坏语义）：吞错会让
// 零值空侧以「裁决 ours 内容」身份把空串写进 document 行。
func decodeSide(raw []byte) (conflictSide, error) {
	var s conflictSide
	err := json.Unmarshal(raw, &s)
	return s, err
}
