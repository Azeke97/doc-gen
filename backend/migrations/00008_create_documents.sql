-- +goose Up
CREATE TABLE documents (
                           id                  uuid PRIMARY KEY DEFAULT uuidv7(),
                           public_id           text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9a-f]{32}$'),
                           api_client_id       uuid NOT NULL REFERENCES api_clients (id),
                           project             text NOT NULL REFERENCES projects (code),
                           template_id         uuid NOT NULL,
                           template_version_id uuid NOT NULL,
                           external_id         text,
                           requested_by        text,
                           format              text NOT NULL CHECK (format IN ('pdf', 'docx')),
                           file_key            text NOT NULL,
                           file_sha256         bytea NOT NULL CHECK (length(file_sha256) = 32),
                           file_size           bigint NOT NULL CHECK (file_size > 0),
                           data                jsonb NOT NULL,
                           idempotency_key     text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
                           request_sha256      bytea NOT NULL CHECK (length(request_sha256) = 32),
                           created_at          timestamptz NOT NULL DEFAULT now(),
                           expires_at          timestamptz NOT NULL,

    -- Версия обязана принадлежать шаблону, на который ссылается документ.
                           FOREIGN KEY (template_id, template_version_id) REFERENCES template_versions (template_id, id),
                           UNIQUE (api_client_id, idempotency_key),
                           CHECK (expires_at > created_at)
);

-- Журнал документов (ТЗ, Ф15): по проекту и по шаблону за период; поиск по сущности портала.
CREATE INDEX documents_project_created_idx ON documents (project, created_at DESC);
CREATE INDEX documents_template_created_idx ON documents (template_id, created_at DESC);
CREATE INDEX documents_external_idx ON documents (project, external_id) WHERE external_id IS NOT NULL;
CREATE INDEX documents_expires_idx ON documents (expires_at);

-- Выпущенный документ не меняется (ТЗ, Ф2, Ф10): удалить его можно только по истечении срока хранения.
-- +goose StatementBegin
CREATE FUNCTION forbid_document_update() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'document % cannot be changed', OLD.id;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER documents_immutable
    BEFORE UPDATE ON documents
    FOR EACH ROW EXECUTE FUNCTION forbid_document_update();

-- Документ выпускается только по опубликованной версии.
-- +goose StatementBegin
CREATE FUNCTION check_document_version_published() RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM template_versions
        WHERE id = NEW.template_version_id AND status = 'published'
    ) THEN
        RAISE EXCEPTION 'template version % is not published', NEW.template_version_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER documents_version_published
    BEFORE INSERT ON documents
    FOR EACH ROW EXECUTE FUNCTION check_document_version_published();

-- +goose Down
DROP TABLE documents;
DROP FUNCTION check_document_version_published();
DROP FUNCTION forbid_document_update();
