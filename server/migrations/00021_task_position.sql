-- 00021_task_position: 任务兄弟排序键（协议 2.4，TODO.md §9 2026-09-30）。
-- tasks.position = 同一父容器子层内 0 起的序位；children 集合按
-- (position ASC, id DESC) 返回。老数据回填 = 创建正序（此前展示序为 id 倒序，
-- 回填后统一收敛为 position 序，此后创建一律追加兄弟尾部）。
-- 兄弟位移（创建/移动）由应用层在同一事务内维护稠密序位；position 不参与
-- revision（仅被移动任务自身 bump）。

-- +goose Up
ALTER TABLE tasks ADD COLUMN position BIGINT NOT NULL DEFAULT 0;

WITH ranked AS (
    SELECT id,
           ROW_NUMBER() OVER (
               PARTITION BY workspace_id, COALESCE(parent_id, '(root)')
               ORDER BY id ASC
           ) - 1 AS rn
    FROM tasks
)
UPDATE tasks SET position = ranked.rn FROM ranked WHERE tasks.id = ranked.id;

CREATE INDEX idx_tasks_sibling_order ON tasks(workspace_id, parent_id, position);

-- +goose Down
DROP INDEX IF EXISTS idx_tasks_sibling_order;
ALTER TABLE tasks DROP COLUMN position;
