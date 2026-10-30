// Package fantlab looks books up on FantLab (https://fantlab.ru) through its
// public API and stores a snapshot of their ratings for the static site.
package fantlab

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"
)

const DefaultBaseURL = "https://api.fantlab.ru"

// WorkURL is the human page of a work.
func WorkURL(id int) string { return fmt.Sprintf("https://fantlab.ru/work%d", id) }

// SearchURL is a FantLab search page, used when a book was not matched.
func SearchURL(q string) string {
	return "https://fantlab.ru/searchmain?searchstr=" + url.QueryEscape(q)
}

type Client struct {
	HTTP    *http.Client
	BaseURL string
}

func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 30 * time.Second}, BaseURL: DefaultBaseURL}
}

// Work is the part of a FantLab work we care about.
type Work struct {
	ID      int
	RusName string
	Name    string
	Authors string  // all_autor_name: Latin author names, comma-separated
	Votes   int     // number of ratings
	Mark    float64 // average rating, 0..10
	Kind    string  // "роман", "сборник", …

	Description string // the annotation, as FantLab publishes it (only set by WorkByID)
}

type searchHit struct {
	WorkID  int       `json:"work_id"`
	RusName string    `json:"rusname"`
	Name    string    `json:"name"`
	Authors string    `json:"all_autor_name"`
	Count   int       `json:"markcount"`
	MidMark []float64 `json:"midmark"`
	Kind    string    `json:"name_show_im"`
}

// Search runs a work search and returns the hits that are actual works.
func (c *Client) Search(q string) ([]Work, error) {
	var hits []searchHit
	u := fmt.Sprintf("%s/search-works?q=%s&page=1&onlymatches=1", c.BaseURL, url.QueryEscape(q))
	if err := c.getJSON(u, &hits); err != nil {
		return nil, err
	}
	var out []Work
	for _, h := range hits {
		if h.WorkID == 0 {
			continue
		}
		w := Work{ID: h.WorkID, RusName: h.RusName, Name: h.Name, Authors: h.Authors, Votes: h.Count, Kind: h.Kind}
		if len(h.MidMark) > 0 {
			w.Mark = h.MidMark[0]
		}
		out = append(out, w)
	}
	return out, nil
}

// WorkByID fetches one work (used for manual overrides).
func (c *Client) WorkByID(id int) (Work, error) {
	var r struct {
		Title       string      `json:"title"`
		Voters      int         `json:"val_voters"`
		MidMark     json.Number `json:"val_midmark"` // a string or a number, depending on the work
		Description string      `json:"work_description"`
	}
	if err := c.getJSON(fmt.Sprintf("%s/work/%d", c.BaseURL, id), &r); err != nil {
		return Work{}, err
	}
	w := Work{ID: id, RusName: r.Title, Votes: r.Voters, Description: r.Description}
	w.Mark, _ = r.MidMark.Float64()
	return w, nil
}

func (c *Client) getJSON(u string, v any) error {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "wahbook/1.0 (fan catalog; one-off rating snapshot)")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fantlab: HTTP %d for %s", resp.StatusCode, u)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// Match picks the work that is the given book: the title (Russian or
// original) must match exactly after normalisation, and for single-author
// books the author's surname must appear among FantLab's authors. When
// several works qualify (a collection and a story inside it share a name),
// the one with the most votes wins.
func Match(titleRU, title, author string, hits []Work) (Work, bool) {
	var best Work
	found := false
	for _, w := range hits {
		if !(sameTitle(w.RusName, titleRU) || sameTitle(w.Name, title)) {
			continue
		}
		if author != "Various" && !hasAnyAuthor(w.Authors, author) {
			continue
		}
		if !found || w.Votes > best.Votes {
			best, found = w, true
		}
	}
	return best, found
}

// hasAnyAuthor reports whether FantLab's authors include any of the book's authors
// (the book may have several, separated by commas), compared by surname.
func hasAnyAuthor(fantlabAuthors, bookAuthors string) bool {
	have := strings.ToLower(fantlabAuthors)
	for _, a := range strings.Split(bookAuthors, ",") {
		if a = strings.TrimSpace(a); a != "" && strings.Contains(have, strings.ToLower(surname(a))) {
			return true
		}
	}
	return false
}

func sameTitle(a, b string) bool { return a != "" && b != "" && norm(a) == norm(b) }

func surname(full string) string {
	f := strings.Fields(full)
	return f[len(f)-1]
}

// norm lowercases and keeps only letters and digits; ё and е are the same.
func norm(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		if r == 'ё' {
			r = 'е'
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// ── snapshot file ───────────────────────────────────────────────────────────

type Entry struct {
	Work   int     `json:"work"`
	Rating float64 `json:"rating"`
	Votes  int     `json:"votes"`
}

// File is data/ratings.json: a snapshot, refreshed by `go run ./cmd/ratings`.
type File struct {
	Updated string           `json:"updated"` // YYYY-MM-DD
	Books   map[string]Entry `json:"books"`   // by book id
}

// Load reads a snapshot; a missing file is an empty snapshot.
func Load(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &File{Books: map[string]Entry{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	if f.Books == nil {
		f.Books = map[string]Entry{}
	}
	return &f, nil
}

// Entry returns the rating entry of a book; safe to call on a nil snapshot.
func (f *File) Entry(bookID string) (Entry, bool) {
	if f == nil {
		return Entry{}, false
	}
	e, ok := f.Books[bookID]
	return e, ok
}

func (f *File) Save(path string) error {
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}
