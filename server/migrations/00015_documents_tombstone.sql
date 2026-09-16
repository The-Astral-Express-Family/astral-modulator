-- 00015_documents_tombstone: documents 软删除（tombstone，round 38 TODO.md S1）。
-- deleted_at 非 NULL = tombstone：manifest 默认排除、include_deleted=true 才返回，
-- get 仍返回（deleted=true）；tombstone 后 push base_revision=0 可复活（置 NULL）。
-- 语义由应用层手动控制，model.Document.DeletedAt 为普通可空时间列（非 gorm 软删）。

-- +goose Up
ALTER TABLE documents ADD COLUMN deleted_at TIMESTAMPTZ NULL;

-- +goose Down
ALTER TABLE documents DROP COLUMN IF EXISTS deleted_at;
