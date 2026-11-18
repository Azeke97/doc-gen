# Переменные из .env (direnv в make не работает).
# "-" — не падать, если .env нет; export — передать переменные во все команды.
-include .env
export

BACKEND := backend

.DEFAULT_GOAL := help

.PHONY: help
help: ## Показать список команд
	@awk 'BEGIN {FS = ":.*?## "} /^##@/ {printf "\n\033[1m%s\033[0m\n", substr($$0, 5)} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

##@ Основное

.PHONY: dev
dev: ## Запустить всё: .env, контейнеры, миграции, админка, сервис на :8080
	@test -f .env || (cp .env.example .env && echo "Создан .env из .env.example")
	@$(MAKE) --no-print-directory _dev

# Отдельная цель: make перечитает только что созданный .env.
.PHONY: _dev
_dev: admin/dist/index.html
	docker compose up -d --wait
	cd $(BACKEND) && go run ./cmd/migrate up
	cd $(BACKEND) && go run ./cmd/docgen

.PHONY: key
key: ## Выдать ключ порталу (проект создастся): make key project=zakupki [name="Портал закупок"]
	@test -n "$(project)" || (echo 'usage: make key project=zakupki [name="Портал закупок"]' && exit 1)
	cd $(BACKEND) && go run ./cmd/keygen -project="$(project)" -name="$(name)"

.PHONY: check
check: lint test ## Линтер + тесты (перед коммитом)

.PHONY: down
down: ## Остановить контейнеры (данные сохраняются)
	docker compose down

##@ Для разработки сервиса

.PHONY: up
up: ## Только поднять Postgres, MinIO, Gotenberg
	docker compose up -d --wait

.PHONY: run
run: ## Только запустить сервис
	cd $(BACKEND) && go run ./cmd/docgen

.PHONY: ps
ps: ## Статус контейнеров
	docker compose ps

.PHONY: logs
logs: ## Логи контейнеров (make logs s=postgres — одного сервиса)
	docker compose logs -f $(s)

.PHONY: build
build: ## Собрать бинарники в backend/bin
	cd $(BACKEND) && go build -o bin/docgen ./cmd/docgen && go build -o bin/migrate ./cmd/migrate

.PHONY: test-integration
test-integration: ## Тесты с настоящими Postgres, MinIO, Gotenberg (нужен make up)
	cd $(BACKEND) && DOCGEN_INTEGRATION=1 go test ./...

.PHONY: keylist
keylist: ## Список API-ключей: make keylist [project=zakupki]
	cd $(BACKEND) && go run ./cmd/keygen -list -project="$(project)"

.PHONY: keyrevoke
keyrevoke: ## Отозвать API-ключ: make keyrevoke id=<ID из make keylist>
	@test -n "$(id)" || (echo 'usage: make keyrevoke id=...' && exit 1)
	cd $(BACKEND) && go run ./cmd/keygen -revoke="$(id)"

.PHONY: admin-add
admin-add: ## Создать администратора (нужно, только если ADMIN_AUTH_DISABLED=false)
	@test -n "$(email)" -a -n "$(name)" || (echo 'usage: make admin-add email=... name="..."' && exit 1)
	cd $(BACKEND) && go run ./cmd/adminadd -email="$(email)" -name="$(name)"

.PHONY: admin-disable
admin-disable: ## Заблокировать администратора: make admin-disable email=...
	@test -n "$(email)" || (echo 'usage: make admin-disable email=...' && exit 1)
	cd $(BACKEND) && go run ./cmd/adminadd -disable -email="$(email)"

ADMIN_SRC := $(shell find admin/src admin/index.html admin/package.json admin/vite.config.ts -type f 2>/dev/null)

# Админка пересобирается, только если менялись её исходники.
admin/dist/index.html: $(ADMIN_SRC)
	cd admin && (test -d node_modules || npm ci) && npm run build

.PHONY: admin-build
admin-build: ## Собрать админку в admin/dist
	cd admin && (test -d node_modules || npm ci) && npm run build

.PHONY: admin-dev
admin-dev: ## Админка с горячей перезагрузкой на :5173 (API — запущенный make dev)
	cd admin && (test -d node_modules || npm ci) && npm run dev

.PHONY: migrate-up
migrate-up: ## Применить новые миграции
	cd $(BACKEND) && go run ./cmd/migrate up

.PHONY: migrate-down
migrate-down: ## Откатить последнюю миграцию
	cd $(BACKEND) && go run ./cmd/migrate down

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

.PHONY: fmt
fmt: ## Отформатировать код и импорты
	cd $(BACKEND) && golangci-lint fmt ./...

.PHONY: lint
lint: ## Линтер
	cd $(BACKEND) && golangci-lint run ./...

.PHONY: test
test: ## Unit-тесты
	cd $(BACKEND) && go test ./...

.PHONY: tidy
tidy: ## Привести в порядок go.mod / go.sum
	cd $(BACKEND) && go mod tidy

.PHONY: vuln
vuln: ## Проверить уязвимости в зависимостях и Go
	cd $(BACKEND) && govulncheck ./...
