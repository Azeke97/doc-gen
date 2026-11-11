-- +goose Up
CREATE TABLE projects (
                          code       text PRIMARY KEY CHECK (code ~ '^[a-z][a-z0-9_]{1,49}$'),
                          name       text NOT NULL,
                          created_at timestamptz NOT NULL DEFAULT now(),
                          updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER projects_set_updated_at
    BEFORE UPDATE ON projects
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Проекты, на которые уже ссылаются выданные ключи, переносим в новую таблицу,
-- иначе внешний ключ ниже не создастся.
INSERT INTO projects (code, name)
SELECT DISTINCT project, project FROM api_clients;

ALTER TABLE api_clients
    ADD CONSTRAINT api_clients_project_fkey FOREIGN KEY (project) REFERENCES projects (code);

-- +goose Down
ALTER TABLE api_clients DROP CONSTRAINT api_clients_project_fkey;
DROP TABLE projects;