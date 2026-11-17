# Переменные из .env (direnv в make не работает).
# "-" — не падать, если .env нет; export — передать переменные во все команды.
-include .env
export

BACKEND := backend

.DEFAULT_GOAL := help

## ---------- Общее ----------

.PHONY: help
help: ## Показать список команд
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

## ---------- Инфраструктура (Docker) ----------

.PHONY: up
up: ## Поднять postgres, minio, gotenberg
	docker compose up -d

.PHONY: down
down: ## Остановить контейнеры (данные в томах сохраняются)
	docker compose down

.PHONY: ps
ps: ## Статус контейнеров
	docker compose ps

.PHONY: logs
logs: ## Логи контейнеров (make logs s=postgres — одного сервиса)
	docker compose logs -f $(s)

## ---------- Сервис ----------

.PHONY: run
run: ## Запустить сервис docgen
	cd $(BACKEND) && go run ./cmd/docgen

.PHONY: build
build: ## Собрать бинарники в backend/bin
	cd $(BACKEND) && go build -o bin/docgen ./cmd/docgen && go build -o bin/migrate ./cmd/migrate

## ---------- Админка (Vue, admin/) ----------

.PHONY: admin-install
admin-install: ## Установить зависимости админки
	cd admin && npm ci

.PHONY: admin-dev
admin-dev: ## Админка в режиме разработки на :5173 (API — запущенный make run)
	cd admin && npm run dev

.PHONY: admin-build
admin-build: ## Собрать админку в admin/dist (сервис раздаёт её по /admin/, если задан ADMIN_UI_DIR)
	cd admin && npm run build

## ---------- База данных ----------

.PHONY: migrate-up
migrate-up: ## Применить все новые миграции
	cd $(BACKEND) && go run ./cmd/migrate up

.PHONY: migrate-down
migrate-down: ## Откатить последнюю миграцию
	cd $(BACKEND) && go run ./cmd/migrate down

.PHONY: migrate-redo
migrate-redo: ## Откатить и заново применить последнюю миграцию
	cd $(BACKEND) && go run ./cmd/migrate redo

.PHONY: migrate-status
migrate-status: ## Статус миграций
	cd $(BACKEND) && go run ./cmd/migrate status

.PHONY: migration
migration: ## Создать миграцию: make migration name=create_something
	@test -n "$(name)" || (echo "usage: make migration name=create_something" && exit 1)
	cd $(BACKEND) && goose -dir migrations -s create $(name) sql

.PHONY: generate
generate: ## Сгенерировать код sqlc
	cd $(BACKEND) && sqlc generate

.PHONY: keygen
keygen: ## Выдать API-ключ: make keygen name="Портал" project=procurement
	@test -n "$(name)" -a -n "$(project)" || (echo 'usage: make keygen name="..." project=...' && exit 1)
	cd $(BACKEND) && go run ./cmd/keygen -name="$(name)" -project="$(project)"

.PHONY: admin-add
admin-add: ## Создать администратора: make admin-add email=me@example.com name="Имя"
	@test -n "$(email)" -a -n "$(name)" || (echo 'usage: make admin-add email=... name="..."' && exit 1)
	cd $(BACKEND) && go run ./cmd/adminadd -email="$(email)" -name="$(name)"

.PHONY: admin-demo
admin-demo: ## Тестовая учётка бета-теста из ADMIN_DEMO_* (только тестовый стенд)
	cd $(BACKEND) && go run ./cmd/adminadd -demo

.PHONY: admin-disable
admin-disable: ## Заблокировать администратора: make admin-disable email=...
	@test -n "$(email)" || (echo 'usage: make admin-disable email=...' && exit 1)
	cd $(BACKEND) && go run ./cmd/adminadd -disable -email="$(email)"

## ---------- линт ----------

.PHONY: fmt
fmt: ## Отформатировать код и импорты
	cd $(BACKEND) && golangci-lint fmt ./...

.PHONY: lint
lint: ## Запустить линтер
	cd $(BACKEND) && golangci-lint run ./...

.PHONY: test
test: ## Запустить тесты
	cd $(BACKEND) && go test ./...

.PHONY: tidy
tidy: ## Привести в порядок go.mod / go.sum
	cd $(BACKEND) && go mod tidy

.PHONY: check
check: lint test ## Линтер + тесты (запускать перед коммитом)

.PHONY: vuln
vuln: ## Проверить уязвимости в зависимостях и Go
	cd $(BACKEND) && govulncheck ./...

.PHONY: test-integration
test-integration: ## Тесты с настоящими Postgres и MinIO (сначала make up)
	cd $(BACKEND) && DOCGEN_INTEGRATION=1 go test ./...
