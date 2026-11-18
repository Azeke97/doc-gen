# examples

Готовые шаблоны и Postman-коллекция, чтобы проверить DocGen и понять, как к нему подключаться.

| Файл | Что это |
|---|---|
| `opening_protocol.docx` + `.json` | протокол вскрытия заявок: шаблон и данные |
| `results_protocol.docx` + `.json` | протокол об итогах: шаблон и данные |
| `opening_protocol_errors.json` | данные с ошибками к протоколу вскрытия → ответ 400 со списком полей |
| `DocGen.postman_collection.json` | 7 запросов портала: ключ, проверка шаблона, PDF, DOCX, ошибки, метаданные, скачивание |

## Запуск и ключ

Из корня репозитория:

```bash
make dev                         # поднимает всё и запускает сервис на http://localhost:8080
make key project=zakupki         # в другом терминале: ключ портала, печатается один раз
```

## Postman

1. **Import** → `DocGen.postman_collection.json`.
2. Коллекция → вкладка **Variables** → `apiKey` = ключ из `make key` → **Save**.
3. **Settings → General → Working directory** → эта папка `examples`.
4. Запросы 1 → 7 по порядку. Файл ответа сохраняется через ▾ у **Send** → **Send and Download**.

Протокол об итогах: в запросах 3–4 заменить `template` на `results_protocol.docx` и `data` — на содержимое `results_protocol.json`.
