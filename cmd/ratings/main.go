// Command ratings fetches average ratings from FantLab into data/ratings.json.
//
//	go run ./cmd/ratings            # (re)fetch every book
//	go run ./cmd/ratings -missing   # only books without a rating yet
//
// A book that is not found by title can be pinned by hand with `fantlab: <work id>`
// in data/books.yaml (the number from https://fantlab.ru/work<id>).
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
		out      = flag.String("out", "data/ratings.json", "ratings snapshot")
		missing  = flag.Bool("missing", false, "only fetch books that have no entry yet")
		delay    = flag.Duration("delay", 400*time.Millisecond, "pause between requests")
	)
	flag.Parse()

	cat, err := catalog.Load(*dataPath)
	if err != nil {
		log.Fatal(err)
	}
	snap, err := fantlab.Load(*out)
	if err != nil {
		log.Fatal(err)
	}
	client := fantlab.NewClient()

	var notFound []string
	for _, b := range cat.Books() {
		if _, ok := snap.Books[b.ID]; ok && *missing {
			continue
		}
		w, ok, err := lookup(client, b)
		time.Sleep(*delay)
		if err != nil {
			fmt.Printf("  !!  %-45s %v\n", b.Title, err)
			continue
		}
		if !ok {
			fmt.Printf("  --  %-45s not found on FantLab\n", b.Title)
			notFound = append(notFound, b.Title)
			delete(snap.Books, b.ID)
			continue
		}
		snap.Books[b.ID] = fantlab.Entry{Work: w.ID, Rating: round2(w.Mark), Votes: w.Votes}
		fmt.Printf("  ok  %-45s %.2f (%d) work%d %s\n", b.Title, w.Mark, w.Votes, w.ID, w.Kind)
	}

	snap.Updated = time.Now().Format("2006-01-02")
	if err := snap.Save(*out); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nsaved %d ratings -> %s; not found: %d\n", len(snap.Books), *out, len(notFound))
}

func lookup(c *fantlab.Client, b *catalog.Book) (fantlab.Work, bool, error) {
	if b.FantLab != 0 {
		w, err := c.WorkByID(b.FantLab)
		return w, err == nil, err
	}
	for _, q := range []string{b.TitleRU, b.Title} {
		if q == "" {
			continue
		}
		hits, err := c.Search(q)
		if err != nil {
			return fantlab.Work{}, false, err
		}
		if w, ok := fantlab.Match(b.TitleRU, b.Title, b.Author, hits); ok {
			return w, true, nil
		}
	}
	return fantlab.Work{}, false, nil
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }
