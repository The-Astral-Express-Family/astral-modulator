-- 00002_auth_sessions: device flow、human session 与 agent credential。
-- 对应 docs/architecture.md §8（opaque access/refresh）、§9（agent credential）。
-- 库中只存 hash/verifier，永不存明文 secret。

-- +goose Up
CREATE TABLE device_authorizations (
    id            TEXT PRIMARY KEY,          -- dev_
    device_code_hash TEXT NOT NULL,          -- device_code 至少 128-bit 随机，只存 hash
    user_code     TEXT NOT NULL UNIQUE,      -- 人类输入用，限流+防枚举在应用层做
    client_type   TEXT NOT NULL CHECK (client_type IN ('cli','web')),
    status        TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending','approved','denied','exchanged','expired')),
    actor_id      TEXT REFERENCES actors(id),-- 浏览器完成登录后回填
    expires_at    TIMESTAMPTZ NOT NULL,      -- TTL 600s（architecture §8.2）
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    exchanged_at  TIMESTAMPTZ
);

CREATE TABLE sessions (
    id                TEXT PRIMARY KEY,      -- ses_
    actor_id          TEXT NOT NULL REFERENCES actors(id),
    client_type       TEXT NOT NULL CHECK (client_type IN ('cli','web')),
    -- opaque rotating refresh token 的 hash；轮换时更新并推进 family
    refresh_token_hash TEXT NOT NULL,
    family_id         TEXT NOT NULL,         -- 检测旧 refresh 重放时整族撤销（§8.3）
    -- TODO(phase-1): access token hash 与过期时间。MVP 可只存 refresh 侧 + 内存/短 TTL 校验，
    -- 定稿前在 ADR 里明确 access 校验路径（查库 or 缓存）。
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at        TIMESTAMPTZ NOT NULL,  -- refresh 上限 30 天，可配置收紧
    last_used_at      TIMESTAMPTZ,
    revoked_at        TIMESTAMPTZ,
    user_agent        TEXT,
    remote_addr       TEXT
);

CREATE INDEX idx_sessions_actor ON sessions(actor_id);
CREATE INDEX idx_sessions_family ON sessions(family_id);

CREATE TABLE credentials (
    id            TEXT PRIMARY KEY,          -- cred_
    actor_id      TEXT NOT NULL REFERENCES actors(id),
    kind          TEXT NOT NULL CHECK (kind IN ('agent','service')),
    secret_hash   TEXT NOT NULL,             -- 明文只在创建响应里出现一次
    workspace_id  TEXT REFERENCES workspaces(id), -- NULL = 全服务器范围（少见，需 scope 收紧）
    scopes        TEXT[] NOT NULL DEFAULT '{}',   -- 见 internal/modules/auth/scopes.go
    created_by    TEXT NOT NULL REFERENCES actors(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at    TIMESTAMPTZ,
    last_used_at  TIMESTAMPTZ,
    revoked_at    TIMESTAMPTZ
);

CREATE INDEX idx_credentials_actor ON credentials(actor_id);

-- +goose Down
DROP TABLE IF EXISTS credentials;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS device_authorizations;
