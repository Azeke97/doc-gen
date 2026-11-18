-- name: CreateAPIClient :one
INSERT INTO api_clients (name, project, key_hash, key_prefix)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetActiveAPIClientByKeyHash :one
SELECT * FROM api_clients WHERE key_hash = $1 AND revoked_at IS NULL;

-- name: ListAPIClients :many
-- Ключи для keygen -list и админки: сам ключ не хранится, видно только начало.
SELECT id, name, project, key_prefix, created_at, revoked_at
FROM api_clients
WHERE sqlc.narg(project)::text IS NULL OR project = sqlc.narg(project)
ORDER BY project, created_at;

-- name: RevokeAPIClient :one
-- Отозванный ключ больше не принимается; строка остаётся, на неё ссылаются документы и журнал.
UPDATE api_clients
SET revoked_at = now()
WHERE id = $1 AND revoked_at IS NULL
RETURNING id, name, project, key_prefix, created_at, revoked_at;
