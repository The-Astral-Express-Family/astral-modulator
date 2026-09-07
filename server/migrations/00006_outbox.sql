-- 00006_outbox: transactional outbox（docs/architecture.md §19）。
-- 业务写事务内同时 INSERT outbox，后台 dispatcher 读取后推给本实例 SSE 订阅者。
-- MVP 无 NATS/Redis；多实例跨广播是引入 JetStream 的触发条件，不提前做。

-- +goose Up
CREATE TABLE outbox (
    id           TEXT PRIMARY KEY,           -- evt_
    workspace_id TEXT,                       -- NULL = 服务器级事件
    type         TEXT NOT NULL,              -- 见 internal/modules/event/types.go 与 api/schemas/event.json
    actor_id     TEXT,
    payload      JSONB NOT NULL,             -- envelope 的 data 部分
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- dispatcher 用 LISTEN/NOTIFY 或轮询推进；sent_at NULL 表示未投递
    sent_at      TIMESTAMPTZ
);

CREATE INDEX idx_outbox_unsent ON outbox(id) WHERE sent_at IS NULL;
CREATE INDEX idx_outbox_workspace ON outbox(workspace_id, id);

-- +goose Down
DROP TABLE IF EXISTS outbox;
