# Step-by-Step Dummy Code — Product Catalog API (Go, no framework)

Companion to [todos.md](todos.md). Same phases, same order — but every step here has copy-pasteable code so you don't get lost.

Domain: **Product Catalog** — `products` belong to `categories`. Module: `github.com/ariskaAdi/go-restful-api-native` (matches your `go.mod`).

Stack: standard library `net/http` (Go 1.22+ pattern-based `ServeMux`, no gin/echo/fiber/chi) + `github.com/jackc/pgx/v5` for PostgreSQL access. No ORM — you write the SQL.

Do not skip ahead — later phases assume earlier files exist and replace things from earlier phases (e.g. Phase 4 replaces the placeholder sentinel error from Phase 3 with a typed `apperr.AppError`).

---

## Phase 0 — Environment Check

```bash
go version   # need 1.22+, go.mod already targets 1.25.0

docker run --name pg-restful \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=restful_api \
  -p 5432:5432 -d postgres:16
```

No app code yet. Confirm you can connect: `psql -h localhost -U postgres -d restful_api` (password `postgres`).

---

## Phase 1 — Hello Database

```bash
go get github.com/jackc/pgx/v5
```

**SQL — run manually via psql/DBeaver for now (migrations take over in Phase 6):**
```sql
CREATE TABLE products (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    price NUMERIC(10, 2) NOT NULL,
    stock INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT now()
);

INSERT INTO products (name, price, stock) VALUES
  ('Keyboard', 49.99, 100),
  ('Mouse', 19.99, 200),
  ('Monitor', 199.99, 50);
```

**`internal/db/db.go`**
```go
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return pool, nil
}
```

**`internal/product/model.go`**
```go
package product

import "time"

// NOTE: float64 is fine for learning; for real money math prefer an exact
// decimal type (e.g. github.com/shopspring/decimal) to avoid rounding drift.
type Product struct {
	ID        int64
	Name      string
	Price     float64
	Stock     int
	CreatedAt time.Time
}
```

**`internal/product/repository.go`**
```go
package product

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) FindAll(ctx context.Context) ([]Product, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT id, name, price, stock, created_at FROM products")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []Product
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Price, &p.Stock, &p.CreatedAt); err != nil {
			return nil, err
		}
		products = append(products, p)
	}
	return products, rows.Err()
}
```

**`internal/product/handler.go`** — direct call, just for this phase
```go
package product

import (
	"encoding/json"
	"net/http"
)

type Handler struct {
	repo *Repository // direct dependency, just for this phase
}

func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

func (h *Handler) GetAll(w http.ResponseWriter, r *http.Request) {
	products, err := h.repo.FindAll(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(products)
}
```

**`cmd/api/main.go`**
```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/ariskaAdi/go-restful-api-native/internal/db"
	"github.com/ariskaAdi/go-restful-api-native/internal/product"
)

func main() {
	ctx := context.Background()

	pool, err := db.NewPool(ctx, "postgres://postgres:postgres@localhost:5432/restful_api")
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	repo := product.NewRepository(pool)
	handler := product.NewHandler(repo)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/products", handler.GetAll)

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

Run the app (`go run ./cmd/api`), hit `GET http://localhost:8080/api/products` — you should see the 3 seeded rows.

---

## Phase 2 — Layered Architecture + DTOs

**`internal/product/dto.go`**
```go
package product

import "time"

type ProductRequest struct {
	Name  string  `json:"name"`
	Price float64 `json:"price"`
	Stock int     `json:"stock"`
}

type ProductResponse struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Price     float64   `json:"price"`
	Stock     int       `json:"stock"`
	CreatedAt time.Time `json:"created_at"`
}

func toResponse(p Product) ProductResponse {
	return ProductResponse{
		ID:        p.ID,
		Name:      p.Name,
		Price:     p.Price,
		Stock:     p.Stock,
		CreatedAt: p.CreatedAt,
	}
}
```

**`internal/product/service.go`**
```go
package product

import "context"

// Interface owned by the consumer (the service), not the repository package —
// that's the Go idiom: accept interfaces, return structs.
type repository interface {
	FindAll(ctx context.Context) ([]Product, error)
}

type Service struct {
	repo repository
}

func NewService(repo repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) FindAll(ctx context.Context) ([]ProductResponse, error) {
	products, err := s.repo.FindAll(ctx)
	if err != nil {
		return nil, err
	}
	responses := make([]ProductResponse, 0, len(products))
	for _, p := range products {
		responses = append(responses, toResponse(p))
	}
	return responses, nil
}
```

**`internal/product/handler.go` — updated to go through the service**
```go
package product

import (
	"context"
	"encoding/json"
	"net/http"
)

type service interface {
	FindAll(ctx context.Context) ([]ProductResponse, error)
}

type Handler struct {
	service service
}

func NewHandler(service service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GetAll(w http.ResponseWriter, r *http.Request) {
	products, err := h.service.FindAll(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(products)
}
```

**`cmd/api/main.go` — wiring updated**
```go
repo := product.NewRepository(pool)
svc := product.NewService(repo)
handler := product.NewHandler(svc)
```

Handler now never touches the repository directly — that's the rule from here on.

---

## Phase 3 — Full CRUD + REST Conventions

**`internal/product/repository.go` — add CRUD methods:**
```go
package product

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("product not found") // placeholder — Phase 4 replaces call sites with apperr.NotFound

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) FindAll(ctx context.Context) ([]Product, error) {
	// unchanged from Phase 1
	rows, err := r.pool.Query(ctx, "SELECT id, name, price, stock, created_at FROM products")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []Product
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Price, &p.Stock, &p.CreatedAt); err != nil {
			return nil, err
		}
		products = append(products, p)
	}
	return products, rows.Err()
}

func (r *Repository) FindPage(ctx context.Context, size, offset int) ([]Product, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT id, name, price, stock, created_at FROM products ORDER BY id LIMIT $1 OFFSET $2",
		size, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []Product
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Price, &p.Stock, &p.CreatedAt); err != nil {
			return nil, err
		}
		products = append(products, p)
	}
	return products, rows.Err()
}

func (r *Repository) FindByID(ctx context.Context, id int64) (Product, error) {
	var p Product
	err := r.pool.QueryRow(ctx,
		"SELECT id, name, price, stock, created_at FROM products WHERE id = $1", id).
		Scan(&p.ID, &p.Name, &p.Price, &p.Stock, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, ErrNotFound
	}
	return p, err
}

func (r *Repository) Insert(ctx context.Context, p *Product) error {
	return r.pool.QueryRow(ctx,
		"INSERT INTO products (name, price, stock) VALUES ($1, $2, $3) RETURNING id, created_at",
		p.Name, p.Price, p.Stock).Scan(&p.ID, &p.CreatedAt)
}

func (r *Repository) Update(ctx context.Context, p Product) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE products SET name=$1, price=$2, stock=$3 WHERE id=$4",
		p.Name, p.Price, p.Stock, p.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) Delete(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, "DELETE FROM products WHERE id = $1", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
```

**`internal/product/service.go` — add CRUD methods:**
```go
package product

import (
	"context"
	"fmt"
)

type repository interface {
	FindAll(ctx context.Context) ([]Product, error)
	FindPage(ctx context.Context, size, offset int) ([]Product, error)
	FindByID(ctx context.Context, id int64) (Product, error)
	Insert(ctx context.Context, p *Product) error
	Update(ctx context.Context, p Product) error
	Delete(ctx context.Context, id int64) error
}

type Service struct {
	repo repository
}

func NewService(repo repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) FindPage(ctx context.Context, page, size int) ([]ProductResponse, error) {
	products, err := s.repo.FindPage(ctx, size, page*size)
	if err != nil {
		return nil, err
	}
	responses := make([]ProductResponse, 0, len(products))
	for _, p := range products {
		responses = append(responses, toResponse(p))
	}
	return responses, nil
}

func (s *Service) FindByID(ctx context.Context, id int64) (ProductResponse, error) {
	p, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return ProductResponse{}, fmt.Errorf("find product %d: %w", id, err)
	}
	return toResponse(p), nil
}

func (s *Service) Create(ctx context.Context, req ProductRequest) (ProductResponse, error) {
	p := Product{Name: req.Name, Price: req.Price, Stock: req.Stock}
	if err := s.repo.Insert(ctx, &p); err != nil {
		return ProductResponse{}, err
	}
	return toResponse(p), nil
}

func (s *Service) Update(ctx context.Context, id int64, req ProductRequest) (ProductResponse, error) {
	existing, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return ProductResponse{}, fmt.Errorf("find product %d: %w", id, err)
	}
	existing.Name = req.Name
	existing.Price = req.Price
	existing.Stock = req.Stock
	if err := s.repo.Update(ctx, existing); err != nil {
		return ProductResponse{}, err
	}
	return toResponse(existing), nil
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}
```
(Errors still bubble up as the raw `ErrNotFound` sentinel wrapped with `fmt.Errorf` — replaced by a proper typed error in Phase 4.)

**`internal/product/handler.go` — full CRUD:**
```go
package product

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

type service interface {
	FindPage(ctx context.Context, page, size int) ([]ProductResponse, error)
	FindByID(ctx context.Context, id int64) (ProductResponse, error)
	Create(ctx context.Context, req ProductRequest) (ProductResponse, error)
	Update(ctx context.Context, id int64, req ProductRequest) (ProductResponse, error)
	Delete(ctx context.Context, id int64) error
}

type Handler struct {
	service service
}

func NewHandler(service service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GetAll(w http.ResponseWriter, r *http.Request) {
	page, size := parsePaging(r)
	products, err := h.service.FindPage(r.Context(), page, size)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, products)
}

func (h *Handler) GetByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	product, err := h.service.FindByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, product)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req ProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	created, err := h.service.Create(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Location", fmt.Sprintf("/api/v1/products/%d", created.ID))
	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var req ProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	updated, err := h.service.Update(r.Context(), id, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := h.service.Delete(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parsePaging(r *http.Request) (page, size int) {
	page, size = 0, 20
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			page = n
		}
	}
	if v := r.URL.Query().Get("size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			size = n
		}
	}
	return page, size
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
```

**`cmd/api/main.go` — register full CRUD under `/api/v1`:**
```go
mux := http.NewServeMux()
mux.HandleFunc("GET /api/v1/products", handler.GetAll)
mux.HandleFunc("GET /api/v1/products/{id}", handler.GetByID)
mux.HandleFunc("POST /api/v1/products", handler.Create)
mux.HandleFunc("PUT /api/v1/products/{id}", handler.Update)
mux.HandleFunc("DELETE /api/v1/products/{id}", handler.Delete)
```

Note the route moved to `/api/v1/products` — versioning decided now, not retrofitted later.

---

## Phase 4 — Validation & Error Handling

**`internal/apperr/errors.go`**
```go
package apperr

import "net/http"

type AppError struct {
	Status  int
	Code    string
	Message string
	Fields  map[string]string
}

func (e *AppError) Error() string { return e.Message }

func NotFound(message string) *AppError {
	return &AppError{Status: http.StatusNotFound, Code: "NOT_FOUND", Message: message}
}

func Validation(fields map[string]string) *AppError {
	return &AppError{Status: http.StatusBadRequest, Code: "VALIDATION_FAILED", Message: "Validation failed", Fields: fields}
}

func BadRequest(message string) *AppError {
	return &AppError{Status: http.StatusBadRequest, Code: "BAD_REQUEST", Message: message}
}
```

**`internal/httpx/response.go`**
```go
package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/ariskaAdi/go-restful-api-native/internal/apperr"
)

type ErrorResponse struct {
	Timestamp   time.Time         `json:"timestamp"`
	Status      int               `json:"status"`
	Error       string            `json:"error"`
	Message     string            `json:"message"`
	Path        string            `json:"path"`
	FieldErrors map[string]string `json:"field_errors,omitempty"`
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		json.NewEncoder(w).Encode(v)
	}
}

func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var appErr *apperr.AppError
	if errors.As(err, &appErr) {
		WriteJSON(w, appErr.Status, ErrorResponse{
			Timestamp:   time.Now(),
			Status:      appErr.Status,
			Error:       appErr.Code,
			Message:     appErr.Message,
			Path:        r.URL.Path,
			FieldErrors: appErr.Fields,
		})
		return
	}

	slog.Error("unhandled error", "err", err, "path", r.URL.Path)
	WriteJSON(w, http.StatusInternalServerError, ErrorResponse{
		Timestamp: time.Now(),
		Status:    http.StatusInternalServerError,
		Error:     "INTERNAL_ERROR",
		Message:   "Something went wrong",
		Path:      r.URL.Path,
	})
}
```

**`internal/middleware/recover.go`**
```go
package middleware

import (
	"log/slog"
	"net/http"
)

func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered", "err", rec, "path", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":"INTERNAL_ERROR","message":"Something went wrong"}`))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
```

**`internal/product/dto.go` — add validation:**
```go
package product

import (
	"strings"
	"time"
)

type ProductRequest struct {
	Name  string  `json:"name"`
	Price float64 `json:"price"`
	Stock int     `json:"stock"`
}

func (req ProductRequest) Validate() map[string]string {
	fields := map[string]string{}
	if strings.TrimSpace(req.Name) == "" {
		fields["name"] = "name is required"
	}
	if req.Price <= 0 {
		fields["price"] = "price must be greater than 0"
	}
	if req.Stock < 0 {
		fields["stock"] = "stock cannot be negative"
	}
	return fields
}

type ProductResponse struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Price     float64   `json:"price"`
	Stock     int       `json:"stock"`
	CreatedAt time.Time `json:"created_at"`
}

func toResponse(p Product) ProductResponse {
	return ProductResponse{
		ID:        p.ID,
		Name:      p.Name,
		Price:     p.Price,
		Stock:     p.Stock,
		CreatedAt: p.CreatedAt,
	}
}
```

**`internal/product/service.go` — replace the sentinel with `apperr.NotFound`:**
```go
func (s *Service) FindByID(ctx context.Context, id int64) (ProductResponse, error) {
	p, err := s.repo.FindByID(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return ProductResponse{}, apperr.NotFound(fmt.Sprintf("product %d not found", id))
	}
	if err != nil {
		return ProductResponse{}, err
	}
	return toResponse(p), nil
}

func (s *Service) Update(ctx context.Context, id int64, req ProductRequest) (ProductResponse, error) {
	existing, err := s.repo.FindByID(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return ProductResponse{}, apperr.NotFound(fmt.Sprintf("product %d not found", id))
	}
	if err != nil {
		return ProductResponse{}, err
	}
	existing.Name = req.Name
	existing.Price = req.Price
	existing.Stock = req.Stock
	if err := s.repo.Update(ctx, existing); err != nil {
		return ProductResponse{}, err
	}
	return toResponse(existing), nil
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			return apperr.NotFound(fmt.Sprintf("product %d not found", id))
		}
		return err
	}
	return nil
}
```
(Add `"errors"` and `"github.com/ariskaAdi/go-restful-api-native/internal/apperr"` to the imports.)

**`internal/product/handler.go` — validate + delegate error rendering to `httpx`:**
```go
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req ProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, apperr.BadRequest("invalid request body"))
		return
	}
	if fields := req.Validate(); len(fields) > 0 {
		httpx.WriteError(w, r, apperr.Validation(fields))
		return
	}

	created, err := h.service.Create(r.Context(), req)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.Header().Set("Location", fmt.Sprintf("/api/v1/products/%d", created.ID))
	httpx.WriteJSON(w, http.StatusCreated, created)
}
```
Apply the same `httpx.WriteError(w, r, err)` swap to `GetByID`, `Update`, and `Delete`'s error branches, and the same `Validate()` check to `Update`.

**`cmd/api/main.go` — wrap the mux with the recovery middleware:**
```go
handlerChain := middleware.Recover(mux)
log.Fatal(http.ListenAndServe(":8080", handlerChain))
```

---

## Phase 5 — Raw SQL Deep Dive (relationships, dynamic filtering, pagination)

**SQL — add categories + link products to them:**
```sql
CREATE TABLE categories (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL
);

ALTER TABLE products ADD COLUMN category_id INT REFERENCES categories(id);

INSERT INTO categories (name) VALUES ('Peripherals'), ('Displays');
UPDATE products SET category_id = 1 WHERE name IN ('Keyboard', 'Mouse');
UPDATE products SET category_id = 2 WHERE name = 'Monitor';
```

**`internal/product/model.go` — add `CategoryID`:**
```go
type Product struct {
	ID         int64
	Name       string
	Price      float64
	Stock      int
	CategoryID *int64 // nullable FK
	CreatedAt  time.Time
}
```

**`internal/product/repository.go` — dynamic search, safe sorting, pagination:**
```go
package product

import (
	"context"
	"fmt"
)

type SearchFilter struct {
	Name       string
	CategoryID *int64
	SortBy     string
	Size       int
	Offset     int
}

// Notice sortBy is matched against a fixed switch, not interpolated directly —
// that's the SQL-injection-safe way to let users control ORDER BY.
func (r *Repository) Search(ctx context.Context, f SearchFilter) ([]Product, error) {
	query := `SELECT id, name, price, stock, category_id, created_at FROM products WHERE 1=1`
	var args []any
	argPos := 1

	if f.Name != "" {
		query += fmt.Sprintf(" AND name ILIKE $%d", argPos)
		args = append(args, "%"+f.Name+"%")
		argPos++
	}
	if f.CategoryID != nil {
		query += fmt.Sprintf(" AND category_id = $%d", argPos)
		args = append(args, *f.CategoryID)
		argPos++
	}

	switch f.SortBy {
	case "price":
		query += " ORDER BY price"
	case "name":
		query += " ORDER BY name"
	default:
		query += " ORDER BY id"
	}

	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argPos, argPos+1)
	args = append(args, f.Size, f.Offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []Product
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Price, &p.Stock, &p.CategoryID, &p.CreatedAt); err != nil {
			return nil, err
		}
		products = append(products, p)
	}
	return products, rows.Err()
}

func (r *Repository) Count(ctx context.Context, f SearchFilter) (int64, error) {
	query := `SELECT COUNT(*) FROM products WHERE 1=1`
	var args []any
	argPos := 1

	if f.Name != "" {
		query += fmt.Sprintf(" AND name ILIKE $%d", argPos)
		args = append(args, "%"+f.Name+"%")
		argPos++
	}
	if f.CategoryID != nil {
		query += fmt.Sprintf(" AND category_id = $%d", argPos)
		args = append(args, *f.CategoryID)
	}

	var total int64
	err := r.pool.QueryRow(ctx, query, args...).Scan(&total)
	return total, err
}
```

**`internal/product/page.go` — generic page wrapper:**
```go
package product

type PageResponse[T any] struct {
	Content       []T   `json:"content"`
	Page          int   `json:"page"`
	Size          int   `json:"size"`
	TotalElements int64 `json:"total_elements"`
}
```

**`internal/product/service.go` — `FindPage` now calls `Search` + `Count`:**
```go
func (s *Service) FindPage(ctx context.Context, page, size int, name string, categoryID *int64, sortBy string) (PageResponse[ProductResponse], error) {
	filter := SearchFilter{Name: name, CategoryID: categoryID, SortBy: sortBy, Size: size, Offset: page * size}

	products, err := s.repo.Search(ctx, filter)
	if err != nil {
		return PageResponse[ProductResponse]{}, err
	}
	total, err := s.repo.Count(ctx, filter)
	if err != nil {
		return PageResponse[ProductResponse]{}, err
	}

	responses := make([]ProductResponse, 0, len(products))
	for _, p := range products {
		responses = append(responses, toResponse(p))
	}
	return PageResponse[ProductResponse]{Content: responses, Page: page, Size: size, TotalElements: total}, nil
}
```
Update the `repository` interface in `service.go` to replace `FindPage` with `Search` and add `Count`. Update `handler.GetAll` to read `name`, `category_id`, `sort_by` query params and pass them through.

**`internal/category/model.go`**
```go
package category

import "github.com/ariskaAdi/go-restful-api-native/internal/product"

type Category struct {
	ID       int64
	Name     string
	Products []product.Product
}
```

**`internal/category/repository.go` — one-to-many, hand-grouped (no ORM resultMap, you do the grouping):**
```go
package category

import (
	"context"

	"github.com/ariskaAdi/go-restful-api-native/internal/product"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) FindWithProducts(ctx context.Context, id int64) (Category, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, c.name, p.id, p.name, p.price, p.stock
		FROM categories c
		LEFT JOIN products p ON p.category_id = c.id
		WHERE c.id = $1`, id)
	if err != nil {
		return Category{}, err
	}
	defer rows.Close()

	var cat Category
	found := false
	for rows.Next() {
		var (
			pID    *int64
			pName  *string
			pPrice *float64
			pStock *int
		)
		if err := rows.Scan(&cat.ID, &cat.Name, &pID, &pName, &pPrice, &pStock); err != nil {
			return Category{}, err
		}
		found = true
		if pID != nil {
			cat.Products = append(cat.Products, product.Product{
				ID: *pID, Name: *pName, Price: *pPrice, Stock: *pStock,
			})
		}
	}
	if !found {
		return Category{}, ErrNotFound
	}
	return cat, rows.Err()
}
```
(`ErrNotFound` defined the same way as `product.ErrNotFound` — a package-local sentinel.)

---

## Phase 6 — Database Migrations

```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
```

**`db/migrations/00001_init_schema.sql`**
```sql
-- +goose Up
CREATE TABLE categories (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL
);

CREATE TABLE products (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    price NUMERIC(10, 2) NOT NULL,
    stock INT NOT NULL DEFAULT 0,
    category_id INT REFERENCES categories(id),
    created_at TIMESTAMP NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE products;
DROP TABLE categories;
```

**`db/migrations/00002_seed_data.sql`**
```sql
-- +goose Up
INSERT INTO categories (name) VALUES ('Peripherals'), ('Displays');

INSERT INTO products (name, price, stock, category_id) VALUES
  ('Keyboard', 49.99, 100, 1),
  ('Mouse', 19.99, 200, 1),
  ('Monitor', 199.99, 50, 2);

-- +goose Down
DELETE FROM products;
DELETE FROM categories;
```

Drop your manually-created tables first (`DROP TABLE products, categories CASCADE;`), then run:
```bash
goose -dir db/migrations postgres "postgres://postgres:postgres@localhost:5432/restful_api?sslmode=disable" up
```
From now on, all schema changes are new `NNNNN_description.sql` files, never manual `ALTER TABLE`.

---

## Phase 7 — Testing

**`internal/product/service_test.go` — unit test, fake repository:**
```go
package product

import (
	"context"
	"errors"
	"testing"
)

type fakeRepo struct {
	findByIDFn func(ctx context.Context, id int64) (Product, error)
}

func (f *fakeRepo) FindAll(ctx context.Context) ([]Product, error) { return nil, nil }
func (f *fakeRepo) Search(ctx context.Context, filter SearchFilter) ([]Product, error) { return nil, nil }
func (f *fakeRepo) Count(ctx context.Context, filter SearchFilter) (int64, error)       { return 0, nil }
func (f *fakeRepo) FindByID(ctx context.Context, id int64) (Product, error)             { return f.findByIDFn(ctx, id) }
func (f *fakeRepo) Insert(ctx context.Context, p *Product) error                        { return nil }
func (f *fakeRepo) Update(ctx context.Context, p Product) error                         { return nil }
func (f *fakeRepo) Delete(ctx context.Context, id int64) error                          { return nil }

func TestService_FindByID_NotFound(t *testing.T) {
	repo := &fakeRepo{
		findByIDFn: func(ctx context.Context, id int64) (Product, error) {
			return Product{}, ErrNotFound
		},
	}
	svc := NewService(repo)

	_, err := svc.FindByID(context.Background(), 99)

	var appErr *apperr.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *apperr.AppError, got %T (%v)", err, err)
	}
	if appErr.Status != 404 {
		t.Fatalf("expected 404, got %d", appErr.Status)
	}
}
```
(Add `"github.com/ariskaAdi/go-restful-api-native/internal/apperr"` to the imports.)

**`internal/product/handler_test.go` — `httptest`:**
```go
package product

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeService struct{}

func (f *fakeService) FindPage(ctx context.Context, page, size int, name string, categoryID *int64, sortBy string) (PageResponse[ProductResponse], error) {
	return PageResponse[ProductResponse]{Content: []ProductResponse{{ID: 1, Name: "Keyboard"}}}, nil
}
func (f *fakeService) FindByID(ctx context.Context, id int64) (ProductResponse, error) { return ProductResponse{}, nil }
func (f *fakeService) Create(ctx context.Context, req ProductRequest) (ProductResponse, error) { return ProductResponse{}, nil }
func (f *fakeService) Update(ctx context.Context, id int64, req ProductRequest) (ProductResponse, error) { return ProductResponse{}, nil }
func (f *fakeService) Delete(ctx context.Context, id int64) error { return nil }

func TestHandler_GetAll(t *testing.T) {
	handler := NewHandler(&fakeService{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	rec := httptest.NewRecorder()

	handler.GetAll(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
```

**`internal/product/repository_test.go` — integration test against real Postgres via Testcontainers:**
```bash
go get github.com/testcontainers/testcontainers-go
go get github.com/testcontainers/testcontainers-go/modules/postgres
```
```go
package product

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go"
)

func TestRepository_InsertAndFind(t *testing.T) {
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx, "postgres:16",
		postgres.WithDatabase("restful_api"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategyAndDeadline(60, nil),
	)
	if err != nil {
		t.Fatalf("start container: %v", err)
	}
	defer pgContainer.Terminate(ctx)

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	_, err = pool.Exec(ctx, `CREATE TABLE products (
		id SERIAL PRIMARY KEY, name VARCHAR(255) NOT NULL,
		price NUMERIC(10,2) NOT NULL, stock INT NOT NULL DEFAULT 0,
		category_id INT, created_at TIMESTAMP NOT NULL DEFAULT now())`)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := NewRepository(pool)
	p := &Product{Name: "Webcam", Price: 59.90, Stock: 10}
	if err := repo.Insert(ctx, p); err != nil {
		t.Fatalf("insert: %v", err)
	}

	found, err := repo.FindByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found.Name != "Webcam" {
		t.Fatalf("expected Webcam, got %s", found.Name)
	}
}
```

Run everything: `go test ./... -cover`

---

## Phase 8 — API Documentation

**`api/openapi.yaml`** (hand-written spec, versioned alongside the code):
```yaml
openapi: 3.0.3
info:
  title: Product Catalog API
  version: "1.0"
paths:
  /api/v1/products:
    get:
      summary: List products (paginated, filterable)
      parameters:
        - { name: page, in: query, schema: { type: integer, default: 0 } }
        - { name: size, in: query, schema: { type: integer, default: 20 } }
        - { name: name, in: query, schema: { type: string } }
        - { name: category_id, in: query, schema: { type: integer } }
        - { name: sort_by, in: query, schema: { type: string, enum: [id, name, price] } }
      responses:
        "200": { description: OK }
    post:
      summary: Create a product
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: "#/components/schemas/ProductRequest" }
      responses:
        "201": { description: Created }
        "400": { description: Validation failed }
components:
  schemas:
    ProductRequest:
      type: object
      required: [name, price, stock]
      properties:
        name: { type: string }
        price: { type: number }
        stock: { type: integer }
```

**`cmd/api/main.go` — serve the spec (and, optionally, a downloaded Swagger UI bundle) with a plain file server:**
```go
mux.Handle("GET /openapi.yaml", http.FileServer(http.Dir("api")))
// Optional: download the swagger-ui `dist` folder into `web/swagger-ui/` and serve it:
// mux.Handle("GET /docs/", http.StripPrefix("/docs/", http.FileServer(http.Dir("web/swagger-ui"))))
```
No codegen framework needed — the spec is just a file you keep honest by hand. If hand-maintaining becomes painful once the API is large, revisit with `swaggo/swag` comment-driven generation.

---

## Phase 9 — Security (JWT sketch)

```bash
go get github.com/golang-jwt/jwt/v5
go get golang.org/x/crypto/bcrypt
```

**`internal/auth/jwt.go` — skeleton:**
```go
package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var signingKey = []byte("replace-with-env-secret") // load from env in Phase 10, never hardcode in prod

func IssueToken(userID int64) (string, error) {
	claims := jwt.MapClaims{
		"sub": userID,
		"exp": time.Now().Add(24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(signingKey)
}

func ParseToken(tokenString string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		return signingKey, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, jwt.ErrTokenInvalidClaims
	}
	return claims, nil
}
```

**`internal/middleware/auth.go` — skeleton:**
```go
package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/ariskaAdi/go-restful-api-native/internal/auth"
)

type ctxKey string

const ClaimsKey ctxKey = "claims"

func Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		claims, err := auth.ParseToken(token)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), ClaimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
```
This is intentionally a skeleton — the full login flow (user store, bcrypt password check, refresh tokens) is a bigger chunk of code; ask when you get here and we'll write it against your actual login flow.

---

## Phase 10 — Production Readiness (snippets)

**`internal/config/config.go`**
```go
package config

import "os"

type Config struct {
	Env    string
	Port   string
	DBDSN  string
}

func Load() Config {
	return Config{
		Env:   getEnv("APP_ENV", "dev"),
		Port:  getEnv("PORT", "8080"),
		DBDSN: getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/restful_api"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
```

**Structured logging — `cmd/api/main.go`:**
```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
slog.SetDefault(logger)
```

**Graceful shutdown — `cmd/api/main.go`:**
```go
srv := &http.Server{Addr: ":" + cfg.Port, Handler: handlerChain}

go func() {
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen: %v", err)
	}
}()

ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
<-ctx.Done()

shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
srv.Shutdown(shutdownCtx)
```

**Health/readiness — register alongside the product routes:**
```go
mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})
mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
	if err := pool.Ping(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
})
```

**`Dockerfile` (multi-stage):**
```dockerfile
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app ./cmd/api

FROM gcr.io/distroless/static-debian12
COPY --from=build /app /app
ENTRYPOINT ["/app"]
```

**`docker-compose.yml`**
```yaml
services:
  db:
    image: postgres:16
    environment:
      POSTGRES_DB: restful_api
      POSTGRES_PASSWORD: postgres
    ports: ["5432:5432"]
  app:
    build: .
    depends_on: [db]
    environment:
      APP_ENV: prod
      DATABASE_URL: postgres://postgres:postgres@db:5432/restful_api
    ports: ["8080:8080"]
```

**`.github/workflows/ci.yml`**
```yaml
name: CI
on: [push, pull_request]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.25" }
      - run: go build ./...
      - run: go vet ./...
      - run: go test ./... -cover
```

---

## Phase 11 — Stretch Goals

No dummy code here on purpose — these are optional and very implementation-specific (rate limiting knobs, tracing setup, HATEOAS link shape, Kafka topics, module layout). When you're ready to pick one, ask and we'll write it against the real state of the project at that point.

---

### How to use this file
Copy each block in as you reach that phase in `todos.md`, run the app, hit the endpoint, confirm it behaves as described, *then* move on. If something doesn't compile or doesn't match your actual files (package names, existing methods), stop and ask rather than guessing — this is dummy/reference code, not a diff against your real project.
