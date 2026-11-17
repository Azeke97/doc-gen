#!/usr/bin/env python3
"""Собирает Postman-коллекцию DocGen (examples/postman/DocGen.postman_collection.json).

Коллекцию удобнее править здесь, чем в JSON: запросы и проверки описаны короче.
Запуск из корня репозитория: python3 examples/postman/build_collection.py
"""

import json
import pathlib

HERE = pathlib.Path(__file__).resolve().parent

API_KEY = [{"key": "X-API-Key", "value": "{{apiKey}}"}]
ADMIN = [{"key": "Authorization", "value": "Bearer {{adminToken}}"}]


def tests(*lines: str) -> list:
    return [{"listen": "test", "script": {"type": "text/javascript", "exec": list(lines)}}]


def prerequest(*lines: str) -> dict:
    return {"listen": "prerequest", "script": {"type": "text/javascript", "exec": list(lines)}}


def status(code: int) -> str:
    return f"pm.test('HTTP {code}', () => pm.response.to.have.status({code}));"


def error(code: int, err: str) -> list:
    return [status(code), f"pm.test('error.code = {err}', () => pm.expect(pm.response.json().error.code).to.eql('{err}'));"]


def url(path: str, query: list | None = None) -> dict:
    u = {"raw": "{{baseUrl}}" + path, "host": ["{{baseUrl}}"], "path": [p for p in path.split("?")[0].split("/") if p]}
    if query:
        u["query"] = query
        u["raw"] += "?" + "&".join(f"{q['key']}={q['value']}" for q in query if not q.get("disabled"))
    return u


def req(name, method, path, headers=None, body=None, events=None, query=None, description=""):
    r = {"name": name, "request": {"method": method, "header": headers or [], "url": url(path, query)}}
    if description:
        r["request"]["description"] = description
    if body:
        r["request"]["body"] = body
    if events:
        r["event"] = events
    return r


def json_body(obj) -> dict:
    return {"mode": "raw", "raw": json.dumps(obj, ensure_ascii=False, indent=2), "options": {"raw": {"language": "json"}}}


def form(template: str, data: str | None = None, fmt: str = "pdf", **extra) -> dict:
    fields = [{"key": "template", "type": "file", "src": template}]
    if data:
        fields.append({"key": "data", "type": "file", "src": data})
        fields += [
            {"key": "format", "value": fmt, "type": "text"},
            {"key": "template_name", "value": extra.pop("template_name", "opening_protocol"), "type": "text"},
            {"key": "external_id", "value": extra.pop("external_id", "tender:100778-3"), "type": "text"},
            {"key": "requested_by", "value": "postman", "type": "text"},
            {"key": "retention_days", "value": extra.pop("retention_days", "30"), "type": "text"},
        ]
    for k, v in extra.items():
        fields.append({"key": k, "value": v, "type": "text"})
    return {"mode": "formdata", "formdata": fields}


def generate(name, template, data, fmt, template_name, expect=201, err=None, description=""):
    """Выпуск документа: новый Idempotency-Key на каждый запуск."""
    headers = API_KEY + [{"key": "Idempotency-Key", "value": "{{$guid}}"}]
    if expect == 201:
        ctype = "application/pdf" if fmt == "pdf" else "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
        ev = tests(
            status(201),
            f"pm.test('Content-Type {fmt}', () => pm.expect(pm.response.headers.get('Content-Type')).to.include('{ctype}'));",
            "pm.test('заголовки документа', () => {",
            "  ['X-Document-Id', 'X-Document-SHA256', 'X-Document-Expires-At', 'Location'].forEach(h => pm.expect(pm.response.headers.get(h), h).to.be.ok);",
            "});",
            "pm.collectionVariables.set('documentId', pm.response.headers.get('X-Document-Id'));",
        )
    else:
        ev = tests(*error(expect, err), "console.log(JSON.stringify(pm.response.json().error.details, null, 2));")
    return req(name, "POST", "/v1/documents", headers, form(template, data, fmt, template_name=template_name), ev, description=description)


OPENING = "examples/opening/template.docx"
RESULTS = "examples/results/template.docx"
BROKEN = "examples/broken/unclosed_each.docx"


def folder(name, items, description=""):
    f = {"name": name, "item": items}
    if description:
        f["description"] = description
    return f


login = req(
    "Вход", "POST", "/admin/v1/login",
    [{"key": "Content-Type", "value": "application/json"}],
    json_body({"email": "{{adminEmail}}", "password": "{{adminPassword}}"}),
    tests(status(200), "pm.collectionVariables.set('adminToken', pm.response.json().token);"),
    description="Сохраняет токен в переменную adminToken; остальные запросы админки берут его оттуда.",
)

collection = {
    "info": {
        "name": "DocGen",
        "description": (
            "Проверка DocGen через Postman. Перед запуском:\n"
            "1. Settings → General → Working directory = корень репозитория doc-gen (файлы шаблонов и данных берутся оттуда).\n"
            "2. Импортировать окружение DocGen.local.postman_environment.json и заполнить apiKey, apiKeyOther, adminEmail, adminPassword.\n"
            "3. Запускать папки по порядку (Run collection) или по одному запросу. PDF и DOCX: Send and Download или просмотр во вкладке Body.\n"
            "Подробно — examples/postman/README.md."
        ),
        "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
    },
    "variable": [
        {"key": "adminToken", "value": ""},
        {"key": "documentId", "value": ""},
        {"key": "fixedKey", "value": ""},
        {"key": "journalId", "value": ""},
    ],
    "item": [
        folder("0. Сервис", [
            req("Живость", "GET", "/healthz", events=tests(status(200))),
        ]),
        folder("1. Админка: вход и проекты", [
            req("Нужен ли вход", "GET", "/admin/v1/login-info", events=tests(status(200))),
            login,
            req("Кто я", "GET", "/admin/v1/me", ADMIN, events=tests(status(200), "pm.test('email', () => pm.expect(pm.response.json().email).to.eql(pm.environment.get('adminEmail')));")),
            req("Проекты", "GET", "/admin/v1/projects", ADMIN, events=tests(status(200))),
            req("Создать проект", "POST", "/admin/v1/projects", ADMIN + [{"key": "Content-Type", "value": "application/json"}],
                json_body({"code": "pm_{{$timestamp}}", "name": "Проект из Postman", "retention_days": 90}),
                tests(status(201), "pm.collectionVariables.set('newProject', pm.response.json().code);"),
                description="Код проекта: латиница, цифры и _. Ключ для нового проекта выдаётся командой make keygen."),
            req("Изменить срок хранения", "PATCH", "/admin/v1/projects/{{newProject}}", ADMIN + [{"key": "Content-Type", "value": "application/json"}],
                json_body({"retention_days": 30}),
                tests(status(200), "pm.test('30 дней', () => pm.expect(pm.response.json().retention_days).to.eql(30));")),
            req("Проект с таким кодом уже есть", "POST", "/admin/v1/projects", ADMIN + [{"key": "Content-Type", "value": "application/json"}],
                json_body({"code": "{{newProject}}", "name": "Повтор"}), tests(*error(409, "project_exists"))),
        ], "Проект — это портал: у него свои API-ключи, журнал и максимальный срок хранения документов."),
        folder("2. Портал: ключ и проверка шаблонов", [
            req("Чей ключ", "GET", "/v1/whoami", API_KEY, events=tests(status(200), "console.log(pm.response.json());")),
            req("Без ключа", "GET", "/v1/whoami", events=tests(*error(401, "unauthorized"))),
            req("Проверить шаблон: протокол вскрытия", "POST", "/v1/templates/check", API_KEY, form(OPENING),
                tests(status(200), "pm.test('valid', () => pm.expect(pm.response.json().valid).to.eql(true));",
                      "pm.test('есть поля', () => pm.expect(pm.response.json().variables.length).to.be.above(10));")),
            req("Проверить шаблон: протокол итогов", "POST", "/v1/templates/check", API_KEY, form(RESULTS),
                tests(status(200), "pm.test('valid', () => pm.expect(pm.response.json().valid).to.eql(true));")),
            req("Проверить шаблон с ошибкой", "POST", "/v1/templates/check", API_KEY, form(BROKEN),
                tests(*error(422, "invalid_template"),
                      "pm.test('указана метка', () => pm.expect(pm.response.json().error.details[0].message).to.include('не закрыт'));")),
            req("Проверить не-DOCX", "POST", "/v1/templates/check", API_KEY, form("examples/opening/data.json"),
                tests(*error(422, "invalid_template"))),
        ]),
        folder("3. Выпуск: протокол вскрытия", [
            generate("data.json → PDF", OPENING, "examples/opening/data.json", "pdf", "opening_protocol"),
            generate("data.json → DOCX", OPENING, "examples/opening/data.json", "docx", "opening_protocol"),
            generate("data-edge.json → PDF", OPENING, "examples/opening/data-edge.json", "pdf", "opening_protocol"),
            generate("data-edge.json → DOCX", OPENING, "examples/opening/data-edge.json", "docx", "opening_protocol"),
            generate("data-invalid.json → 400", OPENING, "examples/opening/data-invalid.json", "pdf", "opening_protocol", 400, "invalid_data",
                     "Ожидается 7 ошибок в error.details; они же — в Console."),
        ], "Что смотреть в файлах — examples/README.md."),
        folder("4. Выпуск: протокол об итогах", [
            generate("data.json → PDF (несостоявшийся)", RESULTS, "examples/results/data.json", "pdf", "results_protocol"),
            generate("data.json → DOCX", RESULTS, "examples/results/data.json", "docx", "results_protocol"),
            generate("data-winner.json → PDF (победитель)", RESULTS, "examples/results/data-winner.json", "pdf", "results_protocol"),
            generate("data-winner.json → DOCX", RESULTS, "examples/results/data-winner.json", "docx", "results_protocol"),
            generate("data-invalid.json → 400", RESULTS, "examples/results/data-invalid.json", "pdf", "results_protocol", 400, "invalid_data"),
            generate("Шаблон с ошибкой → 422", BROKEN, "examples/opening/data.json", "pdf", "broken", 422, "invalid_template"),
        ]),
        folder("5. Идемпотентность", [
            {**req("Первый запрос → 201", "POST", "/v1/documents", API_KEY + [{"key": "Idempotency-Key", "value": "{{fixedKey}}"}],
                   form(OPENING, "examples/opening/data.json", "pdf"),
                   [prerequest("pm.collectionVariables.set('fixedKey', 'postman-' + Date.now());"),
                    *tests(status(201), "pm.collectionVariables.set('documentId', pm.response.headers.get('X-Document-Id'));")])},
            req("Тот же ключ и запрос → 200, тот же документ", "POST", "/v1/documents", API_KEY + [{"key": "Idempotency-Key", "value": "{{fixedKey}}"}],
                form(OPENING, "examples/opening/data.json", "pdf"),
                tests(status(200), "pm.test('тот же X-Document-Id', () => pm.expect(pm.response.headers.get('X-Document-Id')).to.eql(pm.collectionVariables.get('documentId')));")),
            req("Тот же ключ, другой external_id → 409", "POST", "/v1/documents", API_KEY + [{"key": "Idempotency-Key", "value": "{{fixedKey}}"}],
                form(OPENING, "examples/opening/data.json", "pdf", external_id="tender:other"), tests(*error(409, "idempotency_conflict"))),
        ]),
        folder("6. Чтение документа", [
            req("Метаданные", "GET", "/v1/documents/{{documentId}}", API_KEY, events=tests(status(200), "console.log(pm.response.json());")),
            req("Файл", "GET", "/v1/documents/{{documentId}}/file", API_KEY,
                events=tests(status(200), "pm.test('attachment', () => pm.expect(pm.response.headers.get('Content-Disposition')).to.include('attachment'));")),
            {**req("Чужой проект → 404", "GET", "/v1/documents/{{documentId}}", [{"key": "X-API-Key", "value": "{{apiKeyOther}}"}],
                   events=tests(*error(404, "document_not_found"))),
             "description": "apiKeyOther — ключ другого проекта."},
            req("Неверный ID → 400", "GET", "/v1/documents/not-a-uuid", API_KEY, events=tests(*error(400, "invalid_id"))),
        ]),
        folder("7. Ошибки запроса", [
            req("Без Idempotency-Key", "POST", "/v1/documents", API_KEY, form(OPENING, "examples/opening/data.json"), tests(*error(400, "invalid_idempotency_key"))),
            req("format=xlsx", "POST", "/v1/documents", API_KEY + [{"key": "Idempotency-Key", "value": "{{$guid}}"}],
                form(OPENING, "examples/opening/data.json", "xlsx"), tests(*error(400, "invalid_format"))),
            req("Срок больше, чем у проекта", "POST", "/v1/documents", API_KEY + [{"key": "Idempotency-Key", "value": "{{$guid}}"}],
                form(OPENING, "examples/opening/data.json", retention_days="100000"), tests(*error(400, "invalid_retention_days"))),
            req("Лишнее поле формы", "POST", "/v1/documents", API_KEY + [{"key": "Idempotency-Key", "value": "{{$guid}}"}],
                form(OPENING, "examples/opening/data.json", foo="bar"), tests(*error(400, "invalid_form"))),
        ]),
        folder("8. Журнал и отчёты", [
            req("Журнал", "GET", "/admin/v1/journal", ADMIN, query=[{"key": "limit", "value": "20"}],
                events=tests(status(200), "const j = pm.response.json();",
                             "pm.test('есть записи', () => pm.expect(j.items.length).to.be.above(0));",
                             "pm.collectionVariables.set('journalId', j.items[0].id);",
                             "pm.collectionVariables.set('nextCursor', j.next_cursor || '');")),
            req("Журнал: следующая страница", "GET", "/admin/v1/journal", ADMIN,
                query=[{"key": "limit", "value": "20"}, {"key": "cursor", "value": "{{nextCursor}}"}], events=tests(status(200))),
            req("Журнал: отклонённые и сбои", "GET", "/admin/v1/journal", ADMIN,
                query=[{"key": "status", "value": "rejected"}, {"key": "status", "value": "failed"}, {"key": "template_name", "value": "", "disabled": True},
                       {"key": "from", "value": "", "disabled": True}, {"key": "to", "value": "", "disabled": True}],
                events=tests(status(200), "pm.test('только rejected/failed', () => pm.response.json().items.forEach(i => pm.expect(['rejected','failed']).to.include(i.status)));")),
            req("Запись журнала", "GET", "/admin/v1/journal/{{journalId}}", ADMIN, events=tests(status(200), "console.log(pm.response.json().document_state);")),
            req("Отчёт за 30 дней", "GET", "/admin/v1/reports", ADMIN, events=tests(status(200), "console.log(pm.response.json().totals);")),
        ]),
        folder("9. Выход", [
            req("Выйти", "POST", "/admin/v1/logout", ADMIN, events=tests(status(204))),
            req("Старый токен → 401", "GET", "/admin/v1/me", ADMIN, events=tests(*error(401, "unauthorized"))),
        ]),
    ],
}

environment = {
    "name": "DocGen local",
    "values": [
        {"key": "baseUrl", "value": "http://localhost:8080", "type": "default", "enabled": True},
        {"key": "apiKey", "value": "", "type": "secret", "enabled": True},
        {"key": "apiKeyOther", "value": "", "type": "secret", "enabled": True},
        {"key": "adminEmail", "value": "", "type": "default", "enabled": True},
        {"key": "adminPassword", "value": "", "type": "secret", "enabled": True},
    ],
    "_postman_variable_scope": "environment",
}

(HERE / "DocGen.postman_collection.json").write_text(json.dumps(collection, ensure_ascii=False, indent=2) + "\n")
(HERE / "DocGen.local.postman_environment.json").write_text(json.dumps(environment, ensure_ascii=False, indent=2) + "\n")
print("ok")
