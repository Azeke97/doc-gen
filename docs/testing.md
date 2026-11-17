# Как тестировать DocGen

Функциональная проверка шаблонов и выпуска DOCX/PDF на протоколах тендера — в [examples/README.md](../examples/README.md) (`examples/try.sh`). Ниже — проверка сервиса целиком.

Три уровня, от быстрого к полному:

| Уровень | Команда | Что проверяет | Нужна инфраструктура |
|---|---|---|---|
| Unit | `make check` | линтер, движок DOCX, форматтеры, обработчики HTTP на заглушках, DOCX-эталон | нет |
| Интеграционные | `make test-integration` | SQL-запросы, сервис документов с настоящими Postgres/MinIO, PDF через Gotenberg, PDF-эталон | `make up` + база на последней миграции |
| Ручной сценарий | раздел ниже | сервис целиком глазами портала и админа | `make up` + `make run` |

Unit и интеграционные тесты запускаются перед каждым коммитом. Ручной сценарий — перед показом, после изменений API или когда нужно увидеть журнал и отчёты.

## Подготовка

```bash
cp .env.example .env     # если .env ещё нет; проверить ADMIN_UI_DIR=../admin/dist
make up
make migrate-up
make admin-install && make admin-build
make run                 # в отдельном терминале
```

Админ и проекты (один раз):

```bash
make admin-add email=tester@example.com name="Тестировщик"
```

Войти в `http://localhost:8080/admin/`, на экране «Проекты» создать `test_a` (срок хранения 30 дней) и `test_b`. Код проекта — латиница, цифры и `_`, дефис не допускается. Выдать ключи:

```bash
make keygen name="Портал A" project=test_a
make keygen name="Портал B" project=test_b
```

Переменные для команд ниже (из корня репозитория):

```bash
export API=http://localhost:8080/v1
export KEY_A=dg_...   # ключ портала A
export KEY_B=dg_...   # ключ портала B
export T=backend/internal/golden/testdata/opening_protocol.docx
export D=backend/internal/golden/testdata/opening_protocol.json
mkdir -p /tmp/dg
```

`opening_protocol.docx` — синтетический двуязычный протокол: в нём есть циклы по абзацам и строкам таблицы, условия, `{{else}}`, колонтитулы и все основные форматтеры, данные вымышленные.

Вспомогательная функция для выпуска документа (повторяет обязательные поля):

```bash
gen() {  # gen <ключ> <idempotency-key> <format> [доп. -F ...]
  local key=$1 idem=$2 fmt=$3; shift 3
  curl -s -D /tmp/dg/headers -o /tmp/dg/out.$fmt -w '%{http_code}\n' "$API/documents" \
    -H "X-API-Key: $key" -H "Idempotency-Key: $idem" \
    -F template=@$T -F data=@$D -F format=$fmt \
    -F template_name=opening_protocol -F external_id=lot:42 \
    -F requested_by=user-17 -F retention_days=30 "$@"
}
```

## Сценарий

Для каждого шага: команда → ожидаемый результат. Отметить ✅/❌, при ❌ записать ответ сервера и `request_id` из журнала.

### А. Доступ

| № | Действие | Ожидается |
|---|---|---|
| А1 | `curl -s localhost:8080/healthz` | `200` |
| А2 | `curl -s $API/whoami -H "X-API-Key: $KEY_A"` | JSON с проектом `test_a` |
| А3 | `curl -s $API/whoami` (без ключа) | `401`, `{"error":{"code":"unauthorized",…}}` |
| А4 | `curl -s $API/whoami -H "X-API-Key: dg_wrong"` | `401 unauthorized` |

### Б. Проверка шаблона

| № | Действие | Ожидается |
|---|---|---|
| Б1 | `curl -s $API/templates/check -H "X-API-Key: $KEY_A" -F template=@$T` | `200`, `valid: true`; в `variables` элементы вида `{"path": "lots[].amount", "usage": ["value"], "filters": ["money"]}`, есть `customer.name_ru`, `applicants` с usage `each` |
| Б2 | Текстовый файл, переименованный в `.docx`: `echo hi > /tmp/dg/bad.docx`, затем check с ним | `422 invalid_template`, в `details` — описание проблемы на русском |
| Б3 | Шаблон из Word с ошибкой: в копии протокола удалить `{{/each}}` (или написать `{{ total \| nosuch }}`), сохранить, check | `422 invalid_template`, `details` указывает метку и место |
| Б4 | Шаблон, сохранённый в Word как «Документ с поддержкой макросов» (`.docm`) | `422 invalid_template`, «это документ с макросами (.docm): нужен .docx» |

### В. Выпуск документа

| № | Действие | Ожидается |
|---|---|---|
| В1 | `gen $KEY_A test-1 pdf` | `201`; в `/tmp/dg/headers` есть `X-Document-Id`, `X-Document-SHA256`, `X-Document-Expires-At` (≈ +30 дней), `Location` |
| В2 | Открыть `/tmp/dg/out.pdf` | Одна страница, протокол на казахском и русском: таблица лотов (`1 500`, `6 000 000.00`, `37 500.50`), итог `6 037 500.50 ₸` и прописью на двух языках, БИН `000740001307` у обоих участников (в данных он то число, то строка), время `19.10.2026 17:40:00`, `☒/☐` у документов первого участника; у второго документов нет, а вместо суммы обеспечения — «Обеспечение не внесено» (ветка `{{else}}`); в конце «Ескертпе жоқ / Примечаний нет» (`default`) |
| В3 | `sha256sum /tmp/dg/out.pdf` | Совпадает с `X-Document-SHA256` |
| В4 | `gen $KEY_A test-2 docx`, открыть в Word/LibreOffice | `201`, файл открывается без предупреждений, оформление шаблона сохранено |
| В5 | `gen $KEY_A test-3 docx` — дважды с разными ключами, сравнить `sha256sum` | Одинаковые хэши (детерминированный вывод) |

### Г. Идемпотентность

| № | Действие | Ожидается |
|---|---|---|
| Г1 | Повторить `gen $KEY_A test-1 pdf` | `200` (не 201), тот же `X-Document-Id` |
| Г2 | Тот же ключ `test-1`, но другой `external_id`: в функции `gen` заменить `lot:42` на `lot:43`, выполнить `gen $KEY_A test-1 pdf`, вернуть `lot:42` | `409 idempotency_conflict` |
| Г3 | `gen $KEY_B test-1 pdf` | `201`, новый документ (ключ идемпотентности свой у каждого портала) |

### Д. Ошибки запроса

| № | Действие | Ожидается |
|---|---|---|
| Д1 | Запрос без заголовка `Idempotency-Key` | `400 invalid_idempotency_key` |
| Д2 | `format=xlsx` | `400 invalid_format` |
| Д3 | Без `requested_by` / `template_name` / `external_id` | `400 invalid_requested_by` / `invalid_template_name` / `invalid_external_id` |
| Д4 | `retention_days=31` для `test_a` (максимум 30) | `400 invalid_retention_days` |
| Д5 | `retention_days=abc` | `400 invalid_retention_days` |
| Д6 | `data='[1,2]'` (не объект) | `400 invalid_data` |
| Д7 | Данные без поля: `jq 'del(.customer.bin) \| del(.total)' $D > /tmp/dg/d.json`, выпуск с ним | `400 invalid_data`, в `details` оба поля, сообщения на русском |
| Д8 | `chair_note: null` в данных (как в эталоне) | Документ выпускается — `null` допустим |
| Д9 | Неизвестное поле формы: `-F foo=bar` | `400 invalid_form` |
| Д10 | Шаблон больше 10 МиБ | `413 request_too_large` |

### Е. Чтение документов

`ID` — значение `X-Document-Id` из В1.

| № | Действие | Ожидается |
|---|---|---|
| Е1 | `curl -s $API/documents/$ID -H "X-API-Key: $KEY_A"` | `200`, метаданные: формат, `template_name`, `external_id`, `expires_at` |
| Е2 | `curl -s -D - -o /tmp/dg/f.pdf $API/documents/$ID/file -H "X-API-Key: $KEY_A"` | `200`, `Content-Disposition: attachment`, `X-Content-Type-Options: nosniff`, `Cache-Control: no-store`; файл совпадает с В1 |
| Е3 | То же с `$KEY_B` | `404` (чужой проект) |
| Е4 | `$API/documents/00000000-0000-0000-0000-000000000000` | `404` |
| Е5 | `$API/documents/not-a-uuid` | `400` |

`410` для истёкшего документа вручную не проверить (строки `documents` неизменяемы, срок нельзя сдвинуть) — это покрыто интеграционными тестами `internal/documents`.

### Ж. Отказ конвертера и отзыв ключа

| № | Действие | Ожидается |
|---|---|---|
| Ж1 | `docker compose stop gotenberg`, затем `gen $KEY_A test-4 pdf` | `503 converter_unavailable` (не дольше `GOTENBERG_TIMEOUT`) |
| Ж2 | При остановленном Gotenberg `gen $KEY_A test-5 docx` | `201` — DOCX конвертер не нужен |
| Ж3 | `docker compose start gotenberg`, повторить Ж1 с тем же `test-4` | `201` |
| Ж4 | `make keylist project=test_b`, `make keyrevoke id=<ID>`, затем `whoami` с `$KEY_B` | `401` сразу |

### З. Админка (`http://localhost:8080/admin/`)

| № | Действие | Ожидается |
|---|---|---|
| З1 | Вход с неверным паролем | Сообщение об ошибке, вход не выполнен |
| З2 | Вход с верным | Открывается журнал |
| З3 | Журнал | Есть все запросы из сценария, включая отклонённые: `created`, `repeated`, `rejected` (4xx), `failed` (503 из Ж1) |
| З4 | Фильтры: портал `test_a`, шаблон `opening_protocol`, external ID `lot:42`, статус `rejected`, диапазон дат | Список сужается; фильтры попадают в URL, обновление страницы их сохраняет |
| З4б | Не перезагружая страницу журнала, сменить фильтры в адресной строке или нажать «Назад» после такого перехода | Фильтры и список соответствуют адресу |
| З5 | «Показать ещё» (если записей больше страницы) | Догружается следующая страница без повторов |
| З6 | Карточка записи из В1 | Метаданные, состояние документа «доступен», входные данные JSON |
| З7 | Карточка записи из Д7 | Ошибка с перечнем полей; входных данных нет (документ не выпущен) |
| З8 | Карточка записи из Б3-подобной ошибки шаблона при выпуске | Ошибка с перечнем меток |
| З9 | Отчёты | Итоги по статусам, среднее время генерации, разбивка по порталам, по дням, по шаблонам; период по умолчанию — 30 дней |
| З10 | Проекты: изменить срок `test_a` на 10 дней, выпустить с `retention_days=30` | `400 invalid_retention_days`; старые документы сохраняют прежний срок |
| З11 | Выход | Возврат на страницу входа; старый токен даёт `401` |

### И. Очистка по сроку (по желанию)

Проверяется в интеграционных тестах. Вручную: поставить `DOCUMENTS_CLEANUP_INTERVAL=1m`, перезапустить `make run`, в логах раз в минуту видна работа очистки без ошибок.

## Последний прогон

17.11.2026, временная база. Пройдено всё. З4б (журнал не следовал за адресом) и английское сообщение после «Выйти» исправлены и перепроверены.

## После сценария

- Все ❌ — с командой, ответом сервера и `request_id`.
- Тестовые проекты и ключи остаются в dev-базе; ключи можно отозвать `make keyrevoke`.
- Если менялись движок, шрифты или Gotenberg — обновить эталоны: `cd backend && DOCGEN_INTEGRATION=1 go test ./internal/golden -update`, просмотреть, закоммитить.
