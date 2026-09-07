-- 00004_tags: tag、两步确认 proposal、task-tag 关联。
-- 对应 docs/architecture.md §14（proposal/confirm；不做模糊相似度自动拒绝）。

-- +goose Up
CREATE TABLE tags (
    id              TEXT PRIMARY KEY,        -- tag_
    workspace_id    TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    normalized_name TEXT NOT NULL,           -- trim + Unicode NFC + case-fold（应用层算好再入库）
    created_by      TEXT NOT NULL REFERENCES actors(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, normalized_name),
    CHECK (char_length(name) BETWEEN 1 AND 64)
);

CREATE TABLE tag_proposals (
    id            TEXT PRIMARY KEY,          -- tgp_
    workspace_id  TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    actor_id      TEXT NOT NULL REFERENCES actors(id), -- confirm 必须同 actor
    action        TEXT NOT NULL CHECK (action IN ('create','rename','delete')),
    canonical_name TEXT NOT NULL,            -- confirm 时只接受完全一致的输入
    target_tag_id TEXT REFERENCES tags(id),
    -- confirm_code 只存 hash；TTL + 单次使用 + 绑定 request id（architecture §14）
    confirm_code_hash TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending','confirmed','expired','cancelled')),
    request_id    TEXT,
    expires_at    TIMESTAMPTZ NOT NULL,      -- 建议 TTL ~120s，只够“三思”，不够挂机
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    confirmed_at  TIMESTAMPTZ
);

CREATE INDEX idx_tag_proposals_workspace ON tag_proposals(workspace_id, status);

CREATE TABLE task_tags (
    task_id  TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    tag_id   TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    added_by TEXT NOT NULL REFERENCES actors(id),
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (task_id, tag_id)
);

CREATE INDEX idx_task_tags_tag ON task_tags(tag_id);

-- +goose Down
DROP TABLE IF EXISTS task_tags;
DROP TABLE IF EXISTS tag_proposals;
DROP TABLE IF EXISTS tags;
