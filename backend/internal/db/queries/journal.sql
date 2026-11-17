-- name: LockTemplateFile :exec
-- Запись о копии шаблона: одна на SHA-256. DO UPDATE (а не DO NOTHING) нужен ради блокировки
-- строки до конца транзакции: очистка пропускает заблокированные копии и не удалит ту,
-- на которую сейчас ссылается выпускаемый документ.
INSERT INTO template_files (sha256, file_key, size)
VALUES ($1, $2, $3)
ON CONFLICT (sha256) DO UPDATE SET file_key = EXCLUDED.file_key;

-- name: LockOrphanTemplateFiles :many
-- Копии шаблонов, на которые не ссылается ни один документ (все удалены по сроку хранения).
SELECT sha256, file_key FROM template_files t
WHERE NOT EXISTS (SELECT 1 FROM documents d WHERE d.template_sha256 = t.sha256)
ORDER BY created_at
LIMIT $1
FOR UPDATE SKIP LOCKED;

-- name: DeleteTemplateFiles :exec
DELETE FROM template_files
WHERE sha256 = ANY(@shas::bytea[]);

-- name: GetTemplateFile :one
SELECT * FROM template_files
WHERE sha256 = $1;

-- name: GetProjectRetention :one
SELECT retention_days FROM projects
WHERE code = $1;

-- name: CreateGenerationLog :exec
INSERT INTO generation_log (
    api_client_id, project, request_id, template_name, template_sha256, format, external_id, requested_by,
    idempotency_key, status, http_status, error_code, error_message, error_details, document_id, result_size, duration_ms
) VALUES (
             $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17
         );
