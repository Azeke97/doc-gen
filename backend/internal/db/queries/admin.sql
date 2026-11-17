-- name: CreateAdminUser :one
INSERT INTO admin_users (email, name, password_hash)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetActiveAdminUserByEmail :one
SELECT * FROM admin_users
WHERE email = $1 AND disabled_at IS NULL;

-- name: CreateAdminSession :one
INSERT INTO admin_sessions (user_id, token_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetAdminBySessionToken :one
SELECT u.id, u.email, u.name
FROM admin_sessions s
         JOIN admin_users u ON u.id = s.user_id
WHERE s.token_hash = $1
  AND s.expires_at > now()
  AND u.disabled_at IS NULL;

-- name: DeleteAdminSession :exec
DELETE FROM admin_sessions
WHERE token_hash = $1;

-- name: UpsertAdminUser :one
-- Тестовая учётная запись бета-теста: создаётся или получает пароль из настроек заново.
INSERT INTO admin_users (email, name, password_hash)
VALUES ($1, $2, $3)
ON CONFLICT (email) DO UPDATE
    SET name = EXCLUDED.name, password_hash = EXCLUDED.password_hash, disabled_at = NULL
RETURNING *;

-- name: DisableAdminUser :one
-- Блокировка: входить больше нельзя, открытые сессии закрываются отдельным запросом.
UPDATE admin_users
SET disabled_at = now()
WHERE email = $1 AND disabled_at IS NULL
RETURNING id;

-- name: DeleteAdminUserSessions :exec
DELETE FROM admin_sessions
WHERE user_id = $1;
