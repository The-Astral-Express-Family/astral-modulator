-- 00007_audit: 审计日志（docs/architecture.md §6.10）。
-- 关键写操作、认证、授权失败、credential 生命周期、审批全部落审计。
-- 业务记录删除不级联审计；审计表只追加，不提供 UPDATE 路径。

-- +goose Up
CREATE TABLE audit_log (
    id           TEXT PRIMARY KEY,           -- aud_
    workspace_id TEXT,                       -- NULL = 服务器级（如登录）
    actor_id     TEXT,                       -- NULL = 匿名（如失败登录）
    action       TEXT NOT NULL,              -- 稳定动作名，如 task.claim / credential.create
    outcome      TEXT NOT NULL CHECK (outcome IN ('allowed','denied','error')),
    target_type  TEXT,
    target_id    TEXT,
    -- details 落库前必须经过 redaction（security.md 敏感字段 matcher）
    details      JSONB NOT NULL DEFAULT '{}',
    request_id   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_workspace_time ON audit_log(workspace_id, created_at DESC);
CREATE INDEX idx_audit_actor_time ON audit_log(actor_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS audit_log;
