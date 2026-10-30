// Command covers downloads book covers from Open Library into assets/covers.
//
//	go run ./cmd/covers            # fetch only missing covers
//	go run ./cmd/covers -force     # re-download everything
//
// Books without a cover keep a generated placeholder on the site. A cover can
// always be added by hand: drop assets/covers/<book-id>.jpg in place.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"wahbook/internal/catalog"
)

var client = &http.Client{Timeout: 30 * time.Second}

func main() {
	dataPath := flag.String("data", "data/books.yaml", "catalog file")
	outDir := flag.String("out", "assets/covers", "output directory")
	force := flag.Bool("force", false, "re-download existing covers")
	flag.Parse()

	cat, err := catalog.Load(*dataPath)
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatal(err)
	}

	var missing []string
	for _, b := range cat.Books() {
		dst := filepath.Join(*outDir, b.ID+".jpg")
		if _, err := os.Stat(dst); err == nil && !*force {
			continue
		}
		coverID, err := findCover(b)
		if err != nil || coverID == 0 {
			fmt.Printf("  --  %-45s no cover found (%v)\n", b.Title, err)
			missing = append(missing, b.Title)
			continue
		}
		if err := download(fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-L.jpg", coverID), dst); err != nil {
			fmt.Printf("  !!  %-45s %v\n", b.Title, err)
			missing = append(missing, b.Title)
			continue
		}
		fmt.Printf("  ok  %-45s cover %d\n", b.Title, coverID)
		time.Sleep(300 * time.Millisecond) // be polite to Open Library
	}
	fmt.Printf("\nмissing covers: %d\n", len(missing))
}

type searchResult struct {
	Docs []struct {
		Title      string   `json:"title"`
		CoverI     int      `json:"cover_i"`
		AuthorName []string `json:"author_name"`
	} `json:"docs"`
}

// findCover looks the book up by title (and author, for single-author books)
// and returns the first result whose title matches closely enough.
func findCover(b *catalog.Book) (int, error) {
	q := url.Values{}
	q.Set("title", b.Title)
	if !b.IsAnthology() {
		q.Set("author", lastName(b.Author))
	}
	q.Set("fields", "title,cover_i,author_name")
	q.Set("limit", "10")

	var res searchResult
	if err := getJSON("https://openlibrary.org/search.json?"+q.Encode(), &res); err != nil {
		return 0, err
	}
	want := norm(b.Title)
	for _, d := range res.Docs {
		if d.CoverI == 0 {
			continue
		}
		got := norm(d.Title)
		if got == want || strings.HasPrefix(got, want) || strings.HasPrefix(want, got) && len(got) > 8 {
			return d.CoverI, nil
		}
	}
	return 0, nil
}

func lastName(full string) string {
	f := strings.Fields(full)
	return f[len(f)-1]
}

// norm lowercases and strips everything but letters and digits.
func norm(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func getJSON(u string, v any) error {
	resp, err := client.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func download(u, dst string) error {
	resp, err := client.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
		return fmt.Errorf("unexpected content type %q", ct)
	}
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, 10<<20))
	f.Close()
	if err != nil || n < 2048 { // Open Library serves a 1px gif for unknown covers
		os.Remove(tmp)
		return fmt.Errorf("bad image (%d bytes, %v)", n, err)
	}
	return os.Rename(tmp, dst)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
