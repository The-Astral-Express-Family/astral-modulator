-- 00017_document_versions: 文档历史版本表（TODO §1.2 P3 解冻；sync-semantics
-- §8 的「有限 revision window」从冲突行扩展为独立版本链）。
-- 每次文档内容被取代（push 快进 / resolve 落地 / 复活 / tombstone）前，同事务
-- 把被取代的行归档为一条全量快照；当前版本仍以 documents 行为准，历史从
-- 迁移上线起算。path 冗余存储（conflicts 同惯例）：按路径查历史免 join。
-- 保留窗口 = TTL + 每文档上限（先到即剪），由 retention 清扫器执行。

-- +goose Up
CREATE TABLE document_versions (
    id            TEXT PRIMARY KEY,          -- dvh_
    workspace_id  TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    document_id   TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    path          TEXT NOT NULL,
    revision      BIGINT NOT NULL,           -- 被取代的版本号
    content_hash  TEXT NOT NULL,
    content       TEXT NOT NULL,             -- 全量快照；delta 压缩是后续演进
    kind          TEXT NOT NULL CHECK (kind IN ('push','resolve','revive','delete')),
    deleted       BOOLEAN NOT NULL DEFAULT false,  -- 快照时刻行的 tombstone 状态
    actor_id      TEXT NOT NULL REFERENCES actors(id),  -- 造成取代的 actor
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (document_id, revision)
);

CREATE INDEX idx_document_versions_workspace ON document_versions(workspace_id, created_at DESC);
CREATE INDEX idx_document_versions_doc ON document_versions(document_id, revision DESC);

-- +goose Down
DROP TABLE IF EXISTS document_versions;
