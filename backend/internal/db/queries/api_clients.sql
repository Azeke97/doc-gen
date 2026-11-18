-- name: CreateAPIClient :one
INSERT INTO api_clients (name, project, key_hash, key_prefix)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetActiveAPIClientByKeyHash :one
SELECT * FROM api_clients WHERE key_hash = $1 AND revoked_at IS NULL;

-- name: ListAPIClients :many
-- Ключи для keygen -list и админки: сам ключ не хранится, видно только начало.
-- used — по ключу есть документы или записи журнала, такой ключ нельзя удалить.
SELECT c.id, c.name, c.project, c.key_prefix, c.created_at, c.revoked_at,
       (EXISTS (SELECT 1 FROM documents d WHERE d.api_client_id = c.id)
           OR EXISTS (SELECT 1 FROM generation_log g WHERE g.api_client_id = c.id))::bool AS used
FROM api_clients c
WHERE sqlc.narg(project)::text IS NULL OR c.project = sqlc.narg(project)
ORDER BY c.project, c.created_at;

-- name: RevokeAPIClient :one
-- Отозванный ключ больше не принимается; строка остаётся, на неё ссылаются документы и журнал.
UPDATE api_clients
SET revoked_at = now()
WHERE id = $1 AND revoked_at IS NULL
RETURNING id, name, project, key_prefix, created_at, revoked_at;

-- name: DeleteAPIClient :one
-- Удаляется только отозванный ключ; если по нему есть документы или журнал, мешает внешний ключ.
DELETE FROM api_clients
WHERE id = $1 AND revoked_at IS NOT NULL
RETURNING id, name, project, key_prefix;
