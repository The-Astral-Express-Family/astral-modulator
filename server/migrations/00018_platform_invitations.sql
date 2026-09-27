-- 00018_platform_invitations: 平台级注册邀请（用户管理页签发）。
-- 与 00013 的 workspace 邀请互补：平台邀请只管「允许注册」，兑换后是
-- 普通 user，不自动加入任何 workspace（无 workspace_id、无 role 列）。
-- 邀请码只存 sha256，明文仅在签发响应出现一次；expired 是派生态
-- （status='invited' 且 now > expires_at），不落库。

-- +goose Up
CREATE TABLE platform_invitations (
    id           TEXT PRIMARY KEY,              -- inv_（与 workspace 邀请共用前缀，不同表空间）
    code_hash    TEXT NOT NULL UNIQUE,          -- sha256(归一化邀请码)
    created_by   TEXT NOT NULL REFERENCES actors(id),
    status       TEXT NOT NULL DEFAULT 'invited'
                 CHECK (status IN ('invited','redeemed','revoked')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,          -- TTL 由应用层约束（默认 7d，上限 30d）
    redeemed_by  TEXT REFERENCES actors(id),
    redeemed_at  TIMESTAMPTZ
);

CREATE INDEX idx_platform_invitations_status_id ON platform_invitations(status, id);

-- +goose Down
DROP TABLE platform_invitations;
