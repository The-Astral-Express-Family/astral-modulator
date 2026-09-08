-- 00009_idempotency_keys: 幂等键存储（docs/protocol.md §4 / architecture §21）。
-- 同一 Actor + endpoint + key 在保留窗口内重放首次 2xx 响应。
-- 保留窗口 24h，由清理任务周期删除。

-- +goose Up
CREATE TABLE idempotency_keys (
    actor_id      TEXT NOT NULL,             -- 发起者（credential 或 human）
    endpoint      TEXT NOT NULL,             -- method + route pattern，如 "POST /workspaces"
    key           TEXT NOT NULL,
    status_code   INTEGER NOT NULL,
    content_type  TEXT NOT NULL DEFAULT 'application/json; charset=utf-8',
    response_body BYTEA NOT NULL,             -- 首次 2xx 响应体（超限不缓存）
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (actor_id, endpoint, key)
);

CREATE INDEX idx_idempotency_created ON idempotency_keys(created_at);

-- +goose Down
DROP TABLE IF EXISTS idempotency_keys;
