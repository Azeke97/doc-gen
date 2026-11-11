-- +goose Up
CREATE TABLE audit_log (
                           id          uuid PRIMARY KEY DEFAULT uuidv7(),
                           occurred_at timestamptz NOT NULL DEFAULT now(),
                           actor       text NOT NULL,
                           action      text NOT NULL,
                           entity_type text NOT NULL,
                           entity_id   uuid NOT NULL,
                           details     jsonb NOT NULL DEFAULT '{}'
);

CREATE INDEX audit_log_entity_idx ON audit_log (entity_type, entity_id, occurred_at);

-- +goose Down
DROP TABLE audit_log;