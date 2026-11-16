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
- `internal/dataschema` — JSON Schema compile/validate, per-field errors, schema diff.
- `internal/htmlengine` — html/template rendering of JSON data.
- `internal/gotenberg` — HTML → PDF client.
- `internal/storage` (S3), `internal/apikey`, `internal/adminauth`, `internal/config`, `internal/pgerr`.
- `api/openapi.yaml` — API contract; keep it in sync with handlers.

## API

- Portal (`/v1`, header `X-API-Key: dg_...`): `GET /whoami`, `GET /templates`, `GET /templates/{code}/schema?locale=&version=`. The project always comes from the key, never from the request.
- Admin (`/admin/v1`, `Authorization: Bearer dgs_...` after `POST /login`): projects, templates, version upload (multipart), publish, diff, activate (also rollback), archive, PDF preview of a version on its test data (`GET /versions/{id}/preview`, html engine only).
- Errors: `{"error": {"code", "message", "details?"}}`; `details` holds per-field errors or a schema diff.

## Domain rules

- Template = (project, code, locale), locale `ru | kk | bilingual`, engine `html | docx`, status `draft | active | archived`.
- Versions: at most one draft per template; a published version is immutable (DB trigger); the active version must be published and belong to the template (FK + trigger).
- Upload validates the schema (draft 2020-12, root `type: object`, no external `$ref`, formats asserted incl. `kz-iin`/`kz-bin`), validates test data against it and renders the HTML template on it.
- Publish and activate compare the schema with the version portals use now; breaking changes return `409 breaking_changes` unless `?confirm_breaking=true`.
- Concurrency: operations that change a template lock its row (`SELECT ... FOR UPDATE`); there is no in-process locking.
- Template files live in S3 under `<template_id>/v<N>-<sha256 prefix>.<ext>`; the DB stores key and SHA-256, and every read of a file checks it against that SHA-256.
- Template/render errors (bad template, Gotenberg rejected the document) are `422`; infrastructure failures are `500`.

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
