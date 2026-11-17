-- +goose Up

-- Срок хранения документов теперь задаёт проект (ТЗ ред. 2, Ф10): это предел, запрос может
-- указать срок короче.
ALTER TABLE projects
    ADD COLUMN retention_days integer NOT NULL DEFAULT 365 CHECK (retention_days > 0);

-- Копии шаблонов, по которым выпущены документы: одна на каждый SHA-256. Шаблоны хранит портал,
-- а копия здесь нужна, чтобы любой документ можно было воспроизвести и разобрать.
CREATE TABLE template_files (
                                sha256     bytea PRIMARY KEY CHECK (length(sha256) = 32),
                                file_key   text NOT NULL UNIQUE,
                                size       bigint NOT NULL CHECK (size > 0),
                                created_at timestamptz NOT NULL DEFAULT now()
);

-- Документ по DOCX-шаблону из запроса: имя шаблона, как его называет портал, и SHA-256 файла.
-- Колонки старого реестра шаблонов становятся необязательными и уйдут вместе с ним.
ALTER TABLE documents
    ALTER COLUMN template_id DROP NOT NULL,
    ALTER COLUMN template_version_id DROP NOT NULL,
    ADD COLUMN template_name text CHECK (length(template_name) BETWEEN 1 AND 200),
    ADD COLUMN template_sha256 bytea REFERENCES template_files (sha256),
    -- Документ выпущен либо по версии реестра, либо по шаблону из запроса — третьего не дано.
    ADD CONSTRAINT documents_template_source_check CHECK (
        (template_version_id IS NOT NULL AND template_sha256 IS NULL)
            OR (template_version_id IS NULL AND template_sha256 IS NOT NULL AND template_name IS NOT NULL)
        );

CREATE INDEX documents_template_name_created_idx ON documents (project, template_name, created_at DESC)
    WHERE template_name IS NOT NULL;
-- Очистка удаляет копию шаблона, когда на неё больше не ссылается ни один документ.
CREATE INDEX documents_template_sha256_idx ON documents (template_sha256) WHERE template_sha256 IS NOT NULL;

-- Проверка опубликованной версии нужна только документам старого реестра.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION check_document_version_published() RETURNS trigger AS $$
BEGIN
    IF NEW.template_version_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM template_versions
        WHERE id = NEW.template_version_id AND status = 'published'
    ) THEN
        RAISE EXCEPTION 'template version % is not published', NEW.template_version_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- Журнал запросов на генерацию (ТЗ ред. 2, Ф11): каждый запрос, в том числе отклонённый.
-- Входные данные сюда не пишутся: они есть только у выпущенного документа и удаляются с ним.
CREATE TABLE generation_log (
                                id              uuid PRIMARY KEY DEFAULT uuidv7(),
                                created_at      timestamptz NOT NULL DEFAULT now(),
                                api_client_id   uuid NOT NULL REFERENCES api_clients (id),
                                project         text NOT NULL REFERENCES projects (code),
                                request_id      text NOT NULL, -- X-Request-Id: связывает запись с логами сервиса и Gotenberg
                                template_name   text,
                                template_sha256 bytea CHECK (length(template_sha256) = 32),
                                format          text,
                                external_id     text,
                                requested_by    text,
                                idempotency_key text,
                                -- created — выпущен новый документ, repeated — повтор по ключу идемпотентности,
                                -- rejected — ошибка запроса, шаблона или данных (4xx), failed — сбой сервиса (5xx).
                                status          text NOT NULL CHECK (status IN ('created', 'repeated', 'rejected', 'failed')),
                                http_status     integer NOT NULL,
                                error_code      text,
                                error_message   text,
                                error_details   jsonb, -- ошибки шаблона или данных по пунктам
                                -- Документ удаляется по сроку хранения, запись журнала остаётся.
                                document_id     uuid REFERENCES documents (id) ON DELETE SET NULL,
                                result_size     bigint,
                                duration_ms     integer NOT NULL CHECK (duration_ms >= 0),

                                -- Код ошибки есть ровно у неуспешных запросов. Ссылку на документ не проверяем:
                                -- после удаления документа по сроку она законно становится NULL.
                                CHECK ((status IN ('rejected', 'failed')) = (error_code IS NOT NULL))
);

-- Фильтры журнала (Ф12): период, портал, шаблон, статус; поиск по сущности портала.
CREATE INDEX generation_log_created_idx ON generation_log (created_at DESC);
CREATE INDEX generation_log_project_created_idx ON generation_log (project, created_at DESC);
CREATE INDEX generation_log_template_created_idx ON generation_log (project, template_name, created_at DESC);
CREATE INDEX generation_log_errors_idx ON generation_log (created_at DESC) WHERE status IN ('rejected', 'failed');
CREATE INDEX generation_log_external_idx ON generation_log (project, external_id) WHERE external_id IS NOT NULL;
CREATE INDEX generation_log_document_idx ON generation_log (document_id) WHERE document_id IS NOT NULL;

-- +goose Down
DROP TABLE generation_log;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION check_document_version_published() RETURNS trigger AS $$
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

-- Документы по шаблонам из запроса в старой схеме не помещаются: удаляются при откате.
DELETE FROM documents WHERE template_version_id IS NULL;
DROP INDEX documents_template_sha256_idx;
DROP INDEX documents_template_name_created_idx;
ALTER TABLE documents
    DROP CONSTRAINT documents_template_source_check,
    DROP COLUMN template_sha256,
    DROP COLUMN template_name,
    ALTER COLUMN template_version_id SET NOT NULL,
    ALTER COLUMN template_id SET NOT NULL;

DROP TABLE template_files;
ALTER TABLE projects DROP COLUMN retention_days;
