-- 00001_init: 扩展、服务器身份、actor 与 workspace 骨架。
-- 对应 docs/architecture.md §7（server_id 稳定）、§6.1、§6.2。
-- ID 均为应用层生成的带前缀字符串（internal/ids），不用自增主键，
-- 避免向客户端泄露规模信息（docs/protocol.md §3）。

-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm; -- TODO(phase-3): 任务模糊检索依赖，见 architecture §13

CREATE TABLE server_meta (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- server_id 首启生成一次，之后永久稳定（修改域名不得改变它）。
-- TODO(phase-1): 由 internal/store 在首启时 INSERT ON CONFLICT DO NOTHING，
-- 并在运行时始终读取库中值。

CREATE TABLE actors (
    id          TEXT PRIMARY KEY,            -- usr_ / agt_ / svc_
    kind        TEXT NOT NULL CHECK (kind IN ('human','agent','service')),
    display_name TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
    -- TODO(phase-2): human 认证方式关联（本地/OIDC）字段，取决于 auth 设计定稿。
);

CREATE TABLE workspaces (
    id          TEXT PRIMARY KEY,            -- ws_
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL,
    -- 精确 name/slug 解析（architecture §11：astral init 依赖 exact-or-slug 查询）
    UNIQUE (name),
    UNIQUE (slug),
    created_by  TEXT NOT NULL REFERENCES actors(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
    -- TODO(phase-2): 默认策略/repo metadata 字段（architecture §6.2）。
);

CREATE TABLE workspace_members (
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    actor_id     TEXT NOT NULL REFERENCES actors(id),
    -- role 只是 scope bundle 的名字，授权必须按最终 scope 计算（architecture §10）
    role         TEXT NOT NULL DEFAULT 'contributor'
                 CHECK (role IN ('viewer','contributor','agent','maintainer','owner')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, actor_id)
);

CREATE INDEX idx_workspace_members_actor ON workspace_members(actor_id);

-- +goose Down
DROP TABLE IF EXISTS workspace_members;
DROP TABLE IF EXISTS workspaces;
DROP TABLE IF EXISTS actors;
DROP TABLE IF EXISTS server_meta;
DROP EXTENSION IF EXISTS pg_trgm;
