# DocGen в Postman

Коллекция `DocGen.postman_collection.json` проходит весь сервис: вход в админку и проекты, проверку шаблонов, выпуск обоих протоколов в DOCX и PDF, ошибки данных и шаблона, идемпотентность, чтение документа, журнал, отчёты и выход. У каждого запроса есть проверки (вкладка Test Results).

## Подготовка (один раз)

1. Запустить сервис: `make up`, `make migrate-up`, `make run`.
2. Создать админа и два проекта с ключами (второй нужен для проверки «чужой документ → 404»):

   ```bash
   make admin-add email=me@example.com name="Тестировщик"   # пароль печатается один раз
   ```
   В админке (`http://localhost:8080/admin/`, «Проекты») создать проекты `test_a` и `test_b`, затем:
   ```bash
   make keygen name="Портал A" project=test_a
   make keygen name="Портал B" project=test_b
   ```
3. Postman → **Import** → оба файла из этой папки: коллекцию и окружение `DocGen.local.postman_environment.json`.
4. В окружении **DocGen local** заполнить `apiKey` (ключ test_a), `apiKeyOther` (ключ test_b), `adminEmail`, `adminPassword` и выбрать это окружение справа вверху.
5. **Settings → General → Working directory** = путь к корню репозитория `doc-gen`. Отсюда берутся файлы шаблонов и данных (`examples/opening/template.docx` и т. п.). Без этого запросы с файлами уйдут пустыми, а Postman покажет у поля жёлтый значок.

## Запуск

- **Всё сразу:** правой кнопкой по коллекции → **Run collection** → Run. Папки идут по порядку: «Вход» сохраняет токен админа, выпуск документа — его ID для папки «Чтение документа».
- **По одному запросу:** сначала «1. Админка → Вход» (для запросов админки), дальше любой.
- **Посмотреть документ:** у запросов выпуска нажать стрелку у Send → **Send and Download**, файл сохранится на диск. PDF виден и прямо во вкладке Body.
- **Ошибки данных** (`data-invalid.json → 400`): список полей — в ответе, в `error.details`.

Что должно быть в каждом документе — [examples/README.md](../README.md), раздел «Что смотреть».

## Свои шаблоны и данные

Скопировать запрос `3. Выпуск → data.json → PDF` и в Body поменять файлы у полей `template` и `data` (Select Files). `template_name` и `external_id` — любые строки, они попадут в журнал.

## Без Postman (то же из консоли)

```bash
npx newman run examples/postman/DocGen.postman_collection.json \
  -e examples/postman/DocGen.local.postman_environment.json --working-dir .
```

В окружении должны быть заполнены ключи и пароль. Не коммитьте окружение с реальными значениями.

## Через OpenAPI

Спецификация API — `backend/api/openapi.yaml`. Её можно открыть в Postman (Import → файл) или в любом просмотрщике OpenAPI как справочник: там все поля, коды ответов и ошибки. Готовые запросы с файлами и проверками есть только в этой коллекции.

## Пересборка коллекции

Коллекция генерируется скриптом: правки удобнее вносить в `build_collection.py`, затем

```bash
python3 examples/postman/build_collection.py
```
