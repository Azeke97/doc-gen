-- +goose Up
CREATE TABLE template_versions (
                                   id           uuid PRIMARY KEY DEFAULT uuidv7(),
                                   template_id  uuid NOT NULL REFERENCES templates (id),
                                   version      integer NOT NULL CHECK (version > 0),
                                   status       text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')),
                                   file_key     text NOT NULL,
                                   file_sha256  bytea NOT NULL,
                                   data_schema  jsonb NOT NULL,
                                   test_data    jsonb,
                                   created_by   text NOT NULL,
                                   created_at   timestamptz NOT NULL DEFAULT now(),
                                   updated_at   timestamptz NOT NULL DEFAULT now(),
                                   published_by text,
                                   published_at timestamptz,

                                   UNIQUE (template_id, version),
                                   UNIQUE (template_id, id),
                                   CHECK ((status = 'published') = (published_at IS NOT NULL AND published_by IS NOT NULL))
);

-- У шаблона может быть не больше одного черновика.
CREATE UNIQUE INDEX template_versions_one_draft_idx
    ON template_versions (template_id)
    WHERE status = 'draft';

CREATE TRIGGER template_versions_set_updated_at
    BEFORE UPDATE ON template_versions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Опубликованная версия неизменяема (ТЗ, Ф2): запрещаем UPDATE и DELETE на уровне БД.
-- +goose StatementBegin
CREATE FUNCTION forbid_published_version_change() RETURNS trigger AS $$
BEGIN
    IF OLD.status = 'published' THEN
        RAISE EXCEPTION 'template version % is published and cannot be changed', OLD.id;
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER template_versions_immutable
    BEFORE UPDATE OR DELETE ON template_versions
    FOR EACH ROW EXECUTE FUNCTION forbid_published_version_change();

-- Активная версия должна принадлежать этому же шаблону: составной внешний ключ.
ALTER TABLE templates
    ADD CONSTRAINT templates_active_version_fkey
        FOREIGN KEY (id, active_version_id) REFERENCES template_versions (template_id, id);

-- Активной можно сделать только опубликованную версию.
-- +goose StatementBegin
CREATE FUNCTION check_active_version_published() RETURNS trigger AS $$
BEGIN
    IF NEW.active_version_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM template_versions
        WHERE id = NEW.active_version_id AND status = 'published'
    ) THEN
        RAISE EXCEPTION 'template version % is not published and cannot be active', NEW.active_version_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER templates_active_version_published
    BEFORE INSERT OR UPDATE OF active_version_id ON templates
    FOR EACH ROW EXECUTE FUNCTION check_active_version_published();

-- +goose Down
DROP TRIGGER templates_active_version_published ON templates;
DROP FUNCTION check_active_version_published();
ALTER TABLE templates DROP CONSTRAINT templates_active_version_fkey;
DROP TABLE template_versions;
DROP FUNCTION forbid_published_version_change();
