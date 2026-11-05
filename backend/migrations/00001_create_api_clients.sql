-- +goose Up
CREATE TABLE api_clients (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    name text NOT NULL,
    project text NOT NULL,
    key_hash bytea NOT NULL UNIQUE,
    key_prefix text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz
);

-- +goose Down
DROP TABLE api_clients;
