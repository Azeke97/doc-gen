# DocGen — agent context

DocGen is a standalone service shared by the company's portals: a portal sends a DOCX template with `{{ }}` marks and JSON data, the service checks both, substitutes the data and returns a DOCX or PDF. It stores every issued document for its retention period and logs every request. Portals call it over REST with a per-portal API key; the admin panel is read-only reporting.

Spec: `docs/ТЗ DocGen ред. 2.docx` (redaction 2, approved). The user edits it in `~/Загрузки/ТЗ DocGen ред. 2.docx`; when they say it changed, copy it into `docs/` and commit. Out of scope by the spec: a template registry, HTML templates, template editors, signatures, QR/verification pages, workflow.

The refactor from redaction 1 is in progress: stages 1–7 are done (DOCX engine, LibreOffice conversion, schema, new portal API, removal of the registry/HTML/QR code, admin API and UI for the journal and reports). Still to do: API key commands (list, revoke), DOCX golden tests.

## Stack

- Go (module `docgen` in `backend/`), chi router, pgx + sqlc, goose migrations embedded in the binary, beevik/etree for DOCX XML.
- PostgreSQL 18, MinIO (S3) for files, Gotenberg 8 (LibreOffice) for DOCX → PDF.
- Admin UI: Vue 3 + TypeScript + Vite + Element Plus in `admin/`.
- Local infra: `docker-compose.yml`; config from env (`.env`, loaded by direnv and by the Makefile; see `.env.example`).

## Commands

- `make up` / `make down` — start/stop Postgres, MinIO, Gotenberg.
- `make run`, `make migrate-up`, `make migration name=...`, `make generate` (sqlc).
- `make keygen name=... project=...` — issue a portal API key (printed once).
- `make admin-add email=... name=...` — create an admin (password printed once). `make admin-disable email=...` — block an admin and close their sessions.
- `make admin-install` / `make admin-dev` (Vite on :5173, proxies `/admin/v1` to `make run` on :8080) / `make admin-build` (→ `admin/dist`).
- `make check` — lint + unit tests; `make test-integration` — tests against real Postgres/MinIO/Gotenberg (needs `make up` and the dev DB at the latest migration).

## Layout (`backend/`)

- `cmd/` — `docgen` (server), `migrate`, `keygen`, `adminadd`.
- `migrations/` — goose SQL; `internal/db/queries/*.sql` → sqlc-generated code in `internal/db` (never edit generated files).
- `internal/httpapi` — router, handlers, middleware. Handlers depend on small interfaces declared next to them. `form.go` reads multipart as a stream with a limit per field.
- `internal/docx` — the DOCX engine (see "Templates" below). No DB or HTTP.
- `internal/documents` — `GenerateDOCX`, `CheckTemplate`, `Get`, `File`, retention cleanup (`RunCleanup`, `DeleteExpired`, `DeleteOrphanTemplates`).
- `internal/format` — formatting of JSON values used by the engine's filters: money, numbers, dates, amounts in words, BIN/IIN, checkboxes; exact decimals via `math/big`.
- `internal/gotenberg` — `ConvertDOCX` (LibreOffice route). Errors: `ErrConvert` (Gotenberg rejected the document), `ErrUnavailable` (no connection, timeout, 503/429, no free slot). At most `GOTENBERG_MAX_CONCURRENCY` (default 4) conversions at once; waiting for a slot counts towards `GOTENBERG_TIMEOUT`. LibreOffice turns even garbage into a PDF, so validating the input is `docx.Parse`'s job.
- `internal/storage` (S3), `internal/apikey`, `internal/adminauth`, `internal/config`, `internal/pgerr`.
- `api/openapi.yaml` — API contract; keep it in sync with handlers.

## API

- Portal (`/v1`, header `X-API-Key: dg_...`; the project always comes from the key):
  - `POST /documents` — multipart: `template` (.docx ≤10 MiB), `data` (JSON object ≤5 MiB, a field or a file), and required `format` (docx|pdf), `template_name`, `external_id`, `requested_by` (the portal's user ID, never a name or IIN), `retention_days` (≤ `projects.retention_days`); header `Idempotency-Key` required. The response body is the file with `X-Document-Id`, `X-Document-SHA256`, `X-Document-Expires-At`, `Location`; 201 new, 200 repeat.
  - `POST /templates/check` (multipart `template`) → `{valid, variables}` or `422 invalid_template`.
  - `GET /documents/{id}` (metadata), `GET /documents/{id}/file` (checked against its SHA-256). Another project's document is `404`, an expired one is `410`.
  - `GET /whoami` — which portal the key belongs to.
- Admin (`/admin/v1`, `Authorization: Bearer dgs_...` after `POST /login`; read-only except projects):
  - `GET /login-info` (no session) → `{auth_required}`. With `ADMIN_AUTH_DISABLED=true` (demo stand with test data only — the journal shows document input data) every admin route works without a session as the actor `demo` / «Демо-режим»; the service logs a warning at startup and the UI shows a banner. Default: login required.
  - `POST /logout`, `GET /me`, `GET|POST /projects` (`retention_days`, default 365), `PATCH /projects/{code}` (`name`, `retention_days` — affects documents issued afterwards).
  - `GET /journal` — filters `project`, `template_name`, `external_id`, `status` (repeatable), `from`/`to` (a date is an Astana day, `to` inclusive; or RFC 3339), `limit` ≤200; keyset pages by `(created_at, id)` with an opaque `cursor`/`next_cursor`.
  - `GET /journal/{id}` — the entry plus `document_state`: `available` (input data included), `expired` (data hidden), `deleted` (removed by retention), `null` (no document).
  - `GET /reports?from=&to=&project=` — totals by status, average generation time of new documents, by project, by template (≤200 rows), by Astana day; default period: the last 30 days including today.
- Errors: `{"error": {"code", "message", "details?"}}`. Codes: `invalid_form` (400: not multipart, unknown or repeated field), `invalid_<field>` (400), `invalid_data` (400, `details` = `docx.FieldError`s), `invalid_template` (422, `details` = `docx.Problem`s), `idempotency_conflict` (409), `conversion_failed` (422), `converter_unavailable` (503), `request_too_large` (413), `internal` (500).

## Templates (`internal/docx`)

- `Parse` checks the archive (size, 50 MiB unpacked limit against zip bombs, no macros/.docm/.dotx/.doc), glues tags that Word split across runs and moves every tag into its own run (keeping its formatting), parses tags and builds blocks. Marks are looked for in `word/document.xml`, headers and footers.
- Syntax: `{{ a.b }}`, filters `{{ x | money }}` — `money number date datetime longdate:ru|kk words:ru|kk moneywords:ru|kk iin bin check upper lower capfirst default:text`; `{{#each list}}…{{else}}…{{/each}}`, `{{#if x}}…{{else}}…{{/if}}` (false: `false`, `null`, `""`, `0`, empty list/object), `{{../x}}`, `{{@number}}` (from 1), `{{@index}}`, `{{this}}`.
- A block's mode comes from where its tags are: same paragraph → part of the paragraph; different table cells → table rows from the opening row to the closing row (`{{else}}` in its own row starts the "empty list" rows); otherwise whole paragraphs, and each block tag must then be alone in its paragraph.
- `Template.Render(data)` copies the XML while expanding it — the parsed template is never changed, so it is safe to reuse and render concurrently. Every field used in the rendered branches must exist in the data (`null` is allowed, `default:` makes it optional); all errors are collected (≤50). Template and data messages are in Russian. Data values are never parsed as tags. `\n` in data becomes a line break. The output zip has fixed timestamps: same template + data → same bytes.
- `Variables()` lists fields for `/templates/check` as `lots[].name` with usage (`value`, `each`, `if`) and filters.
- Formatting rules: `money` → `6 000 000.00` (2 decimals, half away from zero, non-breaking space groups); `number` → `1 500`, `2.5`; dates in `Asia/Almaty` (input ISO `2006-01-02`, RFC 3339, or `2006-01-02 15:04[:05]` read as Kazakhstan time; tzdata embedded); `words`/`moneywords` — Russian counts use masculine forms except `тысяча`, in Kazakh 100 is `жүз` and 1000+ take `бір`; `iin`/`bin` strip spaces/dashes, left-pad JSON numbers and check the digit; `check` → `☒`/`☐`; booleans print `да`/`нет`.
- Without a font in the document LibreOffice uses Carlito; Liberation, DejaVu and Noto in Gotenberg cover Kazakh letters.

## Domain rules

- `documents` rows are immutable (trigger), unique per `(api_client_id, idempotency_key)`, keep the input JSON, `template_name`, `template_sha256` (FK to `template_files`) and `expires_at`. `public_id` (32 random hex) names the file; `id` (UUIDv7) is what the API returns.
- Generation (`GenerateDOCX`): field checks → fingerprint → idempotency lookup (a repeat returns the stored file) → `docx.Parse` → `Render` → for pdf `ConvertDOCX` → document file to `<project>/<yyyy>/<mm>/<public_id>.<ext>` in the documents bucket → one transaction: `LockTemplateFile` (row lock), template copy put to the templates bucket as `<sha256>.docx`, `CreateDocxDocument`. Locking the row before putting the copy keeps cleanup from deleting a copy a new document is about to reference.
- Idempotency fingerprint: SHA-256 of the canonical request (template SHA-256, format, template_name, external_id, requested_by, retention_days, data with sorted keys). Same key + same fingerprint → the existing document; different → `ErrIdempotencyConflict`. A concurrent duplicate loses on the unique index, deletes its file and returns the winner.
- Journal: the generation handler writes one `generation_log` row per request, including rejected ones (`created`, `repeated`, `rejected` 4xx, `failed` 5xx; http status, error code/message/details, duration, size, `request_id`, template name and SHA-256). Never the input data. `document_id` is `ON DELETE SET NULL`, so the journal outlives documents. A journal write failure is only logged.
- Retention: `projects.retention_days` (default 365) is the maximum a request may ask for. `RunCleanup` (every `DOCUMENTS_CLEANUP_INTERVAL`, `0` disables) deletes expired documents in batches of `DOCUMENTS_CLEANUP_BATCH` (`FOR UPDATE SKIP LOCKED`, files first, then rows; a row whose file could not be deleted stays for the next run), then template copies no document references (lock → delete rows, the FK stops it if a document just referenced one → delete files, all in one transaction). Between `expires_at` and cleanup a document answers `410`.
- Tests of `documents` use their own bucket `docgen-test-documents` for documents and template copies: cleanup scans the whole developer database, and only the bucket keeps it from deleting real files.
- Migrations 00004–00008 created the redaction-1 registry; 00009 added the redaction-2 schema; 00010 dropped the registry and its documents and cannot be rolled back. `audit_log` stays for future admin auditing.

## Admin UI (`admin/`)

- Vue 3 + TypeScript + Vite + Element Plus (imported whole, Russian locale), vue-router with hash history; no state library.
- `src/api.ts` is the only place that talks to `/admin/v1`: token in `localStorage`, errors as `ApiError`, 401 sends to the login page. `src/labels.ts` — dates in `Asia/Almaty`.
- Screens: login (skipped when `auth_required` is false), journal (`/journal`: filters in the URL query — portal, template, external ID, statuses, Astana date range; text filters debounced; «Показать ещё» uses `next_cursor`), request card (`/journal/:id`: metadata, error with per-field or per-tag details, document state, input data while kept), reports (`/reports`: totals, by portal, by day, by template; default last 30 days), projects (create/edit with retention). `<router-view :key="route.path">` remounts a page when the path changes but not when only the query does. Served by the Go service at `/admin/` when `ADMIN_UI_DIR` points to the built `admin/dist` (`httpapi.AdminUI`, strict CSP; `frame-ancestors 'self'` so blob: PDFs can be shown in a frame). `npm run build` runs `vue-tsc` first.

## Security decisions

- API keys and admin session tokens are stored only as SHA-256 hashes; admin passwords are bcrypt.
- Templates never execute code: macros, .docm, .dotx are rejected; zip size is capped; data is never interpreted as tags.
- Gotenberg runs with `--webhook-disable` and Chromium closed off; LibreOffice must not reach the network in production (isolate the container's network at deployment).
- Download responses are `attachment` + `nosniff` + `no-store`.

## Conventions

- Code comments are in Russian, short, only where they explain something non-obvious (mostly on functions); no comments about plans.
- Import order: standard, third-party, `docgen/...` (gci).
- `//nolint` always carries a reason.
- Integration tests are skipped unless `DOCGEN_INTEGRATION=1`; DB tests run inside a transaction that is rolled back.
- Commit messages: Conventional Commits, e.g. `feat(backend): ...`.
