// Command translations records, for every book found on FantLab, whether it exists
// in Russian: an official edition, only samizdat or a fan translation, or not at all.
// The snapshot goes to data/translations.json.
//
//	go run ./cmd/translations            # (re)fetch every book
//	go run ./cmd/translations -missing   # only books without an entry yet
//
// It reads the FantLab work ids from data/ratings.json (run cmd/ratings first).
// For books FantLab does not know, or where it is wrong, set `translation:` by hand
// in data/books.yaml; that always wins over the snapshot.
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"wahbook/internal/catalog"
	"wahbook/internal/fantlab"
)

func main() {
	var (
		dataPath = flag.String("data", "data/books.yaml", "catalog file")
		ratings  = flag.String("ratings", "data/ratings.json", "FantLab snapshot with work ids")
		out      = flag.String("out", "data/translations.json", "translations snapshot")
		missing  = flag.Bool("missing", false, "only fetch books that have no entry yet")
		delay    = flag.Duration("delay", 400*time.Millisecond, "pause between requests")
	)
	flag.Parse()

	cat, err := catalog.Load(*dataPath)
	if err != nil {
		log.Fatal(err)
	}
	works, err := fantlab.Load(*ratings)
	if err != nil {
		log.Fatal(err)
	}
	snap, err := fantlab.LoadTranslations(*out)
	if err != nil {
		log.Fatal(err)
	}
	client := fantlab.NewClient()

	counts := map[string]int{}
	var skipped []string
	for _, b := range cat.Books() {
		entry, ok := works.Entry(b.ID)
		if !ok {
			skipped = append(skipped, b.Title)
			continue
		}
		if _, have := snap.Books[b.ID]; have && *missing {
			continue
		}
		t, err := client.Translation(entry.Work)
		time.Sleep(*delay)
		if err != nil {
			fmt.Printf("  !!  %-45s %v\n", b.Title, err)
			continue
		}
		snap.Books[b.ID] = t
		counts[t.Status]++
		fmt.Printf("  ok  %-45s %-9s %s %d\n", b.Title, t.Status, t.Publisher, t.Year)
	}

	snap.Updated = time.Now().Format("2006-01-02")
	if err := snap.Save(*out); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nsaved %d records -> %s: %v\nnot on FantLab (set `translation:` by hand if needed): %v\n", len(snap.Books), *out, counts, skipped)
}
