# DocGen — agent context

DocGen is a standalone service shared by the company's portals: given a template and JSON data it produces a document (DOCX/PDF) and stores it.

**Spec redaction 2 is approved** (`docs/ТЗ DocGen ред. 2.docx`): a DOCX template with `{{ }}` marks arrives in every request together with JSON data; the service returns DOCX or PDF (LibreOffice in Gotenberg), stores the result with a retention period and logs every request; the admin is read-only (journal, reports). No template registry, HTML templates, editors, signatures or QR. The code is being refactored to it in stages; the sections below still describe the redaction-1 code where it has not been replaced yet. Portals call it over REST with a per-portal API key; admins manage templates through the admin API.

## Stack

- Go (module `docgen` in `backend/`), chi router, pgx + sqlc, goose migrations embedded in the binary.
- PostgreSQL 18, MinIO (S3) for files, Gotenberg 8 (Chromium) for HTML → PDF.
- JSON Schema: `santhosh-tekuri/jsonschema/v6`.
- Local infra: `docker-compose.yml`; config from env (`.env`, loaded by direnv and by the Makefile; see `.env.example`).

## Commands

- `make up` / `make down` — start/stop Postgres, MinIO, Gotenberg.
- `make run`, `make migrate-up`, `make migration name=...`, `make generate` (sqlc).
- `make keygen name=... project=...` — issue a portal API key (printed once).
- `make admin-add email=... name=...` — create an admin (password printed once).
- `make admin-install` / `make admin-dev` (Vite on :5173, proxies `/admin/v1` to `make run` on :8080) / `make admin-build` (→ `admin/dist`).
- `make check` — lint + unit tests; `make test-integration` — tests against real Postgres/MinIO/Gotenberg (needs `make up`).

## Admin UI (`admin/`)

- Vue 3 + TypeScript + Vite + Element Plus (imported whole, Russian locale), vue-router with hash history; no state library.
- `src/api.ts` — the only place that talks to `/admin/v1`: token in `localStorage`, errors as `ApiError` (`code`, `fieldErrors`, `diff` for `breaking_changes`), 401 sends to the login page. `src/labels.ts` — Russian labels, dates in `Asia/Almaty`, `errorText` for known error codes.
- Screens: login, projects, templates (filter by project in the URL), template editor (`/templates/:id/edit`, lazy-loaded CodeMirror 6: HTML with `{{ }}` highlighted, schema and test data as JSON, a field list built from the schema that inserts the right helper — `tr` for ru/kk, `date`, `iin`/`bin`, `check`, `range` for arrays — and a helper cheat sheet; starts from the draft, else the latest version, else a starter skeleton; Ctrl+S saves; preview saves unsaved edits first), template card with versions: upload of a file (schema and test data prefilled from the latest version), publish → offer to activate, activate/rollback, preview PDF in a dialog, schema diff, schema/test data, download the version file. A `409 breaking_changes` shows the changes and repeats the call with `?confirm_breaking=true` only after explicit confirmation.
- Served by the Go service at `/admin/` when `ADMIN_UI_DIR` points to the built `admin/dist` (`httpapi.AdminUI`, strict CSP; `frame-ancestors 'self'` because the blob: preview PDF inherits the page policy). `npm run build` runs `vue-tsc` first.

## Layout (`backend/`)

- `cmd/` — `docgen` (server), `migrate`, `keygen`, `adminadd`.
- `migrations/` — goose SQL; `internal/db/queries/*.sql` → sqlc-generated code in `internal/db` (never edit generated files).
- `internal/httpapi` — router, handlers, middleware. Handlers depend on small interfaces declared next to them.
- `internal/templates` — template registry service: every change runs in a transaction and writes `audit_log`.
- `internal/documents` — document generation (`Service.Generate`) and storage; depends on `templates` for `Resolve`/`LoadFile`.
- `internal/dataschema` — JSON Schema compile/validate, per-field errors, schema diff.
- `internal/docx` — the DOCX engine of redaction 2 (beevik/etree): `Parse` checks the archive (size, zip-bomb limit, no macros/.docm/.dotx), glues tags Word split across runs and moves each tag into its own run, builds blocks; `Template.Render(data)` copies the XML and expands it without touching the template (safe to reuse and run concurrently); `Variables()` lists fields for `/templates/check`. Syntax: `{{ a.b | money }}`, filters `money number date datetime longdate:ru|kk words:ru|kk moneywords:ru|kk iin bin check upper lower capfirst default:text`, `{{#each x}}…{{else}}…{{/each}}`, `{{#if x}}…{{else}}…{{/if}}`, `{{../x}}`, `{{@number}}`, `{{@index}}`, `{{this}}`. A block's mode comes from where its tags are: same paragraph → part of the paragraph; different cells → table rows (from the opening row to the closing row; `{{else}}` starts the "empty" rows); otherwise whole paragraphs, and each tag must then be alone in its paragraph. Every field used in the rendered branches must exist in the data (`null` is allowed, `default:` makes it optional); errors are collected — `*TemplateError` (problems with part and paragraph text) and `*DataError` (fields like `lots[2].name`), messages in Russian. Data values are never parsed as tags. Output zip has fixed timestamps, so the same input gives the same bytes.
- `internal/htmlengine` — html/template rendering of JSON data, template functions (`funcs.go`), footer and watermark.
- `internal/format` — engine-independent formatting of JSON values (money, numbers, dates, amounts in words, BIN/IIN, checkboxes, per-locale values); exact decimals via `math/big`. `format.ValidKzID` is the single BIN/IIN checksum, also used by the `kz-iin`/`kz-bin` schema formats.
- `internal/gotenberg` — Gotenberg client: `ConvertDOCX` (LibreOffice, `/forms/libreoffice/convert`) and `ConvertHTML` (Chromium, goes away with the HTML engine). Errors: `ErrConvert` (Gotenberg rejected the document → template error), `ErrUnavailable` (no connection, timeout, 503/429, no free slot → `503`), anything else → `500`. At most `GOTENBERG_MAX_CONCURRENCY` (default 4) conversions run at once; waiting for a slot counts towards `GOTENBERG_TIMEOUT`. LibreOffice converts even garbage into a PDF, so checking that the input is a real DOCX is `docx.Parse`'s job. Without a font in the document LibreOffice uses Carlito; Kazakh letters render.
- `internal/golden` — golden tests: a synthetic bilingual opening protocol (`testdata/opening_protocol.*`) rendered to HTML and PDF and compared byte for byte with `*.golden.*`. After an intended change to the template, helpers, fonts or Gotenberg, regenerate: `DOCGEN_INTEGRATION=1 go test ./internal/golden -update`, look at the PDF, commit.
- `internal/storage` (S3), `internal/apikey`, `internal/adminauth`, `internal/config`, `internal/pgerr`.
- `api/openapi.yaml` — API contract; keep it in sync with handlers.

## API

- Portal (`/v1`, header `X-API-Key: dg_...`): `POST /documents` — multipart (`template` .docx ≤10 MiB, `data` JSON ≤5 MiB as a field or a file, and required `format` docx|pdf, `template_name`, `external_id`, `requested_by` — the portal's user ID, never a name or IIN — `retention_days` ≤ `projects.retention_days`), header `Idempotency-Key` required; the response body is the file itself with `X-Document-Id`, `X-Document-SHA256`, `X-Document-Expires-At`, `Location`; 201 new, 200 repeat. `POST /templates/check` (multipart `template`) → `{valid, variables}` or `422 invalid_template`. `GET /documents/{id}` (metadata: `template_name`, `template_sha256`, …), `GET /documents/{id}/file`. Still from redaction 1 until stage 5: `GET /whoami`, `GET /templates`, `GET /templates/{code}/schema`. The project always comes from the key, never from the request; another project's document is `404`, an expired one is `410`; files are checked against their SHA-256 before sending. Multipart is read as a stream (`httpapi.readForm`): every field has its own limit, unknown or repeated fields are `400 invalid_form`, over the limit is `413`.
- Public (no key): `GET /v1/verify/{public_id}` — where the QR code points. Browsers (`Accept` has `text/html`) get a bilingual ru/kk page (`httpapi/verify.html`, no JS, strict CSP), others get JSON; both `200` / `404` (also for a malformed id) / `410` (expired). Shows only template name, project name, issue time, SHA-256, size and the signers list (empty until CMS signatures exist) — never the input data, `external_id` or the file.
- Admin (`/admin/v1`, `Authorization: Bearer dgs_...` after `POST /login`): projects, templates, version upload (multipart), publish, diff, activate (also rollback), archive, `GET /templates/{id}`, PDF preview of a version on its test data (`GET /versions/{id}/preview`, html engine only), download of a version file (`GET /versions/{id}/file`, always `attachment` + `octet-stream`).
- Errors: `{"error": {"code", "message", "details?"}}`; `details` holds `docx.FieldError`s (`invalid_data`), `docx.Problem`s (`invalid_template`), or a schema diff (redaction-1 admin). Generation codes: `invalid_form`, `invalid_<field>`, `invalid_template` (422), `invalid_data` (400), `idempotency_conflict` (409), `conversion_failed` (422), `converter_unavailable` (503), `request_too_large` (413).

## Domain rules

- Template = (project, code, locale), locale `ru | kk | bilingual`, engine `html | docx`, status `draft | active | archived`.
- Versions: at most one draft per template; a published version is immutable (DB trigger); the active version must be published and belong to the template (FK + trigger).
- Upload accepts an optional `expected_draft` (`none` or the draft `updated_at` the client started from); if the draft appeared, was replaced or published since — `409 draft_changed`. The editor and the upload dialog always send it, so two admins never silently overwrite each other's draft.
- Upload validates the schema (draft 2020-12, root `type: object`, no external `$ref`, formats asserted incl. `kz-iin`/`kz-bin`), validates test data against it and renders the HTML template on it.
- Publish and activate compare the schema with the version portals use now; breaking changes return `409 breaking_changes` unless `?confirm_breaking=true`.
- Documents (`documents` table) are immutable (trigger), reference a published version of the same template (composite FK + trigger), are unique per `(api_client_id, idempotency_key)`, keep the input JSON in the same row and carry `expires_at` computed from the template retention at creation. `public_id` (32 random hex) is what the footer and verification show; `id` is internal UUIDv7.
- Generation (`documents.Service.GenerateDOCX`): field checks (retention against the project) → fingerprint → idempotency lookup (a repeat returns the stored file) → `docx.Parse` → `Render` → for pdf `gotenberg.ConvertDOCX` → document file to `<project>/<yyyy>/<mm>/<public_id>.<docx|pdf>` in the documents bucket → one transaction: `LockTemplateFile` (row lock), template copy put to the templates bucket as `<sha256>.docx`, `CreateDocxDocument`. The order (lock row, then put the copy) is what keeps cleanup from deleting a copy a new document is about to reference. Every request — including rejected ones — is written to `generation_log` by the handler (`recordGeneration`; a journal failure is only logged).
- Idempotency: the fingerprint is SHA-256 of the canonical request (template SHA-256, format, template_name, external_id, requested_by, retention_days, data with sorted keys). Same key + same fingerprint returns the existing document and file; same key + different fingerprint (other data or another template file) is `ErrIdempotencyConflict`. A concurrent duplicate loses on the unique index, deletes its file and returns the winner.
- Migration 00009 (redaction 2, additive so the old code keeps working until it is removed): `projects.retention_days` (default 365, the maximum a request may ask for); `template_files` (one copy per template SHA-256, `SaveTemplateFile` is `ON CONFLICT DO NOTHING`); `documents.template_name` + `documents.template_sha256` (FK to `template_files`), old `template_id`/`template_version_id` nullable, CHECK: a document comes either from a registry version or from a request template; `generation_log` — one row per generation request (`created`/`repeated`/`rejected`/`failed`, http status, error code/message/details, duration, `request_id`), never the input data; `document_id` is `ON DELETE SET NULL`, so the journal outlives documents removed by retention.
- Retention: `documents.Service.RunCleanup` (started from `main` every `DOCUMENTS_CLEANUP_INTERVAL`, `0` disables) deletes expired documents in batches of `DOCUMENTS_CLEANUP_BATCH`: rows locked with `FOR UPDATE SKIP LOCKED` (safe with several instances), files deleted first, then the rows — file, input JSON and metadata all go; a row whose file could not be deleted stays for the next run. Between `expires_at` and cleanup a document already answers `410`.
- Cleanup also removes template copies no document references any more (`DeleteOrphanTemplates`): lock orphans `FOR UPDATE SKIP LOCKED` → delete rows (the FK stops it if a new document just referenced one) → delete files, all inside the transaction.
- Tests of `documents` use their own bucket `docgen-test-documents`: cleanup scans the whole developer database, and only the bucket keeps it from deleting real files (rows come back with the rollback).
- Concurrency: operations that change a template lock its row (`SELECT ... FOR UPDATE`); there is no in-process locking.
- Template files live in S3 under `<template_id>/v<N>-<sha256 prefix>.<ext>`; the DB stores key and SHA-256, and every read of a file checks it against that SHA-256.
- Template/render errors (bad template, Gotenberg rejected the document) are `422`; infrastructure failures are `500`.

## HTML templates and PDF

- Page size, orientation and margins come from CSS `@page` in the template; without `@page size` the page is A4. Keep a bottom margin of at least ~15mm: the footer is drawn there.
- The service adds the standard footer itself (`htmlengine.Footer`): document identifier on the left, `page / total` on the right.
- Elements with `position: fixed` repeat on every printed page; this is how watermarks are made. Preview adds the watermark `ОБРАЗЕЦ · ҮЛГІ` (`htmlengine.WithWatermark`) and shows the version ID in the footer.
- Template functions: `money` (`6 000 000.00`: 2 decimals, half away from zero, groups by non-breaking space), `number` (`1 500`, `2.5`). Both accept numbers or numeric strings; a missing value prints empty.
- Dates: `date` (`20.10.2026`), `datetime` (`20.10.2026 12:00:12`), both with an optional Go layout (`{{datetime .t "2006-01-02 15:04:05"}}`); `longdate v "ru"|"kk"` (`20 октября 2026 г.` / `2026 жылғы 20 қазан`). Input is ISO: `2006-01-02`, RFC 3339, or `2006-01-02 15:04[:05]` without offset (read as Kazakhstan time). Output is in `Asia/Almaty` (UTC+5 since 2024-03-01, UTC+6 before); tzdata is embedded in the binary.
- In words: `words v "ru"|"kk"` (integers only, below 10^15: `шесть миллионов`, `алты миллион`), `moneywords v "ru"|"kk"` (`шесть миллионов тенге 00 тиын`, `алты миллион теңге 00 тиын`; rounded like `money`), `capfirst` for an upper-case first letter. Russian counts use masculine forms except `тысяча`; in Kazakh 100 is `жүз`, 1000 and higher take `бір` (`бір мың`).
- Per-document functions: `{{docid}}` (public_id), `{{verifyurl}}` (`DOCUMENTS_VERIFY_URL` + public_id) and `{{qr}}` (PNG QR code of the verify URL as a trusted `data:` URL, use as `<img src="{{qr}}">`). `Template.Render(data, RenderOptions)` clones the parsed template and binds these values, so one parsed template is safe to render concurrently. On upload the test render uses a zero document ID; preview uses the version ID. `data:` URLs coming from portal data are blocked by html/template (`#ZgotmplZ`).
- `bin`/`iin` print the 12 digits after removing spaces and dashes, left-pad a JSON number with zeros (lost leading zero of IINs from the 2000s) and fail on a bad check digit. `check` prints `☒` for true, `☐` for false or missing.
- Localized data: a field is either a plain string (same in every language) or `{"ru": "...", "kk": "..."}`; `{{tr .name "kk"}}` picks the language and fails if that translation is missing.
- `eq ne lt le gt ge` are replaced: numbers compare by value (`gt .sum 0` works with JSON numbers), strings as strings, mismatched types are not equal and cannot be ordered.
- JSON numbers are `json.Number`, so `{{if .count}}` is true even for 0; use `gt .count 0`.
- Give every template a `<title>`: it becomes the PDF title shown by viewers; without it Gotenberg puts a random temp file name there, and the PDF stops being reproducible. With a title, PDFs of the same version and data differ only in `/CreationDate` and `/ModDate`.
- `dict` builds a map for named blocks (`{{template "block" (dict "lang" "kk" "doc" .)}}` — how bilingual documents render one block per language); `inc` turns a 0-based `range` index into a row number.
- Fonts available in Gotenberg with full Kazakh Cyrillic: Liberation Serif/Sans (Times New Roman/Arial metrics), DejaVu, Noto. A template is a single file, so images must be inline `data:` URLs.

## Security decisions

- API keys and admin session tokens are stored only as SHA-256 hashes; admin passwords are bcrypt.
- Gotenberg runs with no network access and no JavaScript (`--chromium-deny-private-ips --chromium-deny-public-ips --chromium-disable-javascript --webhook-disable`); documents must embed or attach all images and fonts.
- HTML rendering uses html/template escaping and numbers as `json.Number`; rendered HTML and PDF sizes are capped.

## Conventions

- Code comments are in Russian, short, only where they explain something non-obvious (mostly on functions); no comments about plans.
- Import order: standard, third-party, `docgen/...` (gci).
- `//nolint` always carries a reason.
- Integration tests are skipped unless `DOCGEN_INTEGRATION=1`; DB tests run inside a transaction that is rolled back.
- Commit messages: Conventional Commits, e.g. `feat(backend): ...`.
