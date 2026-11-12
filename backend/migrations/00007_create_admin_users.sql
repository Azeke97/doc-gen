-- +goose Up
CREATE TABLE admin_users (
                             id            uuid PRIMARY KEY DEFAULT uuidv7(),
                             email         text NOT NULL UNIQUE CHECK (email = lower(email) AND email LIKE '%@%'),
                             name          text NOT NULL,
                             password_hash text NOT NULL,
                             disabled_at   timestamptz,
                             created_at    timestamptz NOT NULL DEFAULT now(),
                             updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER admin_users_set_updated_at
    BEFORE UPDATE ON admin_users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Сессии входа в админку. Храним хэш токена, а не сам токен — как у API-ключей.
CREATE TABLE admin_sessions (
                                id         uuid PRIMARY KEY DEFAULT uuidv7(),
                                user_id    uuid NOT NULL REFERENCES admin_users (id) ON DELETE CASCADE,
                                token_hash bytea NOT NULL UNIQUE,
                                expires_at timestamptz NOT NULL,
                                created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX admin_sessions_user_idx ON admin_sessions (user_id);

-- +goose Down
DROP TABLE admin_sessions;
DROP TABLE admin_users;
