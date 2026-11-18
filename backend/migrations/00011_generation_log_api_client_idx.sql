-- +goose Up

-- Для удаления ключа: проверка внешнего ключа и «использовался ли ключ» без полного прохода по журналу.
CREATE INDEX generation_log_api_client_idx ON generation_log (api_client_id);

-- +goose Down

DROP INDEX generation_log_api_client_idx;
