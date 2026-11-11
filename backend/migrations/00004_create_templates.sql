-- +goose Up
CREATE TABLE templates (
                           id                uuid PRIMARY KEY DEFAULT uuidv7(),
                           project           text NOT NULL REFERENCES projects (code),
                           code              text NOT NULL CHECK (code ~ '^[a-z][a-z0-9_]{1,99}$'),
                           locale            text NOT NULL CHECK (locale IN ('ru', 'kk', 'bilingual')),
                           name              text NOT NULL,
                           engine            text NOT NULL CHECK (engine IN ('docx', 'html')),
                           status            text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'archived')),
                           retention_days    integer NOT NULL CHECK (retention_days > 0),
                           active_version_id uuid,
                           created_at        timestamptz NOT NULL DEFAULT now(),
                           updated_at        timestamptz NOT NULL DEFAULT now(),

                           UNIQUE (project, code, locale),
                           CHECK (status <> 'active' OR active_version_id IS NOT NULL)
);

CREATE TRIGGER templates_set_updated_at
    BEFORE UPDATE ON templates
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE templates;