-- 00010_presence_workspace_pk: presence 主键改为 (actor_id, workspace_id)。
-- 原表主键只有 actor_id，同一 actor 在第二个 workspace heartbeat 会覆盖
-- （甚至经由应用层先删后插路径清掉第一个 workspace 的 presence）。
-- presence 的 API 形状是按 workspace 列表/心跳，故一行 = (actor, workspace)。

-- +goose Up
ALTER TABLE presence DROP CONSTRAINT presence_pkey;
ALTER TABLE presence ADD PRIMARY KEY (actor_id, workspace_id);

-- +goose Down
-- 回滚有损：多 workspace 行无法还原为单行（保留任意一行）。
DELETE FROM presence p
USING presence q
WHERE p.actor_id = q.actor_id AND p.workspace_id > q.workspace_id;
ALTER TABLE presence DROP CONSTRAINT presence_pkey;
ALTER TABLE presence ADD PRIMARY KEY (actor_id);
