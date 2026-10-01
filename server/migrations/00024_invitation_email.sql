-- 00024_invitation_email: 工作区邀请邮件投递（docs/registration.md 邮件邀请）。
-- email 非空 = 该邀请同时经邮件送达（链接与站内码同体：同一行同一 code_hash，
-- 撤销/过期/兑换两渠道同时失效）。email_sent_at 记投递成功时刻（NULL =
-- 未送达或未配置 SMTP——签发响应仍返回明文码，管理员可手动转发）。

-- +goose Up
ALTER TABLE workspace_invitations
    ADD COLUMN email         TEXT,      -- 小写归一化；可空 = 纯站内分发
    ADD COLUMN email_sent_at TIMESTAMPTZ;

CREATE INDEX idx_invitations_ws_email ON workspace_invitations(workspace_id, email)
    WHERE email IS NOT NULL;

-- +goose Down
DROP INDEX idx_invitations_ws_email;
ALTER TABLE workspace_invitations
    DROP COLUMN email_sent_at,
    DROP COLUMN email;
