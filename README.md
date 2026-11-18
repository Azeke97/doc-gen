# DocGen

Общий сервис генерации документов для порталов компании. Портал присылает DOCX-шаблон с метками `{{ }}` и JSON с данными — сервис проверяет их, подставляет данные и возвращает готовый DOCX или PDF. Выпущенные документы хранятся до конца срока хранения, каждый запрос пишется в журнал; админка показывает журнал и отчёты.

ТЗ: [docs/ТЗ DocGen ред. 2.docx](docs/ТЗ%20DocGen%20ред.%202.docx). Контракт API: [backend/api/openapi.yaml](backend/api/openapi.yaml).

## Запуск

Нужны Go, Node.js и Docker.

```bash
make dev                                         # всё сразу: .env, контейнеры, миграции, админка, сервис
make key project=zakupki name="Портал закупок"   # ключ порталу (печатается один раз), проект создаётся сам
```

Сервис — `http://localhost:8080`, админка — `http://localhost:8080/admin/` (локально без входа). Остановить контейнеры — `make down`, остальные команды — `make help`.

## Проверить в Postman

Импортировать [examples/DocGen.postman_collection.json](examples/DocGen.postman_collection.json), во вкладке **Variables** коллекции указать `apiKey` и выполнять запросы 1–7 по порядку. Шаблоны и данные для них — в [examples/](examples): `opening_protocol` и `results_protocol` (`.docx` + `.json`), `opening_protocol_errors.json` — данные с ошибками. Если Postman не нашёл шаблон, выбрать файл в поле `template` (Body → form-data) или указать в Settings → General → Working directory папку `examples`.

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
make test-integration   # + Postgres, MinIO, Gotenberg (нужен запущенный make dev или make up)
make admin-dev          # админка на :5173 с горячей перезагрузкой (API — make dev)
```

Эталонные тесты (`backend/internal/golden`) выпускают синтетический двуязычный протокол и сравнивают DOCX и PDF с эталонами. После намеренных изменений движка, шрифтов или Gotenberg:

```bash
cd backend && DOCGEN_INTEGRATION=1 go test ./internal/golden -update
```

— посмотреть новые `testdata/*.golden.*` и закоммитить.

## Боевой стенд

Локально и на тестовом стенде админка открыта без входа (`ADMIN_AUTH_DISABLED=true` в `.env.example`). На боевом — `ADMIN_AUTH_DISABLED=false` и учётки через `make admin-add email=... name="..."`: журнал показывает входные данные документов.
