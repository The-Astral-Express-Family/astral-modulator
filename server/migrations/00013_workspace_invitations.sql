-- 00013_workspace_invitations: 一次性邀请码注册（docs/registration.md，TODO.md A5）。
-- 邀请绑定 workspace 与角色（viewer/contributor/maintainer，永不含 owner）；
-- 邀请码只存 sha256，明文仅在签发响应出现一次；expired 是派生态
-- （status='invited' 且 now > expires_at），不落库。

-- +goose Up
CREATE TABLE workspace_invitations (
    id           TEXT PRIMARY KEY,              -- inv_
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    role         TEXT NOT NULL
                 CHECK (role IN ('viewer','contributor','maintainer')),
    code_hash    TEXT NOT NULL UNIQUE,          -- sha256(归一化邀请码)
    created_by   TEXT NOT NULL REFERENCES actors(id),
    status       TEXT NOT NULL DEFAULT 'invited'
                 CHECK (status IN ('invited','redeemed','revoked')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,          -- TTL 由应用层约束（默认 7d，上限 30d）
    redeemed_by  TEXT REFERENCES actors(id),
    redeemed_at  TIMESTAMPTZ
);

CREATE INDEX idx_invitations_workspace_status ON workspace_invitations(workspace_id, status);

-- +goose Down
DROP TABLE workspace_invitations;
