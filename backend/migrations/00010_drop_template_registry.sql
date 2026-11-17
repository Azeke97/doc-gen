-- +goose Up

-- ТЗ ред. 2: реестра шаблонов в сервисе нет, шаблон приходит в каждом запросе.
-- Документы, выпущенные по старому реестру, удаляются вместе с ним (их файлы в хранилище
-- остаются: миграция не ходит в S3).
DELETE FROM documents WHERE template_version_id IS NOT NULL;

DROP TRIGGER documents_version_published ON documents;
DROP FUNCTION check_document_version_published();

ALTER TABLE documents
    DROP CONSTRAINT documents_template_source_check,
    DROP COLUMN template_id,
    DROP COLUMN template_version_id,
    ALTER COLUMN template_name SET NOT NULL,
    ALTER COLUMN template_sha256 SET NOT NULL;

DROP TRIGGER templates_active_version_published ON templates;
DROP FUNCTION check_active_version_published();
ALTER TABLE templates DROP CONSTRAINT templates_active_version_fkey;
DROP TABLE template_versions;
DROP FUNCTION forbid_published_version_change();
DROP TABLE templates;

-- +goose Down
-- Реестр шаблонов и документы по нему удалены безвозвратно: откатывать некуда.
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION 'migration 00010 cannot be rolled back: the template registry and its documents were removed';
END $$;
-- +goose StatementEnd
