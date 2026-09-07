// Package model 定义 GORM 运行时模型，字段必须与 server/migrations/*.sql 保持一致。
// schema 变更流程：先写 migration，再同步本文件，再同步 api/openapi.yaml（TODO.md 有登记模板）。
// 任何“库里有、模型没有”的字段都视为未定稿设计，不得悄悄使用。
package model

import "time"

// Actor 见 00001_init.sql / architecture §6.1。
type Actor struct {
	ID          string `gorm:"primaryKey;size:40"`
	Kind        string `gorm:"size:16"` // human | agent | service
	DisplayName string `gorm:"size:200"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// TODO(phase-1): human 认证方式关联字段（本地口令 hash / OIDC subject）。
}

func (Actor) TableName() string { return "actors" }

// Workspace 见 00001_init.sql / architecture §6.2。
type Workspace struct {
	ID        string `gorm:"primaryKey;size:40"`
	Name      string `gorm:"uniqueIndex;size:200"`
	Slug      string `gorm:"uniqueIndex;size:200"`
	CreatedBy string `gorm:"size:40"`
	CreatedAt time.Time
	UpdatedAt time.Time
	// TODO(phase-2): 默认策略、repo/integration metadata。
}

func (Workspace) TableName() string { return "workspaces" }

// WorkspaceMember 见 00001_init.sql。role 只是 scope bundle 名，授权按 scope 计算。
type WorkspaceMember struct {
	WorkspaceID string `gorm:"primaryKey;size:40"`
	ActorID     string `gorm:"primaryKey;size:40"`
	Role        string `gorm:"size:32"`
	CreatedAt   time.Time
}

func (WorkspaceMember) TableName() string { return "workspace_members" }

// Task 见 00003_task_tree.sql / architecture §12。
// Revision 由数据库触发器递增；应用层更新必须带 WHERE revision = expected_revision。
type Task struct {
	ID              string  `gorm:"primaryKey;size:40"`
	WorkspaceID     string  `gorm:"index:idx_tasks_workspace_parent;size:40"`
	ParentID        *string `gorm:"index:idx_tasks_workspace_parent;size:40"`
	Title           string  `gorm:"size:500"`
	Description     string
	Status          string  `gorm:"index:idx_tasks_workspace_status;size:32"`
	Priority        string  `gorm:"size:16"`
	AssigneeActorID *string `gorm:"size:40"`
	Revision        int64
	CreatedBy       string  `gorm:"size:40"`
	UpdatedBy       *string `gorm:"size:40"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (Task) TableName() string { return "tasks" }

// Tag 见 00004_tags.sql / architecture §14。
// NormalizedName = trim + NFC + case-fold，由应用层在写路径统一计算。
type Tag struct {
	ID             string `gorm:"primaryKey;size:40"`
	WorkspaceID    string `gorm:"uniqueIndex:uq_tag_ws_name;size:40"`
	Name           string `gorm:"size:64"`
	NormalizedName string `gorm:"uniqueIndex:uq_tag_ws_name;size:64"`
	CreatedBy      string `gorm:"size:40"`
	CreatedAt      time.Time
}

func (Tag) TableName() string { return "tags" }

// TagProposal 见 00004_tags.sql。confirm 成功后置 confirmed；过期由读路径判定。
type TagProposal struct {
	ID              string  `gorm:"primaryKey;size:40"`
	WorkspaceID     string  `gorm:"index;size:40"`
	ActorID         string  `gorm:"size:40"`
	Action          string  `gorm:"size:16"`
	CanonicalName   string  `gorm:"size:64"`
	TargetTagID     *string `gorm:"size:40"`
	ConfirmCodeHash string  `gorm:"size:128"`
	Status          string  `gorm:"size:16"`
	RequestID       *string
	ExpiresAt       time.Time
	CreatedAt       time.Time
	ConfirmedAt     *time.Time
}

func (TagProposal) TableName() string { return "tag_proposals" }

// OutboxEvent 见 00006_outbox.sql / architecture §19。
// 业务事务内 INSERT；dispatcher 读取后置 sent_at 并推给 SSE hub。
type OutboxEvent struct {
	ID          string  `gorm:"primaryKey;size:48"`
	WorkspaceID *string `gorm:"index;size:40"`
	Type        string  `gorm:"size:64"`
	ActorID     *string `gorm:"size:40"`
	Payload     []byte  `gorm:"type:jsonb"`
	OccurredAt  time.Time
	SentAt      *time.Time
}

func (OutboxEvent) TableName() string { return "outbox" }

// AuditEntry 见 00007_audit.sql / architecture §6.10。只追加，不更新。
type AuditEntry struct {
	ID          string  `gorm:"primaryKey;size:48"`
	WorkspaceID *string `gorm:"index;size:40"`
	ActorID     *string `gorm:"size:40"`
	Action      string  `gorm:"size:64"`
	Outcome     string  `gorm:"size:16"`
	TargetType  *string `gorm:"size:32"`
	TargetID    *string `gorm:"size:48"`
	Details     []byte  `gorm:"type:jsonb"`
	RequestID   *string
	CreatedAt   time.Time
}

func (AuditEntry) TableName() string { return "audit_log" }
