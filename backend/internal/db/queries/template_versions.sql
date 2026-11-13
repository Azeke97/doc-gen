-- name: LockTemplate :one
-- Блокирует строку шаблона до конца транзакции: две одновременные загрузки версий выстроятся в очередь.
SELECT * FROM templates
WHERE id = $1
    FOR UPDATE;

-- name: GetDraftVersion :one
SELECT * FROM template_versions
WHERE template_id = $1 AND status = 'draft';

-- name: NextVersionNumber :one
SELECT (COALESCE(MAX(version), 0) + 1)::int AS next
FROM template_versions
WHERE template_id = $1;

-- name: CreateTemplateVersion :one
INSERT INTO template_versions (template_id, version, file_key, file_sha256, data_schema, test_data, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ReplaceDraftVersion :one
UPDATE template_versions
SET file_key = $2, file_sha256 = $3, data_schema = $4, test_data = $5, created_by = $6
WHERE id = $1 AND status = 'draft'
RETURNING *;

-- name: ListTemplateVersions :many
SELECT * FROM template_versions
WHERE template_id = $1
ORDER BY version DESC;

-- name: GetTemplateVersion :one
SELECT * FROM template_versions
WHERE id = $1;

-- name: PublishVersion :one
-- Публикуется только черновик: если версия уже опубликована, строк не будет.
UPDATE template_versions
SET status = 'published', published_by = $2, published_at = now()
WHERE id = $1 AND status = 'draft'
RETURNING *;
