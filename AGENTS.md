# DocGen — agent context

DocGen is a standalone service shared by the company's portals: given a template and JSON data it produces a document (DOCX/PDF) and stores it. Portals call it over REST with a per-portal API key; admins manage templates through the admin API.

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
- `make check` — lint + unit tests; `make test-integration` — tests against real Postgres/MinIO/Gotenberg (needs `make up`).

## Layout (`backend/`)

- `cmd/` — `docgen` (server), `migrate`, `keygen`, `adminadd`.
- `migrations/` — goose SQL; `internal/db/queries/*.sql` → sqlc-generated code in `internal/db` (never edit generated files).
- `internal/httpapi` — router, handlers, middleware. Handlers depend on small interfaces declared next to them.
- `internal/templates` — template registry service: every change runs in a transaction and writes `audit_log`.
- `internal/documents` — document generation (`Service.Generate`) and storage; depends on `templates` for `Resolve`/`LoadFile`.
- `internal/dataschema` — JSON Schema compile/validate, per-field errors, schema diff.
- `internal/htmlengine` — html/template rendering of JSON data, template functions (`funcs.go`), footer and watermark.
- `internal/format` — engine-independent formatting of JSON values (money, numbers, dates, amounts in words, BIN/IIN, checkboxes, per-locale values); exact decimals via `math/big`. `format.ValidKzID` is the single BIN/IIN checksum, also used by the `kz-iin`/`kz-bin` schema formats.
- `internal/gotenberg` — HTML → PDF client.
- `internal/storage` (S3), `internal/apikey`, `internal/adminauth`, `internal/config`, `internal/pgerr`.
- `api/openapi.yaml` — API contract; keep it in sync with handlers.

## API

- Portal (`/v1`, header `X-API-Key: dg_...`): `GET /whoami`, `GET /templates`, `GET /templates/{code}/schema?locale=&version=`, `POST /documents` (header `Idempotency-Key` required, body up to 5 MiB; 201 new, 200 repeat), `GET /documents/{id}`, `GET /documents/{id}/file`. The project always comes from the key, never from the request; another project's document is `404`, an expired one is `410`; files are checked against their SHA-256 before sending.
- Admin (`/admin/v1`, `Authorization: Bearer dgs_...` after `POST /login`): projects, templates, version upload (multipart), publish, diff, activate (also rollback), archive, PDF preview of a version on its test data (`GET /versions/{id}/preview`, html engine only).
- Errors: `{"error": {"code", "message", "details?"}}`; `details` holds per-field errors or a schema diff. A JSON body over its limit is `413 request_too_large`.

## Domain rules

- Template = (project, code, locale), locale `ru | kk | bilingual`, engine `html | docx`, status `draft | active | archived`.
- Versions: at most one draft per template; a published version is immutable (DB trigger); the active version must be published and belong to the template (FK + trigger).
- Upload validates the schema (draft 2020-12, root `type: object`, no external `$ref`, formats asserted incl. `kz-iin`/`kz-bin`), validates test data against it and renders the HTML template on it.
- Publish and activate compare the schema with the version portals use now; breaking changes return `409 breaking_changes` unless `?confirm_breaking=true`.
- Documents (`documents` table) are immutable (trigger), reference a published version of the same template (composite FK + trigger), are unique per `(api_client_id, idempotency_key)`, keep the input JSON in the same row and carry `expires_at` computed from the template retention at creation. `public_id` (32 random hex) is what the footer and verification show; `id` is internal UUIDv7.
- Generation: request check → idempotency lookup → `Resolve` → data validated against the version schema (`400 invalid_data` with field errors) → template file read with SHA-256 check → render → PDF with the standard footer showing `public_id` (no watermark) → file to the documents bucket at `<project>/<yyyy>/<mm>/<public_id>.pdf` → row inserted. Only `html` templates producing `pdf` are supported.
- Idempotency: the request fingerprint is SHA-256 of the canonical request (template, locale, version, format, data with sorted keys, external_id, requested_by). Same key + same fingerprint returns the existing document; same key + different fingerprint is `ErrIdempotencyConflict`. A concurrent duplicate loses on the unique index, deletes its file and returns the winner.
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
