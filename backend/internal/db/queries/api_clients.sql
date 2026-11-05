-- name: CreateAPIClient :one
INSERT INTO api_clients (name, project, key_hash, key_prefix)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetActiveAPIClientByKeyHash :one
SELECT * FROM api_clients WHERE key_hash = $1 AND revoked_at IS NULL;
