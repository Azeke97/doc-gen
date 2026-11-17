-- name: SaveTemplateFile :exec
-- Копия шаблона сохраняется один раз на SHA-256; повторная загрузка того же файла ничего не меняет.
INSERT INTO template_files (sha256, file_key, size)
VALUES ($1, $2, $3)
ON CONFLICT (sha256) DO NOTHING;

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
