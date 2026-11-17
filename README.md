# DocGen

Общий сервис генерации документов для порталов компании. Портал присылает DOCX-шаблон с метками `{{ }}` и JSON с данными — сервис проверяет их, подставляет данные и возвращает готовый DOCX или PDF. Выпущенные документы хранятся до конца срока хранения, каждый запрос пишется в журнал; админка показывает журнал и отчёты.

ТЗ: [docs/ТЗ DocGen ред. 2.docx](docs/ТЗ%20DocGen%20ред.%202.docx). Контракт API: [backend/api/openapi.yaml](backend/api/openapi.yaml).

## Быстрый старт

Нужны Go, Node.js, Docker, `golangci-lint`, `sqlc`, `goose`.

```bash
cp .env.example .env          # при необходимости поправить порты
make up                       # Postgres, MinIO, Gotenberg
make migrate-up
make admin-add email=me@example.com name="Имя"   # пароль печатается один раз
make admin-install && make admin-build           # админка в admin/dist
make run                      # http://localhost:8080, админка — /admin/
```

Проект (портал) создаётся в админке («Проекты») или API, затем ему выдаётся ключ:

```bash
make keygen name="Портал закупок" project=procurement   # ключ печатается один раз
make keylist                                            # список ключей
make keyrevoke id=<ID из keylist>                       # отозвать ключ
```

## Как портал выпускает документ

```bash
curl http://localhost:8080/v1/documents \
  -H "X-API-Key: dg_..." -H "Idempotency-Key: lot-209092-opening" \
  -F template=@protocol.docx -F data=@data.json \
  -F format=pdf -F template_name=opening_protocol \
  -F external_id=lot:209092 -F requested_by=user-17 -F retention_days=30 \
  -o protocol.pdf
```

Ответ — сам файл; его ID и срок хранения — в заголовках `X-Document-Id`, `X-Document-Expires-At`. Повтор с тем же `Idempotency-Key` возвращает тот же документ. Проверить шаблон без генерации: `POST /v1/templates/check` (удобно в CI портала).

## Метки в шаблоне

| Что | Пример |
|---|---|
| Значение | `{{ customer.name }}` |
| Форматтер | `{{ total \| money }}`, `{{ total \| moneywords:kk }}`, `{{ date \| longdate:ru }}`, `{{ bin \| bin }}`, `{{ ok \| check }}`, `{{ note \| default:— }}` |
| Цикл | `{{#each lots}}{{@number}}. {{name}}{{else}}нет{{/each}}` |
| Цикл по строкам таблицы | `{{#each lots}}` в первой ячейке, `{{/each}}` в последней; строка с `{{else}}` — для пустого списка |
| Условие | `{{#if security}}…{{else}}…{{/if}}` |
| Поле внешнего цикла | `{{../number}}` |

Цикл или условие на несколько абзацев: каждая метка блока — в отдельном абзаце. Все форматтеры и правила — в ТЗ (п. 3.1–3.2) и в [AGENTS.md](AGENTS.md).

## Разработка

```bash
make check              # линтер + unit-тесты
make test-integration   # + Postgres, MinIO, Gotenberg (нужен make up и база на последней миграции)
make admin-dev          # админка на :5173 с прокси к make run
```

Эталонные тесты (`backend/internal/golden`) выпускают синтетический двуязычный протокол и сравнивают DOCX и PDF с эталонами. После намеренных изменений движка, шрифтов или Gotenberg:

```bash
cd backend && DOCGEN_INTEGRATION=1 go test ./internal/golden -update
```

— посмотреть новые `testdata/*.golden.*` и закоммитить.

Демо-стенд можно открыть без входа в админку: `ADMIN_AUTH_DISABLED=true` (только с тестовыми данными — журнал показывает входные данные документов).
