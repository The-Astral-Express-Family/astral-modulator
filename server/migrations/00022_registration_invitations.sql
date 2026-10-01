-- +goose Up
-- 注册邀请与工作区邀请双轨分离（ADR-0009）：00018 的 platform_invitations
-- 更名为 registration_invitations——名字即语义。表结构不变（只管「允许注册」，
-- 不入任何 workspace）；存量行保留（rename 保数据），仅 id 前缀自此改用 reg_
-- （历史行残留 inv_，前缀只服务可读性）。工作区邀请仍在 00013 表，不受影响。
ALTER TABLE platform_invitations RENAME TO registration_invitations;
ALTER INDEX idx_platform_invitations_status_id RENAME TO idx_registration_invitations_status_id;

-- +goose Down
ALTER INDEX idx_registration_invitations_status_id RENAME TO idx_platform_invitations_status_id;
ALTER TABLE registration_invitations RENAME TO platform_invitations;
