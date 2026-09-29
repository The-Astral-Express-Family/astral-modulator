-- 00019_drop_task_leases: 任务租约时间维度拆除（TODO.md §9 2026-09-30）。
-- claim 互斥改为 tasks.assignee_actor_id 条件更新（持有至 release/任务完成），
-- 过期清扫 / renew / task.lease.expired 事件随之移除；原租约表无承载语义。

-- +goose Up
DROP TABLE IF EXISTS task_leases;

-- +goose Down
-- forward-only：租约语义已从协议移除，不提供回迁。
SELECT 1;
