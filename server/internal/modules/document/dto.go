package document

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"time"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// 本文件是 document 模块的 wire 层：DTO 与 model 分离，字段名一律 snake_case，
// 形状对应 api/openapi.yaml 的 Document / ManifestItem / DocumentConflict /
// DocumentConflictDetail schema（契约门强制同步）。

// documentDTO 对应 openapi Document schema：get / push / resolve 的响应体。
// Deleted 由 DeletedAt 派生（tombstone 行保留内容照常返回，同步端据此侦测远端删除）。
type documentDTO struct {
	Path        string `json:"path"`
	Revision    int64  `json:"revision"`
	ContentHash string `json:"content_hash"`
	Content     string `json:"content"`
	UpdatedAt   string `json:"updated_at"`
	Deleted     bool   `json:"deleted"`
}

func toDocumentDTO(row model.Document) documentDTO {
	return documentDTO{
		Path:        row.Path,
		Revision:    row.Revision,
		ContentHash: row.ContentHash,
		Content:     row.Content,
		UpdatedAt:   row.UpdatedAt.UTC().Format(time.RFC3339),
		Deleted:     row.DeletedAt != nil,
	}
}

// manifestItemDTO 对应 openapi ManifestItem schema；Size 是 content 的 UTF-8
// 字节数（长度口径 = 字节，round 38 硬约束 §1-12）。
type manifestItemDTO struct {
	Path        string `json:"path"`
	Revision    int64  `json:"revision"`
	ContentHash string `json:"content_hash"`
	Size        int    `json:"size"`
	UpdatedAt   string `json:"updated_at"`
	Deleted     bool   `json:"deleted"`
}

func toManifestItemDTO(row model.Document) manifestItemDTO {
	return manifestItemDTO{
		Path:        row.Path,
		Revision:    row.Revision,
		ContentHash: row.ContentHash,
		Size:        len(row.Content),
		UpdatedAt:   row.UpdatedAt.UTC().Format(time.RFC3339),
		Deleted:     row.DeletedAt != nil,
	}
}

// conflictDTO 对应 openapi DocumentConflict schema（列表形状，双方全文不回，
// 全文走详情端点）。OursHash 在 delete 意图冲突（delete-vs-edit）时无值——
// omitempty 省略，与 schema 非必填一致。
type conflictDTO struct {
	ID             string  `json:"id"`
	Path           string  `json:"path"`
	BaseRevision   int64   `json:"base_revision"`
	BaseHash       string  `json:"base_hash"`
	Status         string  `json:"status"` // open | resolved（resolved_at IS NULL 派生）
	OursHash       string  `json:"ours_hash,omitempty"`
	TheirsRevision int64   `json:"theirs_revision"`
	TheirsHash     string  `json:"theirs_hash"`
	CreatedAt      string  `json:"created_at"`
	Resolution     *string `json:"resolution"`
	ResolvedBy     *string `json:"resolved_by"`
	ResolvedAt     *string `json:"resolved_at"`
}

func toConflictDTO(row model.DocumentConflict, ours, theirs conflictSide) conflictDTO {
	dto := conflictDTO{
		ID:             row.ID,
		Path:           row.Path,
		BaseRevision:   row.BaseRevision,
		BaseHash:       row.BaseHash,
		Status:         "open",
		OursHash:       ours.ContentHash,
		TheirsRevision: theirs.Revision,
		TheirsHash:     theirs.ContentHash,
		CreatedAt:      row.CreatedAt.UTC().Format(time.RFC3339),
		Resolution:     row.Resolution,
		ResolvedBy:     row.ResolvedBy,
		ResolvedAt:     httpx.TimeString(row.ResolvedAt),
	}
	if row.ResolvedAt != nil {
		dto.Status = "resolved"
	}
	return dto
}

// conflictDetailDTO 对应 openapi DocumentConflictDetail：列表形状 + 双方完整
// 内容，冲突解决视图的对比数据源。delete 意图方的 ours_content 为空串
// （schema 仅要求字段存在）。
type conflictDetailDTO struct {
	conflictDTO
	OursContent   string `json:"ours_content"`
	TheirsContent string `json:"theirs_content"`
}

// conflictSide 是 ours_json / theirs_json 落库 JSON 的统一形状（一种结构覆盖
// 双方三种形态，omitempty 保持落库 JSON 紧凑）：
//   - push 方内容：{content, content_hash, actor_id}；
//   - delete 意图（delete-vs-edit / 对 tombstone 的失配 push）：{delete:true,
//     base_revision, actor_id}——无内容与 hash；
//   - 服务端当前内容：{content, content_hash, revision, deleted}——deleted=true
//     标记远端已删除（edit-vs-delete 的 theirs 侧）。
type conflictSide struct {
	Delete       bool   `json:"delete,omitempty"`
	BaseRevision int64  `json:"base_revision,omitempty"`
	ActorID      string `json:"actor_id,omitempty"`
	Content      string `json:"content,omitempty"`
	ContentHash  string `json:"content_hash,omitempty"`
	Revision     int64  `json:"revision,omitempty"`
	Deleted      bool   `json:"deleted,omitempty"`
}

// sideFromDocument 构造 theirs 侧（服务端当前行）。
func sideFromDocument(row model.Document) conflictSide {
	return conflictSide{
		Content:     row.Content,
		ContentHash: row.ContentHash,
		Revision:    row.Revision,
		Deleted:     row.DeletedAt != nil,
	}
}

// hashPattern 是 content_hash 的 wire 格式：sha256:<64 位小写 hex>
// （sync-semantics §7；对原始 UTF-8 bytes 计算，不重写换行）。
var hashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// contentHash 对原始 UTF-8 bytes 重算协议 hash。这是双端对接的唯一事实来源：
// CLI 本地 hash 必须与此一致。
func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}
