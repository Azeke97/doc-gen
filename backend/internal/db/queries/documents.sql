-- name: CreateDocument :one
INSERT INTO documents (
    public_id, api_client_id, project, template_id, template_version_id, external_id, requested_by,
    format, file_key, file_sha256, file_size, data, idempotency_key, request_sha256, expires_at
) VALUES (
             $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
         )
RETURNING *;

-- name: GetDocumentByIdempotencyKey :one
SELECT * FROM documents
WHERE api_client_id = $1 AND idempotency_key = $2;

-- name: CreateDocxDocument :one
-- Документ по DOCX-шаблону из запроса.
INSERT INTO documents (
    public_id, api_client_id, project, template_name, template_sha256, external_id, requested_by,
    format, file_key, file_sha256, file_size, data, idempotency_key, request_sha256, expires_at
) VALUES (
             $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
         )
RETURNING *;

-- name: GetProjectDocument :one
-- Портал видит только документы своего проекта. Имя шаблона — как его назвал портал;
-- у документов старого реестра — код шаблона.
SELECT sqlc.embed(d), COALESCE(d.template_name, t.code)::text AS display_template_name
FROM documents d
         LEFT JOIN templates t ON t.id = d.template_id
WHERE d.id = $1 AND d.project = $2;

-- name: LockExpiredDocuments :many
-- Документы с истёкшим сроком хранения. SKIP LOCKED: несколько экземпляров сервиса
-- чистят одновременно и не берут одни и те же строки.
SELECT id, file_key FROM documents
WHERE expires_at <= $1
ORDER BY expires_at
LIMIT $2
FOR UPDATE SKIP LOCKED;

-- name: DeleteDocuments :exec
DELETE FROM documents
WHERE id = ANY(@ids::uuid[]);

-- name: GetDocumentByPublicID :one
-- Для публичной проверки: только то, что подтверждает подлинность, без данных документа.
SELECT d.public_id, d.format, d.file_sha256, d.file_size, d.created_at, d.expires_at,
       t.name AS template_name, p.name AS project_name
FROM documents d
         JOIN templates t ON t.id = d.template_id
         JOIN projects p ON p.code = d.project
WHERE d.public_id = $1;
