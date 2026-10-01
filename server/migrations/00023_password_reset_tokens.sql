-- 00023_password_reset_tokens: 忘记密码一次性重置 token（auth 模块 reset.go，
-- docs/security.md 密码重置节）。明文 prt_<32B> 只随邮件链接出服务器，库中
-- sha256；30 分钟过期；单活跃（新请求补写旧 token used_at）；兑换走条件更新。

-- +goose Up
CREATE TABLE password_reset_tokens (
    id            TEXT PRIMARY KEY,              -- prt_
    human_auth_id TEXT NOT NULL REFERENCES human_auth(actor_id) ON DELETE CASCADE,
    token_hash    TEXT NOT NULL UNIQUE,          -- sha256(明文 token)
    expires_at    TIMESTAMPTZ NOT NULL,          -- 签发起 30 分钟
    used_at       TIMESTAMPTZ,                   -- 非 NULL = 已兑换或已被新请求作废
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    request_ip    TEXT                           -- 签发来源 IP（审计排障，非授权依据）
);

CREATE INDEX idx_password_reset_human ON password_reset_tokens(human_auth_id);

-- +goose Down
DROP TABLE password_reset_tokens;
