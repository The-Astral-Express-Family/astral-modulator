-- 00020_task_dependencies: 任务依赖边（协议 2.2，TODO.md §9 2026-09-30）。
-- from = 依赖方（等待者），to = 被依赖方（blocker）；kind=blocks 为硬阻塞
-- （防环校验仅沿 blocks 边），kind=relates 为对称关联（无方向语义）。
-- 编号跳过 00019（预留给并行的兄弟排序变更，合并时按先到者就位）。
-- 边的写入/删除走三件套同事务，两端任务各 bump revision 并发 task.updated
-- （D11 tag 语义：任务事实变更必须可见于乐观并发与 SSE 同步）。

-- +goose Up
CREATE TABLE task_dependencies (
    from_task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    to_task_id   TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('blocks', 'relates')),
    created_by   TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (from_task_id, to_task_id, kind)
);

CREATE INDEX idx_task_dependencies_to ON task_dependencies(to_task_id);
CREATE INDEX idx_task_dependencies_from_kind ON task_dependencies(from_task_id, kind);

-- +goose Down
DROP INDEX IF EXISTS idx_task_dependencies_from_kind;
DROP INDEX IF EXISTS idx_task_dependencies_to;
DROP TABLE IF EXISTS task_dependencies;
