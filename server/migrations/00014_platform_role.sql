-- 00014_platform_role: 平台全局角色层（TODO.md round 33）。
-- platform_role 与 actors.kind 分轴：human ∈ {admin,user}（admin 为特权角色）；
-- agent/service 仅固化 kind（CHECK 钉死，agent 归属仍按 D9 走 workspace）。
-- 授权一律按 GlobalScopesFor(platform_role) 计算的 scope 集合判定，
-- 不信任客户端传的 role 字符串（scopes.go / architecture §10）。
-- disabled_at 为 admin 用户管理预留（round 34）；NULL = 正常。

-- +goose Up
ALTER TABLE actors ADD COLUMN platform_role TEXT NOT NULL DEFAULT 'user';
UPDATE actors SET platform_role = kind WHERE kind <> 'human';
-- 存量部署：最早注册的 human 即 bootstrap 管理员（invite P1 之前它是唯一
-- 建号通道），随迁移一并授予 admin，避免升级后出现「无 admin」死局。
UPDATE actors SET platform_role = 'admin'
WHERE kind = 'human'
  AND id = (SELECT id FROM actors WHERE kind = 'human' ORDER BY created_at ASC LIMIT 1);
ALTER TABLE actors ADD CONSTRAINT actors_platform_role_kind_check CHECK (
    (kind = 'human' AND platform_role IN ('admin','user'))
    OR (kind IN ('agent','service') AND platform_role = kind)
);
ALTER TABLE actors ADD COLUMN disabled_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE actors DROP COLUMN disabled_at;
ALTER TABLE actors DROP CONSTRAINT actors_platform_role_kind_check;
ALTER TABLE actors DROP COLUMN platform_role;
