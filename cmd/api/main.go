// Command api runs the accounts backend (registration, login, reading progress).
//
// The static site on GitHub Pages works without it; build the site with
// -api <this server's URL> to switch accounts on.
//
//	ADDR=:8080 DATA_FILE=data/state.json ALLOWED_ORIGIN=https://user.github.io go run ./cmd/api
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"wahbook/internal/api"
	"wahbook/internal/auth"
	"wahbook/internal/catalog"
)

func main() {
	addr := env("ADDR", ":8080")
	dataFile := env("DATA_FILE", "data/state.json")
	origin := env("ALLOWED_ORIGIN", "http://localhost:8000")
	catalogPath := env("CATALOG", "data/books.yaml")

	cat, err := catalog.Load(catalogPath)
	if err != nil {
		log.Fatal(err)
	}
	store, err := auth.NewFileStore(dataFile)
	if err != nil {
		log.Fatal(err)
	}
	svc, err := auth.NewService(store)
	if err != nil {
		log.Fatal(err)
	}

	srv := &http.Server{
		Addr: addr,
		Handler: api.New(api.Config{
			Auth:          svc,
			AllowedOrigin: origin,
			KnownBook:     func(id string) bool { _, ok := cat.Book(id); return ok },
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("api listening on %s (allowed origin %s, data %s)", addr, origin, dataFile)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
