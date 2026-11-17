-- name: CreateDocxDocument :one
-- Документ по DOCX-шаблону из запроса.
INSERT INTO documents (
    public_id, api_client_id, project, template_name, template_sha256, external_id, requested_by,
    format, file_key, file_sha256, file_size, data, idempotency_key, request_sha256, expires_at
) VALUES (
             $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
         )
RETURNING *;

-- name: GetDocumentByIdempotencyKey :one
SELECT * FROM documents
WHERE api_client_id = $1 AND idempotency_key = $2;

-- name: GetProjectDocument :one
-- Портал видит только документы своего проекта.
SELECT * FROM documents
WHERE id = $1 AND project = $2;

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
