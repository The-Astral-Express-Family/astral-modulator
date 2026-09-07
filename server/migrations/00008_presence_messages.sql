-- 00008_presence_messages: presence 与消息（docs/architecture.md §6.7、§6.8）。
-- Presence 是短生命周期展示状态，Task Lease 才是所有权事实，两者严格分离。

-- +goose Up
CREATE TABLE presence (
    actor_id          TEXT PRIMARY KEY REFERENCES actors(id) ON DELETE CASCADE,
    workspace_id      TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    -- TODO(phase-4): 'offline' 不落库——由读路径把 expires_at < now() 视为 offline。
    state             TEXT NOT NULL
                      CHECK (state IN ('idle','planning','working','waiting','blocked','reviewing')),
    current_task_id   TEXT REFERENCES tasks(id),
    note              TEXT,
    last_heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at        TIMESTAMPTZ NOT NULL   -- ttl 上限由服务端钳制（protocol §11）
);

CREATE INDEX idx_presence_workspace ON presence(workspace_id);

CREATE TABLE messages (
    id           TEXT PRIMARY KEY,           -- msg_
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    thread_id    TEXT REFERENCES messages(id), -- task thread 顶层为 NULL
    -- target_type+target_id 定向：actor 私信 / workspace 广播 / task thread
    target_type  TEXT NOT NULL CHECK (target_type IN ('actor','workspace','task')),
    target_id    TEXT NOT NULL,
    sender_id    TEXT NOT NULL REFERENCES actors(id),
    body         TEXT NOT NULL CHECK (char_length(body) BETWEEN 1 AND 20000),
    metadata     JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_messages_target ON messages(target_type, target_id, created_at DESC);
CREATE INDEX idx_messages_thread ON messages(thread_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS presence;
