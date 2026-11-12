-- name: CreateTemplate :one
INSERT INTO templates (project, code, locale, name, engine, retention_days)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListTemplates :many
-- Фильтр по проекту необязательный: NULL — все проекты.
SELECT * FROM templates
WHERE sqlc.narg('project')::text IS NULL OR project = sqlc.narg('project')
ORDER BY project, code, locale;
