-- name: CreateTemplate :one
INSERT INTO templates (project, code, locale, name, engine, retention_days)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetTemplate :one
SELECT * FROM templates
WHERE id = $1;

-- name: ListTemplates :many
-- Фильтр по проекту необязательный: NULL — все проекты.
SELECT * FROM templates
WHERE sqlc.narg('project')::text IS NULL OR project = sqlc.narg('project')
ORDER BY project, code, locale;

-- name: SetActiveVersion :one
UPDATE templates
SET active_version_id = $2, status = 'active'
WHERE id = $1
RETURNING *;

-- name: ArchiveTemplate :one
UPDATE templates
SET status = 'archived'
WHERE id = $1
RETURNING *;

-- name: ListActiveTemplatesByProject :many
-- Каталог для портала: только активные шаблоны его проекта и номер активной версии.
SELECT t.code, t.locale, t.name, t.engine, v.version AS active_version
FROM templates t
JOIN template_versions v ON v.id = t.active_version_id
WHERE t.project = $1 AND t.status = 'active'
ORDER BY t.code, t.locale;

-- name: ResolveTemplateVersion :many
-- Версия шаблона, которую портал просит по коду: активная или конкретная опубликованная.
-- Видны только активные шаблоны своего проекта. Без локали может найтись несколько строк —
-- по одной на каждую локаль кода; выбирать за портал сервис не будет.
SELECT t.id AS template_id, t.code, t.locale, t.engine, t.retention_days,
       v.id AS version_id, v.version, v.file_key, v.data_schema
FROM templates t
JOIN template_versions v ON v.template_id = t.id
WHERE t.project = sqlc.arg('project')
  AND t.code = sqlc.arg('code')
  AND t.status = 'active'
  AND (sqlc.narg('locale')::text IS NULL OR t.locale = sqlc.narg('locale'))
  AND v.status = 'published'
  AND CASE
          WHEN sqlc.narg('version')::int IS NULL THEN v.id = t.active_version_id
          ELSE v.version = sqlc.narg('version')
      END
ORDER BY t.locale;
