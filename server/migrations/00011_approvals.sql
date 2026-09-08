-- 00011_approvals: server-side approval 状态机（architecture §22）。
-- 高风险动作（MVP：membership.promote_owner）先建请求，经 owner 批准后
-- 同事务执行。CLI 的 --yes 不能绕过（服务端是唯一裁决点）。

-- +goose Up
CREATE TABLE approvals (
    id              TEXT PRIMARY KEY,               -- apv_
    workspace_id    TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    action          TEXT NOT NULL,                  -- MVP 固定 'membership.promote_owner'
    target_actor_id TEXT NOT NULL REFERENCES actors(id),
    payload         JSONB NOT NULL DEFAULT '{}',    -- 动作参数快照（如 {"role":"owner"}）
    status          TEXT NOT NULL
                    CHECK (status IN ('requested','approved','rejected','expired','executed')),
    requested_by    TEXT NOT NULL REFERENCES actors(id),
    decided_by      TEXT REFERENCES actors(id),
    decided_at      TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ NOT NULL,           -- 过期由读路径惰性判定
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_approvals_workspace_status ON approvals(workspace_id, status);

-- +goose Down
DROP TABLE approvals;
