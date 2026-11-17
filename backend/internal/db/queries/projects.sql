-- name: CreateProject :one
INSERT INTO projects (code, name, retention_days)
VALUES (@code, @name, COALESCE(sqlc.narg(retention_days)::integer, 365))
RETURNING *;

-- name: UpdateProject :one
-- Пустые поля не меняются.
UPDATE projects
SET name           = COALESCE(sqlc.narg(name), name),
    retention_days = COALESCE(sqlc.narg(retention_days)::integer, retention_days)
WHERE code = @code
RETURNING *;

-- name: ListProjects :many
SELECT * FROM projects
ORDER BY code;
