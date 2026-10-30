// Command contents records, for every anthology and collection found on FantLab, the works
// it consists of (title, author, kind and readers' rating). The snapshot goes to
// data/contents.json.
//
//	go run ./cmd/contents            # (re)fetch every anthology and collection
//	go run ./cmd/contents -missing   # only books without an entry yet
//
// It reads the FantLab work ids from data/ratings.json (run cmd/ratings first).
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
		out      = flag.String("out", "data/contents.json", "contents snapshot")
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
	snap, err := fantlab.LoadContents(*out)
	if err != nil {
		log.Fatal(err)
	}
	client := fantlab.NewClient()

	var skipped []string
	for _, b := range cat.Books() {
		if b.Kind != catalog.KindAnthology && b.Kind != catalog.KindCollection {
			continue
		}
		entry, ok := works.Entry(b.ID)
		if !ok {
			skipped = append(skipped, b.Title)
			continue
		}
		if _, have := snap.Books[b.ID]; have && *missing {
			continue
		}
		stories, err := client.Contents(entry.Work)
		time.Sleep(*delay)
		if err != nil {
			fmt.Printf("  !!  %-45s %v\n", b.Title, err)
			continue
		}
		if len(stories) == 0 {
			fmt.Printf("  --  %-45s FantLab lists no works\n", b.Title)
			delete(snap.Books, b.ID)
			continue
		}
		snap.Books[b.ID] = stories
		fmt.Printf("  ok  %-45s %d works\n", b.Title, len(stories))
	}

	snap.Updated = time.Now().Format("2006-01-02")
	if err := snap.Save(*out); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nsaved %d books -> %s\nnot on FantLab: %v\n", len(snap.Books), *out, skipped)
}
