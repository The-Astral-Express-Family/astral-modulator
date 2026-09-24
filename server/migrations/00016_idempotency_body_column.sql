-- 00016_idempotency_body_column: 修复 00009 与 GORM 模型的列名漂移。
--
-- 00009 建表时列名写作 response_body，而 model.IdempotencyKey.Body 的默认
-- 列名是 body（无 column tag）——INSERT/SELECT 全部按 body 发出，PostgreSQL
-- 部署下 42703 列不存在，幂等记录从未落档：同 key 重放语义（protocol.md
-- §4 声明的 24h 重放首次 2xx）在生产 PG 上完全不成立，且静默（仅 WARN 日志）。
-- sqlite 单测走 testsupport AutoMigrate（按模型建列），永远发现不了。
--
-- 发现路径：astral-cli 仓 round 39 E2E（真实 PG 集群 + 双仓库联动验证，
-- CLI 侧 document push 的确定性 Idempotency-Key 重放回归暴露）。

-- +goose Up
ALTER TABLE idempotency_keys RENAME COLUMN response_body TO body;

-- +goose Down
ALTER TABLE idempotency_keys RENAME COLUMN body TO response_body;
