// Package model 定义 GORM 运行时模型，字段必须与 server/migrations/*.sql 保持一致。
// schema 变更流程：先写 migration，再同步本文件，再同步 api/openapi.yaml（TODO.md 有登记模板）。
// 任何“库里有、模型没有”的字段都视为未定稿设计，不得悄悄使用。
package model

import "time"

// Actor 见 00001_init.sql / architecture §6.1；bio/avatar_url 见 00012_actor_profile.sql；
// platform_role/disabled_at 见 00014_platform_role.sql。
type Actor struct {
	ID          string `gorm:"primaryKey;size:40"`
	Kind        string `gorm:"size:16"` // human | agent | service
	DisplayName string `gorm:"size:200"`
	Bio         string `gorm:"size:500"`
	// AvatarURL 是头像外链（http/https）；nil = 未设置，由 API 层序列化为空串。
	AvatarURL *string `gorm:"size:500"`
	// PlatformRole 是平台全局角色（admin/user/agent/service）；与 kind 的
	// 一致性由 PG CHECK 约束钉死，创建点必须显式赋值（GORM 零值会插空串）。
	PlatformRole string `gorm:"size:16"`
	// DisabledAt 非 nil = 被平台管理员停用（round 34）；认证管线据此拒绝。
	DisabledAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
	// Human 本地登录凭证在 human_auth 表（D6）；OIDC subject 字段后续迁移再加。
}

func (Actor) TableName() string { return "actors" }

// HumanAuth 见 00001_init.sql（D6 本地账号）。bcrypt hash；email 唯一。
type HumanAuth struct {
	ActorID      string `gorm:"primaryKey;size:40"`
	Email        string `gorm:"uniqueIndex;size:254"`
	PasswordHash string `gorm:"size:128"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (HumanAuth) TableName() string { return "human_auth" }

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

// DeviceAuthorization 见 00002_auth_sessions.sql。
// UpdatedAt 兼作最近轮询时间戳（SLOW_DOWN 判定，A1）。
type DeviceAuthorization struct {
	ID             string  `gorm:"primaryKey;size:40"`
	DeviceCodeHash string  `gorm:"size:128;index"`
	UserCode       string  `gorm:"uniqueIndex;size:16"`
	ClientType     string  `gorm:"size:8"`
	Status         string  `gorm:"size:16"`
	ActorID        *string `gorm:"size:40"`
	ExpiresAt      time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ExchangedAt    *time.Time
}

func (DeviceAuthorization) TableName() string { return "device_authorizations" }

// Session 见 00002_auth_sessions.sql。opaque access+refresh 只存 hash；
// prev_refresh_token_hash 命中即整族撤销（重放检测）。
type Session struct {
	ID                   string  `gorm:"primaryKey;size:40"`
	ActorID              string  `gorm:"index;size:40"`
	ClientType           string  `gorm:"size:8"`
	RefreshTokenHash     string  `gorm:"index;size:128"`
	PrevRefreshTokenHash *string `gorm:"size:128"`
	FamilyID             string  `gorm:"index;size:40"`
	AccessTokenHash      string  `gorm:"index;size:128"`
	AccessExpiresAt      time.Time
	CreatedAt            time.Time
	ExpiresAt            time.Time
	LastUsedAt           *time.Time
	RevokedAt            *time.Time
	UserAgent            string
	RemoteAddr           string
}

func (Session) TableName() string { return "sessions" }

// Credential 见 00002_auth_sessions.sql。Scopes 为 JSON 数组文本（可移植，见 TODO.md）。
type Credential struct {
	ID          string  `gorm:"primaryKey;size:40"`
	ActorID     string  `gorm:"index;size:40"`
	Kind        string  `gorm:"size:16"`
	SecretHash  string  `gorm:"size:128"`
	WorkspaceID *string `gorm:"size:40"`
	Scopes      string  `gorm:"size:4096"`
	CreatedBy   string  `gorm:"size:40"`
	CreatedAt   time.Time
	ExpiresAt   *time.Time
	LastUsedAt  *time.Time
	RevokedAt   *time.Time
}

func (Credential) TableName() string { return "credentials" }

// Task 见 00003_task_tree.sql / architecture §12。
// Revision 由应用层在 UPDATE 中显式 +1（TODO.md D5）；乐观并发用
// UPDATE ... WHERE revision = expected（冲突 0 行 → REVISION_CONFLICT）。
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

// TaskLease 见 00003_task_tree.sql。读路径必须把 expires_at < now 视为无主；
// 过期清扫由 task 模块 sweeper 负责（发 task.lease.expired）。
type TaskLease struct {
	TaskID        string `gorm:"primaryKey;size:40"`
	HolderActorID string `gorm:"size:40"`
	ExpiresAt     time.Time
	RenewedAt     time.Time
	CreatedAt     time.Time
}

func (TaskLease) TableName() string { return "task_leases" }

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

// TaskTag 见 00004_tags.sql：task 与 tag 多对多关联。task 模块经 GORM
// Create/Delete/Count 与批量 JOIN（tagsForTasks）读写；task-search 的
// tag 过滤走参数化 SQL JOIN。
type TaskTag struct {
	TaskID  string `gorm:"primaryKey;size:40"`
	TagID   string `gorm:"primaryKey;size:40"`
	AddedBy string `gorm:"size:40"`
	AddedAt time.Time
}

func (TaskTag) TableName() string { return "task_tags" }

// Document 见 00005_documents.sql / architecture §16、docs/sync-semantics.md。
// (workspace_id, path) 唯一；revision 由应用层在写路径显式 +1（同 Task 的 D5 约定）。
// DeletedAt 见 00015_documents_tombstone.sql：非 nil = tombstone，manifest 默认
// 排除、include_deleted=true 才返回。tombstone 语义由应用层手动控制，刻意不用
// gorm.DeletedAt（其隐式过滤与自动写入会和 get/manifest 的显式取舍、复活路径冲突）。
type Document struct {
	ID          string `gorm:"primaryKey;size:40"` // doc_ 前缀
	WorkspaceID string `gorm:"uniqueIndex:uq_documents_ws_path;index:idx_documents_workspace;size:40"`
	// Path 服务端 canonicalize（document/paths.go），存储精确原文；大小写冲突
	// 由 push 前置检查拒绝（round 38 R1），库层不做 LOWER 唯一索引。
	Path        string `gorm:"uniqueIndex:uq_documents_ws_path"`
	Revision    int64
	ContentHash string // 'sha256:<hex>' 小写，服务端对原始 UTF-8 bytes 重算
	Content     string // UTF-8 only；MVP 直接入库，不引对象存储
	UpdatedBy   string `gorm:"size:40"`
	CreatedAt   time.Time
	UpdatedAt   time.Time `gorm:"index:idx_documents_workspace"`
	DeletedAt   *time.Time
}

func (Document) TableName() string { return "documents" }

// DocumentConflict 见 00005_documents.sql：push 的 base_revision 与服务端当前
// revision 不一致时落冲突工件；resolution/resolved_by/resolved_at 由 resolve
// 端点同事务写入（open = resolved_at IS NULL，读路径派生，不落 status 列）。
type DocumentConflict struct {
	ID           string `gorm:"primaryKey;size:40"` // cfl_ 前缀
	WorkspaceID  string `gorm:"index:idx_conflicts_workspace;size:40"`
	Path         string
	BaseRevision int64
	BaseHash     string
	OursJSON     []byte `gorm:"type:jsonb"` // push 方内容
	TheirsJSON   []byte `gorm:"type:jsonb"` // 服务端当前内容
	// Resolution ∈ ours|theirs|merged|manual（PG CHECK 钉死）；NULL = 未解决。
	Resolution *string
	ResolvedBy *string    `gorm:"size:40"`
	ResolvedAt *time.Time `gorm:"index:idx_conflicts_workspace"`
	CreatedAt  time.Time
}

func (DocumentConflict) TableName() string { return "document_conflicts" }

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

// Presence 见 00008_presence_messages.sql / 00010（主键改为 actor+workspace）。
// 同一 actor 可在多个 workspace 各自 heartbeat，互不覆盖；
// offline 是读路径派生值，不落库。
type Presence struct {
	ActorID         string  `gorm:"primaryKey;size:40"`
	WorkspaceID     string  `gorm:"primaryKey;size:40;index"`
	State           string  `gorm:"size:16"`
	CurrentTaskID   *string `gorm:"size:40"`
	Note            *string
	LastHeartbeatAt time.Time
	ExpiresAt       time.Time
}

func (Presence) TableName() string { return "presence" }

// Message 见 00008_presence_messages.sql。target 三类：actor/workspace/task。
type Message struct {
	ID          string  `gorm:"primaryKey;size:40"`
	WorkspaceID string  `gorm:"size:40"`
	ThreadID    *string `gorm:"size:40"`
	TargetType  string  `gorm:"size:16"`
	TargetID    string  `gorm:"size:48"`
	SenderID    string  `gorm:"size:40"`
	Body        string
	Metadata    []byte `gorm:"type:jsonb"`
	CreatedAt   time.Time
}

func (Message) TableName() string { return "messages" }

// ServerMeta 见 00001_init.sql：服务器级持久化元数据（server_id 稳定身份等）。
type ServerMeta struct {
	Key       string `gorm:"primaryKey;size:64"`
	Value     string
	UpdatedAt time.Time
}

func (ServerMeta) TableName() string { return "server_meta" }

// IdempotencyKey 见 00009_idempotency_keys.sql：写操作幂等去重。
type IdempotencyKey struct {
	ActorID     string `gorm:"primaryKey;size:40"`
	Endpoint    string `gorm:"primaryKey;size:255"`
	Key         string `gorm:"primaryKey;size:255"`
	StatusCode  int
	ContentType string `gorm:"size:128"`
	Body        []byte
	CreatedAt   time.Time
}

func (IdempotencyKey) TableName() string { return "idempotency_keys" }

// Approval 见 00011_approvals.sql / architecture §22：高风险动作的
// server-side approval 状态机（requested -> approved|rejected|expired -> executed）。
// approve 与业务执行同事务完成（MVP：membership.promote_owner）。
type Approval struct {
	ID            string  `gorm:"primaryKey;size:40"`
	WorkspaceID   string  `gorm:"index:idx_approvals_ws_status;size:40"`
	Action        string  `gorm:"size:64"`
	TargetActorID string  `gorm:"size:40"`
	Payload       []byte  `gorm:"type:jsonb"`
	Status        string  `gorm:"index:idx_approvals_ws_status;size:16"`
	RequestedBy   string  `gorm:"size:40"`
	DecidedBy     *string `gorm:"size:40"`
	DecidedAt     *time.Time
	ExpiresAt     time.Time
	CreatedAt     time.Time
}

func (Approval) TableName() string { return "approvals" }

// Invitation 见 00013_workspace_invitations.sql / docs/registration.md。
// 一次性邀请码：库中只存归一化码的 sha256；expired 不落库（读路径按
// status='invited' 且 now > expires_at 派生）；redemption 走条件更新抢状态。
type Invitation struct {
	ID          string `gorm:"primaryKey;size:40"`
	WorkspaceID string `gorm:"index:idx_invitations_ws_status;size:40"`
	Role        string `gorm:"size:32"`
	CodeHash    string `gorm:"uniqueIndex;size:128"`
	CreatedBy   string `gorm:"size:40"`
	Status      string `gorm:"index:idx_invitations_ws_status;size:16"`
	CreatedAt   time.Time
	ExpiresAt   time.Time
	RedeemedBy  *string `gorm:"size:40"`
	RedeemedAt  *time.Time
}

func (Invitation) TableName() string { return "workspace_invitations" }
