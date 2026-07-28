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

	pool, err := db.NewPool(ctx, "postgres://postgres:postgres@localhost:5432/go-restful")
	if err != nil {
		log.Fatalf("db: %w", err)
	}

	defer pool.Close()

	repo := product.NewRepository(pool)
	svc := product.NewService(repo)
	handler := product.NewHandler(svc)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/products", handler.GetAll)

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
