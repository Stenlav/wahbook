// Package site renders the catalog into a static site (plain HTML/CSS/JS)
// suitable for GitHub Pages.
package site

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"wahbook/internal/achievements"
	"wahbook/internal/catalog"
	"wahbook/internal/fantlab"
	"wahbook/web"
)

// Config controls a site build.
type Config struct {
	Catalog   *catalog.Catalog
	OutDir    string        // where the site is written, e.g. "dist"
	CoversDir string        // downloaded covers, e.g. "assets/covers"
	BasePath  string        // URL prefix the site is served under: "/" or "/repo/"
	APIBase   string        // backend URL for accounts; empty = no accounts, progress stays in the browser
	Ratings   *fantlab.File // FantLab snapshot; nil = no ratings shown

	Achievements *achievements.Config      // nil = no achievements
	Translations *fantlab.TranslationsFile // Russian editions; nil = unknown for every book without an override
	Contents     *fantlab.ContentsFile     // what each anthology consists of; nil = no contents shown
}

// minVotes is how many FantLab ratings a book needs before its average is
// shown: one enthusiastic vote must not look like a 10/10 book.
const minVotes = 5

type pageBook struct {
	*catalog.Book
	CoverURL   string // empty when no cover file exists
	Initials   string
	Rating     string  // "7.84", empty when there are too few votes
	mark       float64 // the same rating as a number; 0 = not rated
	Votes      int
	FantLabURL string // the work page, or a search when the book is not on FantLab
	TrStatus   string // published | fan | none; empty when unknown
	TrLabel    string // short text for the card
	TrNote     string // longer text for the tooltip and the book dialog
}

type authorOption struct {
	Key   string // the name as in books.yaml
	Label string // "Дэн Абнетт (9)"
}

type pageSeries struct {
	*catalog.Series
	Books []pageBook

	// Series rating: the plain mean of the ratings of its rated books.
	Rating string // "7.78", empty when no book of the series is rated
	Rated  int    // how many books went into the mean
	Total  int    // how many books the series has
}

type pageData struct {
	BasePath    string
	APIBase     string
	Series      []pageSeries
	TotalBooks  int
	RatingsDate string // "30.10.2026", empty without ratings

	Authors []authorOption // for the author filter

	ContentsJSON template.JS // stories of anthologies for the book dialog; empty when there are none

	AchievementsJSON template.JS // resolved rules for the browser, empty without achievements
}

// Build writes the whole site into cfg.OutDir.
func Build(cfg Config) error {
	cfg.BasePath = normalizeBase(cfg.BasePath)

	if err := os.RemoveAll(cfg.OutDir); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.OutDir, 0o755); err != nil {
		return err
	}

	data := pageData{BasePath: cfg.BasePath, APIBase: strings.TrimRight(cfg.APIBase, "/")}
	for _, s := range cfg.Catalog.Series {
		ps := pageSeries{Series: s}
		for _, b := range s.Books {
			pb := pageBook{Book: b, Initials: initials(b.Title)}
			pb.FantLabURL = fantlab.SearchURL(b.Title)
			if e, ok := cfg.Ratings.Entry(b.ID); ok {
				pb.FantLabURL = fantlab.WorkURL(e.Work)
				if e.Votes >= minVotes {
					pb.Rating, pb.Votes, pb.mark = fmt.Sprintf("%.2f", e.Rating), e.Votes, e.Rating
				}
			}
			pb.TrStatus, pb.TrLabel, pb.TrNote = translationText(b, cfg.Translations)
			if _, err := os.Stat(filepath.Join(cfg.CoversDir, b.ID+".jpg")); err == nil {
				pb.CoverURL = cfg.BasePath + "covers/" + b.ID + ".jpg"
			}
			ps.Books = append(ps.Books, pb)
		}
		ps.Total = len(ps.Books)
		var sum float64
		for _, pb := range ps.Books {
			if pb.mark > 0 {
				sum += pb.mark
				ps.Rated++
			}
		}
		if ps.Rated > 0 {
			ps.Rating = fmt.Sprintf("%.2f", sum/float64(ps.Rated))
		}
		data.Series = append(data.Series, ps)
		data.TotalBooks += len(ps.Books)
	}

	data.Authors = authorOptions(cfg.Catalog.Books())

	if js, err := contentsJSON(cfg.Catalog, cfg.Contents); err != nil {
		return err
	} else {
		data.ContentsJSON = js
	}

	if cfg.Achievements != nil {
		page, err := cfg.Achievements.Resolve(achievements.Inputs{Catalog: cfg.Catalog, Ratings: cfg.Ratings, MinVotes: minVotes})
		if err != nil {
			return err
		}
		raw, err := json.Marshal(page) // escapes <, > and &, so it is safe inside <script>
		if err != nil {
			return err
		}
		data.AchievementsJSON = template.JS(raw)
	}

	if cfg.Ratings != nil && cfg.Ratings.Updated != "" {
		if t, err := time.Parse("2006-01-02", cfg.Ratings.Updated); err == nil {
			data.RatingsDate = t.Format("02.01.2006")
		}
	}

	tpl, err := template.New("").Funcs(template.FuncMap{
		"kindLabel": func(k catalog.Kind) string { return k.Label() },
	}).ParseFS(web.FS, "templates/*.html")
	if err != nil {
		return err
	}
	for _, page := range []struct{ tmpl, file string }{
		{"index.html", "index.html"},
		{"404.html", "404.html"},
	} {
		var buf bytes.Buffer
		if err := tpl.ExecuteTemplate(&buf, page.tmpl, data); err != nil {
			return fmt.Errorf("render %s: %w", page.tmpl, err)
		}
		if err := os.WriteFile(filepath.Join(cfg.OutDir, page.file), buf.Bytes(), 0o644); err != nil {
			return err
		}
	}

	// Machine-readable catalog: handy for the frontend and any future client.
	js, err := json.MarshalIndent(cfg.Catalog, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(cfg.OutDir, "catalog.json"), js, 0o644); err != nil {
		return err
	}

	static, err := fs.Sub(web.FS, "static")
	if err != nil {
		return err
	}
	if err := copyFS(static, filepath.Join(cfg.OutDir, "static")); err != nil {
		return err
	}
	if _, err := os.Stat(cfg.CoversDir); err == nil {
		if err := copyFS(os.DirFS(cfg.CoversDir), filepath.Join(cfg.OutDir, "covers")); err != nil {
			return err
		}
	}
	// Tell GitHub Pages not to run Jekyll over the output.
	return os.WriteFile(filepath.Join(cfg.OutDir, ".nojekyll"), nil, 0o644)
}

func normalizeBase(b string) string {
	b = "/" + strings.Trim(b, "/")
	if b != "/" {
		b += "/"
	}
	return b
}

// contentsStory is a story as the book dialog shows it.
type contentsStory struct {
	Title   string `json:"title"`
	Orig    string `json:"orig,omitempty"`
	Authors string `json:"authors,omitempty"`
	Kind    string `json:"kind,omitempty"` // only for what is not a plain story (novella, …)
	Year    int    `json:"year,omitempty"`
	Rating  string `json:"rating,omitempty"` // empty when there are too few votes
	Votes   int    `json:"votes,omitempty"`
	URL     string `json:"url"`
}

// contentsJSON turns the snapshot into {bookId: [story, …]} for the page. Stories of books
// that are not in the catalog are ignored; the result is empty without contents.
func contentsJSON(cat *catalog.Catalog, snap *fantlab.ContentsFile) (template.JS, error) {
	out := map[string][]contentsStory{}
	for _, b := range cat.Books() {
		stories, ok := snap.Entry(b.ID)
		if !ok {
			continue
		}
		for _, s := range stories {
			cs := contentsStory{
				Title: s.Title, Orig: s.Orig, Authors: strings.Join(s.Authors, ", "),
				Year: s.Year, URL: fantlab.WorkURL(s.Work),
			}
			if s.Kind != "" && s.Kind != "рассказ" {
				cs.Kind = s.Kind
			}
			if s.Votes >= minVotes {
				cs.Rating, cs.Votes = fmt.Sprintf("%.2f", s.Rating), s.Votes
			}
			out[b.ID] = append(out[b.ID], cs)
		}
	}
	if len(out) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(out) // escapes <, > and &, so it is safe inside <script>
	return template.JS(raw), err
}

// translationText says whether a book exists in Russian, and who translated it when FantLab
// knows. A `translation:` set by hand in books.yaml wins over the snapshot; with neither,
// the status is unknown (empty).
func translationText(b *catalog.Book, snap *fantlab.TranslationsFile) (status, label, note string) {
	t, ok := snap.Entry(b.ID)
	switch b.Translation {
	case "yes":
		t.Translated, ok = true, true
	case "no":
		t, ok = fantlab.Translation{}, true
	}
	switch {
	case !ok:
		return "", "", ""
	case !t.Translated:
		return "no", "Нет перевода", "На русский язык не переведена"
	}
	note = "Есть перевод на русский"
	switch n := len(t.Translators); {
	case n == 1:
		note += ". Переводчик: " + t.Translators[0]
	case n > 1:
		note += ". Переводчики: " + strings.Join(t.Translators, ", ")
	}
	return "yes", "Есть перевод", note
}

// authorOptions lists the people books are credited to (authors, and editors of
// anthologies) with their number of books, sorted by Russian name.
func authorOptions(books []*catalog.Book) []authorOption {
	type entry struct {
		ru string
		n  int
	}
	seen := map[string]*entry{}
	for _, b := range books {
		for _, p := range b.People() {
			if seen[p.Name] == nil {
				seen[p.Name] = &entry{ru: p.RU}
			}
			seen[p.Name].n++
		}
	}
	opts := make([]authorOption, 0, len(seen))
	for name, e := range seen {
		opts = append(opts, authorOption{Key: name, Label: fmt.Sprintf("%s (%d)", e.ru, e.n)})
	}
	sort.Slice(opts, func(i, j int) bool { return opts[i].Label < opts[j].Label })
	return opts
}

// initials is used by the cover placeholder: first letters of up to 3 words.
func initials(title string) string {
	if i := strings.Index(title, ":"); i > 0 {
		title = title[:i]
	}
	var out []rune
	for _, w := range strings.Fields(title) {
		r := []rune(w)
		if len(w) <= 3 && strings.ToLower(w) != "i" && len(out) > 0 { // skip "of", "the", "and"
			continue
		}
		out = append(out, r[0])
		if len(out) == 3 {
			break
		}
	}
	return strings.ToUpper(string(out))
}

func copyFS(src fs.FS, dst string) error {
	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dst, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if strings.HasSuffix(path.Base(p), ".part") {
			return nil
		}
		in, err := src.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}
