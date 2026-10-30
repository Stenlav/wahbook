// Command site builds the static site into dist/.
//
//	go run ./cmd/site                                  # local preview build
//	go run ./cmd/site -base /wahbook/                  # for https://user.github.io/wahbook/
//	go run ./cmd/site -api https://api.example.com    # enable accounts
//	go run ./cmd/site -serve :8000                     # build, then serve dist/ locally
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"wahbook/internal/achievements"
	"wahbook/internal/catalog"
	"wahbook/internal/fantlab"
	"wahbook/internal/site"
)

func main() {
	var (
		dataPath = flag.String("data", "data/books.yaml", "catalog file")
		achieve  = flag.String("achievements", "data/achievements.yaml", "achievement rules; a missing file switches achievements off")
		ratings  = flag.String("ratings", "data/ratings.json", "FantLab ratings snapshot (see cmd/ratings)")
		covers   = flag.String("covers", "assets/covers", "directory with <book-id>.jpg covers")
		out      = flag.String("out", "dist", "output directory")
		base     = flag.String("base", "/", "URL path the site is served under")
		api      = flag.String("api", os.Getenv("WAHBOOK_API"), "backend URL; empty disables accounts")
		serve    = flag.String("serve", "", "after building, serve the output on this address (e.g. :8000)")
	)
	flag.Parse()

	cat, err := catalog.Load(*dataPath)
	if err != nil {
		log.Fatal(err)
	}
	snap, err := fantlab.Load(*ratings)
	if err != nil {
		log.Fatal(err)
	}
	var rules *achievements.Config
	if _, err := os.Stat(*achieve); err == nil {
		if rules, err = achievements.Load(*achieve); err != nil {
			log.Fatal(err)
		}
	}
	if err := site.Build(site.Config{
		Ratings:      snap,
		Achievements: rules,
		Catalog:      cat,
		OutDir:       *out,
		CoversDir:    *covers,
		BasePath:     *base,
		APIBase:      *api,
	}); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("built %d books in %d series -> %s\n", len(cat.Books()), len(cat.Series), *out)

	if *serve != "" {
		prefix := "/" + strings.Trim(*base, "/")
		fmt.Printf("serving %s on http://localhost%s%s\n", *out, *serve, strings.TrimSuffix(prefix, "/")+"/")
		fs := http.FileServer(http.Dir(*out))
		if prefix != "/" {
			http.Handle(prefix+"/", http.StripPrefix(prefix, fs))
		} else {
			http.Handle("/", fs)
		}
		log.Fatal(http.ListenAndServe(*serve, nil))
	}
}
