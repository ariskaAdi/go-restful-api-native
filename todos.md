# RESTful API Learning Roadmap — Go, no framework (net/http + pgx + PostgreSQL)

Current state of this project (checked 2026-07-27):
- Go module `github.com/ariskaAdi/go-restful-api-native`, Go `1.25.0` (`go.mod`)
- No dependencies yet, no code beyond the module declaration

Suggested domain to build throughout: a simple **Product Catalog API** (`products`, `categories`). Small enough to not distract from the concepts, rich enough to need relationships/filtering later.

**Ground rule:** no web framework (no gin/echo/fiber/chi). Use the standard library `net/http` with Go 1.22+'s pattern-based `ServeMux` (`"GET /products/{id}"`, `r.PathValue("id")`) for routing and middleware. Small, single-purpose libraries (Postgres driver, JWT, bcrypt, migration CLI, testcontainers) are fine to add — they don't own your control flow the way a framework does.

Work top to bottom. Each phase builds on the last — don't skip to Security before you have working CRUD.

---

## Phase 0 — Environment Check
- [ ] Confirm `go version` is 1.22+ (needed for pattern/method routing and `r.PathValue`; go.mod already declares 1.25.0)
- [ ] Install PostgreSQL locally, or run via Docker (recommended so it's disposable/reproducible):
      `docker run --name pg-restful -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=restful_api -p 5432:5432 -d postgres:16`
- [ ] Install a DB client to inspect data (DBeaver, or plain `psql`)
- [ ] Install a REST client for testing endpoints (Postman, Insomnia, or `curl`/`.http` files in VS Code)
- [ ] `go mod tidy` sanity check (no dependencies yet, should be a no-op)

## Phase 1 — Hello Database (first working endpoint)
- [ ] `go get github.com/jackc/pgx/v5` — the only "framework-ish" dependency you need; it's a driver, not a framework
- [ ] Create the `products` table manually via SQL (id, name, price, stock, created_at)
- [ ] Create package layout: `cmd/api`, `internal/db`, `internal/product`
- [ ] Create a `Product` struct (model)
- [ ] Create a `Repository` with a plain `SELECT * FROM products` query method
- [ ] Create a `Handler` with `GET /api/products` that calls the repository directly (skip the service layer just this once, to see the full request→DB round trip)
- [ ] Wire everything in `cmd/api/main.go` with `http.NewServeMux()` + `http.ListenAndServe`
- [ ] Run the app and confirm the endpoint returns data

## Phase 2 — Proper Layered Architecture
- [ ] Introduce a `Service` between handler and repository — handler should never call the repository directly from here on
- [ ] Introduce DTOs (`ProductRequest`, `ProductResponse`) — never expose your DB model directly over the wire
- [ ] Add mapping between model ↔ DTO by hand (small enough not to need a mapping library)
- [ ] Depend on interfaces, not concrete types — and define those interfaces on the **consumer** side (Go idiom: the service package declares the repository interface it needs, not the repository package)
- [ ] Wire dependencies via constructor functions (`NewService(repo)`, `NewHandler(service)`) — no DI container

## Phase 3 — Full CRUD + REST Conventions
- [ ] Implement all endpoints with Go 1.22+ `ServeMux` patterns: `GET /api/v1/products`, `GET /api/v1/products/{id}`, `POST /api/v1/products`, `PUT /api/v1/products/{id}`, `DELETE /api/v1/products/{id}`
- [ ] Return correct status codes: `201 Created` with a `Location` header on create, `204 No Content` on delete, `404 Not Found` when missing, `200 OK` otherwise
- [ ] Use `r.PathValue(...)` + `strconv` for path params, deliberate `w.WriteHeader(...)` for status codes
- [ ] Add pagination via query params (`page`, `size`) and return total count/metadata alongside the list
- [ ] Decide and document your naming/versioning convention now (`/api/v1/products`) — cheap to do early, painful to retrofit

## Phase 4 — Validation & Error Handling
- [ ] Add a `Validate() map[string]string` method on request DTOs (hand-written; or `go-playground/validator` if you'd rather use struct tags)
- [ ] Create a small `apperr` package with typed application errors (`NotFound`, `Validation`, `BadRequest`) instead of raw `errors.New` everywhere
- [ ] Add a central JSON error writer (`httpx.WriteError`) that type-switches on `*apperr.AppError` vs. unknown errors
- [ ] Add a panic-recovery middleware so an unhandled panic becomes a `500` JSON response, not a crashed process
- [ ] Standardize your error response shape (timestamp, status, error code, message, path, field-level validation errors) — pick one shape and reuse it everywhere

## Phase 5 — Raw SQL Deep Dive
- [ ] Add a `categories` table and a one-to-many relationship (category → products); model it with a hand-written `LEFT JOIN` + row-grouping (no ORM "resultMap" — you do the grouping yourself)
- [ ] Implement dynamic SQL filtering by building the query with `strings.Builder`/`fmt.Sprintf` + positional args (`$1`, `$2`, ...) — never string-concatenate user input into SQL
- [ ] Implement sorting via a query param — **whitelist allowed columns** with a `switch`, never interpolate the raw param into `ORDER BY`
- [ ] Implement real pagination in SQL (`LIMIT`/`OFFSET`) plus a separate `COUNT(*)` query for total records
- [ ] Wrap paginated results in a generic `PageResponse[T]` (Go generics) with `content`, `page`, `size`, `total_elements`

## Phase 6 — Database Migrations & Schema Management
- [ ] Add a migration CLI (`goose` or `golang-migrate`) — not a library import, a separate tool your build/deploy calls
- [ ] Move your schema into versioned migration scripts (`00001_init_schema.sql`, `00002_add_categories.sql`, ...)
- [ ] Add a seed-data migration for local dev
- [ ] Stop hand-editing the DB — from now on, every schema change goes through a migration

## Phase 7 — Testing
- [ ] Unit test the `Service` with a hand-written fake implementing the repository interface (table-driven tests, plain `testing` package)
- [ ] Test the repository against a real Postgres (see Testcontainers below) rather than assuming query correctness
- [ ] Add `testcontainers-go` + the Postgres module so integration tests run against a real, disposable Postgres instead of assumptions or SQLite
- [ ] Integration-test handlers with `httptest.NewRecorder` + `httptest.NewRequest` (full request → DB → response round trip)
- [ ] Add coverage visibility: `go test -cover`, `go tool cover -html=coverage.out`

## Phase 8 — API Documentation
- [ ] Hand-write an OpenAPI 3 spec (`openapi.yaml`) describing routes, params, request/response schemas
- [ ] Serve the spec (and optionally a static Swagger UI bundle) via a plain `http.FileServer` — no codegen framework required
- [ ] Keep the spec in sync as the API evolves; consider `swaggo/swag` comment-driven generation later if hand-maintaining gets painful

## Phase 9 — Security
- [ ] Add `github.com/golang-jwt/jwt/v5` for token issuing/parsing
- [ ] Implement JWT-based authentication (login endpoint issuing a token)
- [ ] Hand-write an auth middleware: parse the `Authorization` header, validate the JWT, stash claims in `context.Context`
- [ ] Hash passwords with `golang.org/x/crypto/bcrypt` — never store plaintext
- [ ] Add a hand-written CORS middleware with an explicit allow-list (don't leave it wide open by accident)

## Phase 10 — Production Readiness
- [ ] Config via environment variables (`os.Getenv` + sane defaults), gate dev/prod behavior on an `APP_ENV` var
- [ ] Switch to structured logging with `log/slog` (stdlib — no external logging framework needed)
- [ ] Add a request/correlation ID middleware, propagated via `context.Context`
- [ ] Graceful shutdown: `signal.NotifyContext` + `http.Server.Shutdown`
- [ ] Push `context.Context` (with timeouts) through every repository call so slow queries can't hang a request forever
- [ ] Add `/healthz` and `/readyz` endpoints (hand-written, `/readyz` pings the DB pool)
- [ ] Dockerize the app (multi-stage `Dockerfile`) and write a `docker-compose.yml` that runs app + Postgres together
- [ ] Add a CI pipeline (GitHub Actions): `go build`, `go vet`, `go test`, `golangci-lint`

## Phase 11 — Stretch Goals (optional, pick based on interest)
- [ ] Rate limiting via `golang.org/x/time/rate` wrapped in a hand-written middleware
- [ ] Optimistic locking (`version` column) for concurrent update safety
- [ ] HATEOAS-style links in responses, hand-rolled (no library needed for this)
- [ ] OpenTelemetry tracing across handler → service → repository
- [ ] Outbox pattern / event publishing (Kafka) if you want to explore event-driven architecture
- [ ] Split into more `internal/` packages or a multi-module layout if the project grows

---

### How to use this file
Work sequentially — each phase assumes the previous one's endpoints/tests already work. Check items off as you go (`- [x]`). When a phase introduces a new concept you don't understand yet, stop and ask before moving on — the point is to actually learn Go's stdlib HTTP stack + PostgreSQL, not just copy-paste a working app.
