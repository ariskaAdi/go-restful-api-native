package main

import (
	"github.com/ariskaAdi/go-restful-api-native/internal/db"
	"github.com/ariskaAdi/go-restful-api-native/internal/product"
	"context"
	"log"
	"net/http"
)

func main() {

	ctx := context.Background()

	pool, err := db.NewPool(ctx, "postgres://postgres:postgres@localhost:5432/restful_api")
	if err != nil {
		log.Fatalf("db: %w", err)
	}

	defer pool.Close()

	repo := product.NewRepository(pool)
	handler := product.NewHandler(repo)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/prodcts", handler.GetAll)

	log.Panicln("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
