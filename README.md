<div align="center">

# Gin Template Monolith

**Production-ready Gin monolith with pgx, migrations, Redis, health checks, metrics, and traces**

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Gin](https://img.shields.io/badge/Gin-1.11-00ADD8?logo=go&logoColor=white)](https://gin-gonic.com)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-18-336791?logo=postgresql&logoColor=white)](https://postgresql.org)

Part of the [@teo-garcia/templates](https://github.com/teo-garcia/templates) ecosystem

</div>

---

## Features

| Category          | Technologies                                                        |
| ----------------- | ------------------------------------------------------------------- |
| **Framework**     | Gin 1.11, `net/http`, go-playground/validator                        |
| **Database**      | PostgreSQL 18, pgx/v5 + pgxpool, golang-migrate (embedded SQL)       |
| **Redis**         | go-redis/v9 for rate limiting and cache                              |
| **Observability** | `log/slog` JSON logs, Prometheus, OpenTelemetry (OTLP/HTTP)          |
| **Type Safety**   | Go 1.25, generics on the pagination envelope, typed domain errors    |
| **Testing**       | stdlib `testing`, `httptest`, race detector, in-memory repositories  |
| **Code Quality**  | `golangci-config-shared` (lint + format), `gotest-config-shared`     |
| **DevOps**        | Docker (multi-stage), Compose, GitHub Actions CI/CD                  |

---

## Requirements

- Go 1.25+
- Docker and Docker Compose
- PostgreSQL 18 + Redis (via Compose)

---

## Quick Start

```bash
cp .env.example .env
cp .env.test.example .env.test
docker compose up -d db redis
make db-deploy
make db-seed
make dev
```

The app starts on `http://localhost:3000`. API docs at `/docs` when
`DOCS_ENABLED=true`; OpenAPI JSON at `/openapi.json`.

Or run the whole stack in containers:

```bash
make docker-dev
```

---

## Scripts

| Command | Description |
| --- | --- |
| `make dev` | Load `.env` and start the API |
| `make build` | Build the production binary into `./bin` |
| `make start` | Run the production binary |
| `make check` | Single CI gate: lint + format check + tests |
| `make test` | Unit tests with `-race -shuffle=on` (no infrastructure needed) |
| `make test-integration` | Tests that need Postgres and Redis |
| `make coverage` | Tests with coverage into `coverage/` (text + html) |
| `make lint-check` | Run linters |
| `make format` | Apply gofumpt + gci |
| `make format-check` | Verify formatting without writing |
| `make lint-sync` | Regenerate `.golangci.yml` from `golangci-config-shared` |
| `make db-migrate` | Apply pending migrations locally |
| `make db-deploy` | Apply migrations (pre-deploy step; idempotent) |
| `make db-rollback` | Roll back all migrations (local/test only) |
| `make db-version` | Print the current schema version |
| `make db-seed` | Load deterministic sample data |
| `make docker-dev` | Start the full development stack |
| `make docker-build` | Build the production image |

`make help` lists every target.

---

## API

- `GET /health`, `GET /health/live`, `GET /health/ready` — health
- `GET /metrics` — Prometheus text
- `GET /docs`, `GET /openapi.json` — API documentation
- `GET /api/v1/tasks?page=1&pageSize=20&status=PENDING&priority=1` — paginated list
- `POST /api/v1/tasks` — create
- `GET /api/v1/tasks/{id}` — get one
- `PATCH /api/v1/tasks/{id}` — partial update
- `DELETE /api/v1/tasks/{id}` — soft delete

Success envelope:
`{success, statusCode, timestamp, path, method, data, meta{requestId, version, duration}}`

Error envelope:
`{success: false, statusCode, timestamp, path, method, message, error, errors?, meta{requestId}}`

Paginated `data`: `{data, meta: {total, page, pageSize}}`

`error` is a stable machine-readable class: `ValidationError`, `BadRequestError`,
`UnauthorizedError`, `ForbiddenError`, `NotFoundError`, `ConflictError`,
`RateLimitError`, `TimeoutError`, `DatabaseError`, `InternalServerError`,
`MethodNotAllowedError`.

Health and metrics responses are deliberately **not** enveloped — orchestrators
and scrapers parse them directly.

---

## Structure

```
cmd/
  api/          service entrypoint, graceful shutdown
  migrate/      migration runner (pre-deploy step)
  seed/         deterministic sample data
internal/
  config/       env loading and fail-fast validation
  server/       router assembly, docs page
  modules/
    tasks/      handler -> service -> repository
  shared/
    httpx/      response envelopes, typed API errors, responders
    middleware/ request id, logging, errors, CORS, security, throttle, timeout
    health/     health contract
    metrics/    Prometheus registry and instrumentation
    logging/    slog setup
    tracing/    OpenTelemetry setup
    database/   pgx pool, migrator
    openapi/    OpenAPI document builder
migrations/     embedded SQL migrations
```

Layer rules: handlers only translate HTTP to service calls; business rules live
in the service; SQL lives in the repository; error rendering lives in the error
middleware. Add a domain by copying `internal/modules/tasks`.

---

## Migration Safety

Run migrations as a pre-deploy step with `make db-deploy` **before** the new
version starts. Never run them from application startup, request handlers, the
seed command, or test setup.

Production migrations must be backward-compatible with the running version.
Expand-contract across at least two deploys: add nullable columns/tables/indexes
before code uses them, backfill explicitly, deploy code that stops reading the
old shape, then narrow or drop the schema in a later release.

`make db-deploy` is idempotent — running it with nothing pending succeeds.
Rollback means restoring a known-good backup plus deploying compatible code, or
applying a forward-fix migration. `make db-rollback` is local/test only and
refuses to run when `APP_ENV=production`.

If a migration fails part-way the schema is marked dirty; the runner then
refuses to proceed until a human resolves it.

---

## Environment

See `.env.example`. `make dev`, `make start`, and the database targets load it
automatically. Every variable is validated at startup and the process refuses to
boot on a bad value — all problems are reported at once, not one per restart.

`DATABASE_URL` is the canonical database address and has no default.

### Environment promotion checklist

| Variable | development | staging / production |
| --- | --- | --- |
| `APP_ENV` | `development` | `staging` / `production` |
| `DATABASE_URL` | local Compose credentials | managed instance, rotated secret, never reused from local |
| `CORS_ORIGIN` | `http://localhost:3000` | explicit origin list; `*` is rejected in production |
| `DOCS_ENABLED` | `true` | decide per environment; default it off for public deployments |
| `OPENAPI_SERVER_URL` | `http://localhost:3000` | the public base URL |
| `LOG_LEVEL` / `LOG_JSON` | `info` / `false` | `info` / `true` |
| `THROTTLE_LIMIT` / `THROTTLE_TTL` | `100` / `60` | tune to real traffic |
| `REDIS_PASSWORD` | empty | required |
| `OTEL_ENABLED` / `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` | `false` / local collector | `true` / the environment's collector |

---

## Observability

- Structured JSON logs via `log/slog`; `LOG_LEVEL` controls verbosity and
  `LOG_JSON=false` gives readable local output.
- `X-Request-ID` round-trips: an inbound value is preserved, otherwise one is
  generated. It appears in every log line and in `meta.requestId`.
- Prometheus at `/metrics`: `http_requests_total`,
  `http_request_duration_seconds` (explicit buckets, so p95/p99 work),
  `http_requests_in_flight`, plus Go and process collectors. The `route` label
  is the matched pattern (`/api/v1/tasks/:id`), never a concrete id, so
  cardinality stays bounded.
- OpenTelemetry traces over OTLP/HTTP with W3C trace-context propagation, off by
  default and enabled with `OTEL_ENABLED=true`.
- Attach to the shared local stack with
  `docker compose -f docker-compose.yml -f docker-compose.observability.yml up`.

---

## Security

- Security headers on every response: CSP, `X-Content-Type-Options`,
  `X-Frame-Options`, `Referrer-Policy`, COOP/CORP, `Permissions-Policy`. HSTS is
  emitted only over TLS.
- CORS reflects exactly one allow-listed origin at a time and never the
  wildcard; unlisted origins receive no CORS headers at all.
- Rate limiting is Redis-backed (fixed window per client IP) with
  `X-RateLimit-*` and `Retry-After` headers. Health, metrics, and docs are
  exempt so scrapes keep working under load. A limiter outage fails **open** and
  is logged — a Redis blip must not take the API down.
- Request timeouts propagate through the request context, so database queries
  unwind with the request instead of outliving it.
- Errors never expose stack traces, SQL, driver text, or connection strings;
  the cause is logged server-side and the client gets a safe message.

---

## Shared Configuration

Lint and format settings come from **`golangci-config-shared`** (one package for
both, as golangci-lint v2 owns `run` and `fmt` from a single config — the same
way `ruff` owns lint and format for the Python templates). Test and coverage
settings come from **`gotest-config-shared`**.

`.golangci.yml` is currently committed and marked as synced, because those
packages are not published yet. Once they are, run `make lint-sync`, gitignore
the generated file, and switch `make coverage` to `make coverage-sync`. Put
repo-specific lint rules in `golangci.overrides.yml` — the overlay is deep-merged
onto the shared baseline.

---

## After Creating From This Template

1. Rename the module: replace `github.com/teo-garcia/gin-template-monolith` in
   `go.mod` and all imports.
2. Rename the Compose container names, database name, and
   `OTEL_SERVICE_NAME`.
3. Replace `internal/modules/tasks` with a real domain, then delete
   `cmd/seed`'s sample data and `migrations/000001_create_tasks.*`.
4. Decide `DOCS_ENABLED` per environment and set a real `OPENAPI_SERVER_URL`.
5. Review the environment promotion checklist above before the first deploy.

**Non-goals:** this template does not include authentication, background
workers, or multi-tenancy.

---

## License

MIT © teo-garcia
