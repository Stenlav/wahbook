// Command annotations downloads the official book annotations from FantLab
// into data/annotations.yaml, word for word.
//
//	go run ./cmd/annotations
//
// It reads the FantLab work ids from data/ratings.json (run cmd/ratings first),
// so a book gets an annotation exactly when it has been matched on FantLab.
// The only change made to the text is line-ending normalisation (\r\n → \n)
// and trimming of surrounding whitespace.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"wahbook/internal/catalog"
	"wahbook/internal/fantlab"
)

const header = `Аннотации к книгам: id книги → текст. Файл создаёт ` + "`go run ./cmd/annotations`" + `
из описаний произведений на FantLab (https://fantlab.ru) дословно; руками его лучше не править,
иначе правки пропадут при следующем обновлении. Свои тексты — в annotations_extra.yaml.
Показываются в карточке книги.`

func main() {
	var (
		dataPath = flag.String("data", "data/books.yaml", "catalog file")
		ratings  = flag.String("ratings", "data/ratings.json", "FantLab snapshot with work ids")
		out      = flag.String("out", "data/annotations.yaml", "output file")
		delay    = flag.Duration("delay", 400*time.Millisecond, "pause between requests")
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
	client := fantlab.NewClient()

	doc := &yaml.Node{Kind: yaml.MappingNode, HeadComment: header}
	var none []string
	for _, b := range cat.Books() {
		entry, ok := snap.Entry(b.ID)
		if !ok {
			none = append(none, b.Title+" (not on FantLab)")
			continue
		}
		w, err := client.WorkByID(entry.Work)
		time.Sleep(*delay)
		if err != nil {
			log.Fatalf("%s: %v", b.Title, err)
		}
		text := clean(w.Description)
		if text == "" {
			none = append(none, b.Title+" (empty annotation)")
			continue
		}
		doc.Content = append(doc.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: b.ID, LineComment: ""},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: text})
		fmt.Printf("  ok  %-45s %d chars\n", b.Title, len([]rune(text)))
	}

	raw, err := yaml.Marshal(doc)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, raw, 0o644); err != nil {
		log.Fatal(err)
	}

	// Read the file back through the catalog: what is on disk must equal what was fetched.
	check, err := catalog.Load(*dataPath)
	if err != nil {
		log.Fatalf("written file does not load: %v", err)
	}
	n := 0
	for i := 0; i < len(doc.Content); i += 2 {
		b, _ := check.Book(doc.Content[i].Value)
		if b == nil || b.Summary != doc.Content[i+1].Value {
			log.Fatalf("round-trip mismatch for %s", doc.Content[i].Value)
		}
		n++
	}
	fmt.Printf("\nsaved %d annotations -> %s (verified); without annotation: %v\n", n, *out, none)
}

// clean normalises line endings and trims the ends; the wording is untouched.
func clean(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimSpace(s)
}
