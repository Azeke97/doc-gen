-- name: LockTemplateFile :exec
-- Запись о копии шаблона: одна на SHA-256. DO UPDATE (а не DO NOTHING) нужен ради блокировки
-- строки до конца транзакции: очистка пропускает заблокированные копии и не удалит ту,
-- на которую сейчас ссылается выпускаемый документ.
INSERT INTO template_files (sha256, file_key, size)
VALUES ($1, $2, $3)
ON CONFLICT (sha256) DO UPDATE SET file_key = EXCLUDED.file_key;

-- name: LockOrphanTemplateFiles :many
-- Копии шаблонов, на которые не ссылается ни один документ (все удалены по сроку хранения).
SELECT sha256, file_key FROM template_files t
WHERE NOT EXISTS (SELECT 1 FROM documents d WHERE d.template_sha256 = t.sha256)
ORDER BY created_at
LIMIT $1
FOR UPDATE SKIP LOCKED;

-- name: DeleteTemplateFiles :exec
DELETE FROM template_files
WHERE sha256 = ANY(@shas::bytea[]);

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

-- name: ListGenerationLog :many
-- Журнал для админки: фильтры необязательны; страницы — по (created_at, id) убыванию:
-- следующая страница начинается после последней записи предыдущей.
SELECT sqlc.embed(g), p.name AS project_name
FROM generation_log g
         JOIN projects p ON p.code = g.project
WHERE (sqlc.narg(project)::text IS NULL OR g.project = sqlc.narg(project))
  AND (sqlc.narg(template_name)::text IS NULL OR g.template_name = sqlc.narg(template_name))
  AND (sqlc.narg(external_id)::text IS NULL OR g.external_id = sqlc.narg(external_id))
  AND (cardinality(@statuses::text[]) = 0 OR g.status = ANY (@statuses::text[]))
  AND (sqlc.narg(from_time)::timestamptz IS NULL OR g.created_at >= sqlc.narg(from_time))
  AND (sqlc.narg(to_time)::timestamptz IS NULL OR g.created_at < sqlc.narg(to_time))
  AND (sqlc.narg(after_time)::timestamptz IS NULL
    OR (g.created_at, g.id) < (sqlc.narg(after_time)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY g.created_at DESC, g.id DESC
LIMIT @page_size;

-- name: GetGenerationLog :one
SELECT sqlc.embed(g), p.name AS project_name
FROM generation_log g
         JOIN projects p ON p.code = g.project
WHERE g.id = $1;

-- name: GetDocument :one
SELECT * FROM documents
WHERE id = $1;

-- name: ReportTotals :one
-- Отчёт за период [from, to): число запросов по статусам и среднее время выпуска.
SELECT count(*)                                         AS requests,
       count(*) FILTER (WHERE status = 'created')       AS created,
       count(*) FILTER (WHERE status = 'repeated')      AS repeated,
       count(*) FILTER (WHERE status = 'rejected')      AS rejected,
       count(*) FILTER (WHERE status = 'failed')        AS failed,
       COALESCE(avg(duration_ms) FILTER (WHERE status = 'created'), 0)::integer AS avg_duration_ms
FROM generation_log
WHERE created_at >= @from_time AND created_at < @to_time
  AND (sqlc.narg(project)::text IS NULL OR project = sqlc.narg(project));

-- name: ReportByProject :many
SELECT g.project,
       p.name                                                   AS project_name,
       count(*) FILTER (WHERE g.status = 'created')             AS created,
       count(*) FILTER (WHERE g.status IN ('rejected', 'failed')) AS errors
FROM generation_log g
         JOIN projects p ON p.code = g.project
WHERE g.created_at >= @from_time AND g.created_at < @to_time
  AND (sqlc.narg(project)::text IS NULL OR g.project = sqlc.narg(project))
GROUP BY g.project, p.name
ORDER BY created DESC, g.project;

-- name: ReportByTemplate :many
-- Запросы, отклонённые до разбора полей, могут быть без имени шаблона — они в строке с пустым именем.
SELECT g.project,
       COALESCE(g.template_name, '')::text                         AS template_name,
       count(*) FILTER (WHERE g.status = 'created')                AS created,
       count(*) FILTER (WHERE g.status IN ('rejected', 'failed'))   AS errors,
       COALESCE(avg(g.duration_ms) FILTER (WHERE g.status = 'created'), 0)::integer AS avg_duration_ms
FROM generation_log g
WHERE g.created_at >= @from_time AND g.created_at < @to_time
  AND (sqlc.narg(project)::text IS NULL OR g.project = sqlc.narg(project))
GROUP BY g.project, g.template_name
ORDER BY created DESC, g.project, template_name
LIMIT 200;

-- name: ReportByDay :many
-- Дни — по времени Астаны, как в документах.
SELECT (g.created_at AT TIME ZONE 'Asia/Almaty')::date             AS day,
       count(*) FILTER (WHERE g.status = 'created')                AS created,
       count(*) FILTER (WHERE g.status IN ('rejected', 'failed'))   AS errors
FROM generation_log g
WHERE g.created_at >= @from_time AND g.created_at < @to_time
  AND (sqlc.narg(project)::text IS NULL OR g.project = sqlc.narg(project))
GROUP BY day
ORDER BY day;
