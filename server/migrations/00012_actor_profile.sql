-- 00012_actor_profile: actor 资料字段（头像外链 + 个性签名）。
-- 头像仅存外链 URL（本服务器不做上传/静态服务）；NULL = 未设置。

-- +goose Up
ALTER TABLE actors ADD COLUMN bio TEXT NOT NULL DEFAULT '';
ALTER TABLE actors ADD COLUMN avatar_url TEXT;

-- +goose Down
ALTER TABLE actors DROP COLUMN avatar_url;
ALTER TABLE actors DROP COLUMN bio;
