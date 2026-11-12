-- name: CreateProject :one
INSERT INTO projects (code, name)
VALUES ($1, $2)
RETURNING *;

-- name: ListProjects :many
SELECT * FROM projects
ORDER BY code;
