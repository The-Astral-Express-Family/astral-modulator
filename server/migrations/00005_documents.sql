-- 00005_documents: 受管 Markdown 文档与冲突工件。
-- 对应 docs/architecture.md §16、docs/sync-semantics.md。
-- memory 工作区（architecture §6.5）也复用本表：组织记忆挂专用 workspace，
-- 项目记忆挂各自 workspace 的 memory/ 前缀路径 —— TODO(phase-5) 定稿路径策略。

-- +goose Up
CREATE TABLE documents (
    id            TEXT PRIMARY KEY,          -- doc_
    workspace_id  TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    -- path 服务端 canonicalize：'/'分隔、禁 '..'/绝对路径/secrets 默认路径（architecture §16）
    path          TEXT NOT NULL,
    revision      BIGINT NOT NULL DEFAULT 1,
    content_hash  TEXT NOT NULL,             -- 'sha256:<hex>'
    content       TEXT NOT NULL,             -- UTF-8 only；MVP 直接入库，不引对象存储
    updated_by    TEXT NOT NULL REFERENCES actors(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, path)
);

CREATE INDEX idx_documents_workspace ON documents(workspace_id, updated_at DESC);

CREATE TABLE document_conflicts (
    id            TEXT PRIMARY KEY,          -- cfl_
    workspace_id  TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    path          TEXT NOT NULL,
    base_revision BIGINT NOT NULL,
    base_hash     TEXT NOT NULL,
    -- 双方内容摘要/全文；resolve 时按 resolution 字段落地
    ours_json     JSONB NOT NULL,            -- push 方内容
    theirs_json   JSONB NOT NULL,            -- 服务端当前内容
    resolution    TEXT CHECK (resolution IN ('ours','theirs','merged','manual')),
    -- TODO(phase-5): diff3 merge 策略与 merged 内容字段，随 sync 语义定稿补充。
    resolved_by   TEXT REFERENCES actors(id),
    resolved_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_conflicts_workspace ON document_conflicts(workspace_id, resolved_at);

-- +goose Down
DROP TABLE IF EXISTS document_conflicts;
DROP TABLE IF EXISTS documents;
