-- 00003_task_tree: TODO 树、claim lease、搜索支撑。
-- 对应 docs/architecture.md §12（adjacency list）、§13（regex→fuzzy）、§17（lease）。

-- +goose Up
CREATE TABLE tasks (
    id          TEXT PRIMARY KEY,            -- tsk_
    workspace_id TEXT NOT NULL REFERENCES workspaces(id),
    parent_id   TEXT REFERENCES tasks(id),   -- 必须同 workspace，应用层+触发器双保险
    title       TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 500),
    description TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'open'
                CHECK (status IN ('open','in_progress','blocked','review','done','cancelled')),
    priority    TEXT NOT NULL DEFAULT 'normal'
                CHECK (priority IN ('low','normal','high','urgent')),
    assignee_actor_id TEXT REFERENCES actors(id),
    -- revision 每次业务修改递增；乐观并发靠 expected_revision 比对（protocol §6）
    revision    BIGINT NOT NULL DEFAULT 1,
    created_by  TEXT NOT NULL REFERENCES actors(id),
    updated_by  TEXT REFERENCES actors(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_tasks_workspace_parent ON tasks(workspace_id, parent_id);
CREATE INDEX idx_tasks_workspace_status ON tasks(workspace_id, status);
-- TODO(phase-3): title 的 trigram GIN 索引（regex/fuzzy 搜索热路径）：
--   CREATE INDEX idx_tasks_title_trgm ON tasks USING gin (title gin_trgm_ops);
--   放到搜索实现时一并加，并配套 statement_timeout 防危险 regex（architecture §13）。

-- 同 workspace 父子约束：拒绝跨 workspace parent（应用层先校验给友好错误，此为纵深防御）。
-- StatementBegin/End 必须加：goose 默认按分号切语句，函数体的 `$$ ... $$`
-- 内含分号会被拦腰截断（CI postgres migration test 实测报 unterminated dollar quote）。
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION assert_task_parent_same_workspace() RETURNS trigger AS $$
BEGIN
    IF NEW.parent_id IS NOT NULL THEN
        IF NOT EXISTS (SELECT 1 FROM tasks p WHERE p.id = NEW.parent_id AND p.workspace_id = NEW.workspace_id) THEN
            RAISE EXCEPTION 'task parent must belong to the same workspace';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_task_parent_same_workspace
BEFORE INSERT OR UPDATE OF parent_id ON tasks
FOR EACH ROW EXECUTE FUNCTION assert_task_parent_same_workspace();

-- 注意：revision 自增在应用层 UPDATE 内显式执行（TODO.md D5：可移植可测），
-- 不使用触发器。乐观并发：UPDATE ... WHERE id=$1 AND revision=$expected。

CREATE TABLE task_leases (
    task_id        TEXT PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
    holder_actor_id TEXT NOT NULL REFERENCES actors(id),
    -- TODO(phase-3): 后台清扫器把过期 lease 置为 expired 并发出 task.lease.expired 事件；
    -- 读路径必须把 expires_at < now() 视为无主。
    expires_at     TIMESTAMPTZ NOT NULL,
    renewed_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS task_leases;
DROP TRIGGER IF EXISTS trg_task_parent_same_workspace ON tasks;
DROP FUNCTION IF EXISTS assert_task_parent_same_workspace();
DROP TABLE IF EXISTS tasks;
