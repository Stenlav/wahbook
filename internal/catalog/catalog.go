// Package catalog loads and validates the book catalog (data/books.yaml).
package catalog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type Kind string

const (
	KindNovel      Kind = "novel"
	KindNovella    Kind = "novella"
	KindAnthology  Kind = "anthology"  // collection by several authors
	KindCollection Kind = "collection" // collection of novellas by one author
	KindAudio      Kind = "audio"
	KindArtbook    Kind = "artbook" // illustrated book
)

var kindLabels = map[Kind]string{
	KindNovel:      "Роман",
	KindNovella:    "Новелла",
	KindAnthology:  "Антология",
	KindCollection: "Сборник новелл",
	KindAudio:      "Аудиодрама",
	KindArtbook:    "Артбук",
}

// Label returns the Russian display name of the kind.
func (k Kind) Label() string { return kindLabels[k] }

type Series struct {
	ID          string `yaml:"id" json:"id"`
	Title       string `yaml:"title" json:"title"`
	TitleEN     string `yaml:"title_en" json:"titleEn"`
	Description string `yaml:"description" json:"description"`

	Books []*Book `yaml:"-" json:"books"`
}

type Book struct {
	ID        string `yaml:"id" json:"id"`
	Series    string `yaml:"series" json:"series"`
	Number    int    `yaml:"n" json:"number,omitempty"`
	HHNumber  int    `yaml:"hh" json:"hhNumber,omitempty"`
	Title     string `yaml:"title" json:"title"`
	TitleRU   string `yaml:"title_ru" json:"titleRu,omitempty"`
	Author    string `yaml:"author" json:"author"`
	Editors   string `yaml:"editors" json:"editors,omitempty"`
	Year      int    `yaml:"year" json:"year"`
	Summary   string `yaml:"-" json:"summary,omitempty"`   // from annotations.yaml
	AuthorRU  string `yaml:"-" json:"authorRu,omitempty"`  // from authors.yaml
	EditorsRU string `yaml:"-" json:"editorsRu,omitempty"` // from authors.yaml
	Wiki      string `yaml:"wiki" json:"-"`                // page on the Russian wiki, "Page#Section"
	FantLab   int    `yaml:"fantlab" json:"-"`             // FantLab work id; overrides the title search
	Kind      Kind   `yaml:"kind" json:"kind"`
}

// IsAnthology reports whether the book is a multi-author collection.
func (b *Book) IsAnthology() bool { return b.Author == "Various" }

// Person is someone a book is credited to: its author, or an anthology's editor.
type Person struct {
	Name string // as in books.yaml; also the key of the author filter
	RU   string // Russian spelling, or Name when unknown
}

// AuthorNames splits the author field: a book may have several authors, separated by commas.
func (b *Book) AuthorNames() []string {
	if b.IsAnthology() {
		return nil
	}
	return splitNames(b.Author)
}

func splitNames(s string) []string {
	var out []string
	for _, n := range strings.Split(s, ",") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// People lists who the book is credited to: its authors, or the editors of an anthology.
func (b *Book) People() []Person {
	names, ruJoined := b.AuthorNames(), b.AuthorRU
	if b.IsAnthology() {
		names, ruJoined = splitNames(b.Editors), b.EditorsRU
	}
	ru := splitNames(ruJoined)
	out := make([]Person, 0, len(names))
	for i, name := range names {
		p := Person{Name: name, RU: name}
		if len(ru) == len(names) && ru[i] != "" {
			p.RU = ru[i]
		}
		out = append(out, p)
	}
	return out
}

// PeopleKeys is People as one string for a data attribute: names joined by "|".
func (b *Book) PeopleKeys() string {
	var names []string
	for _, p := range b.People() {
		names = append(names, p.Name)
	}
	return strings.Join(names, "|")
}

// AuthorLabel is the author as shown to the reader, in Russian.
func (b *Book) AuthorLabel() string {
	if b.IsAnthology() {
		if b.EditorsRU != "" {
			return "Антология · ред. " + b.EditorsRU
		}
		return "Антология"
	}
	if b.AuthorRU != "" {
		return b.AuthorRU
	}
	return b.Author
}

// AuthorOrig is the same line in the original spelling, shown under the Russian
// one; empty when there is nothing to add.
func (b *Book) AuthorOrig() string {
	if b.IsAnthology() {
		return b.Editors
	}
	if b.AuthorRU == "" || b.AuthorRU == b.Author {
		return ""
	}
	return b.Author
}

type Catalog struct {
	Series []*Series `json:"series"`

	byID map[string]*Book
}

// Book returns a book by id.
func (c *Catalog) Book(id string) (*Book, bool) {
	b, ok := c.byID[id]
	return b, ok
}

// Books returns every book in display order (series by series).
func (c *Catalog) Books() []*Book {
	var all []*Book
	for _, s := range c.Series {
		all = append(all, s.Books...)
	}
	return all
}

type file struct {
	Series []*Series `yaml:"series"`
	Books  []*Book   `yaml:"books"`
}

// Load reads and validates a catalog file. Russian author names and annotations are
// read from authors.yaml, annotations.yaml and annotations_extra.yaml next to it
// (each optional).
func Load(path string) (*Catalog, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(path)
	if raw, err := readOptional(filepath.Join(dir, "authors.yaml")); err != nil {
		return nil, err
	} else if raw != nil {
		if err := c.ApplyAuthors(raw); err != nil {
			return nil, err
		}
	}
	// annotations.yaml is generated from FantLab; annotations_extra.yaml is written by hand
	// for books FantLab does not have, and wins over the generated file.
	for _, name := range []string{"annotations.yaml", "annotations_extra.yaml"} {
		if raw, err := readOptional(filepath.Join(dir, name)); err != nil {
			return nil, err
		} else if raw != nil {
			if err := c.ApplyAnnotations(raw); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	return c, nil
}

// readOptional returns nil, nil for a file that does not exist.
func readOptional(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return raw, err
}

// ApplyAuthors fills in the Russian spelling of author and editor names
// (name as in books.yaml → Russian name). Every name used by a book must be
// listed, so that a new author cannot silently appear untranslated.
func (c *Catalog) ApplyAuthors(raw []byte) error {
	var names map[string]string
	if err := yaml.Unmarshal(raw, &names); err != nil {
		return fmt.Errorf("authors: %w", err)
	}
	ru := func(book, name string) (string, error) {
		v, ok := names[name]
		if !ok {
			return "", fmt.Errorf("authors: %q (%s) has no Russian name", name, book)
		}
		return v, nil
	}
	for _, b := range c.byID {
		var authors []string
		for _, a := range b.AuthorNames() {
			v, err := ru(b.Title, a)
			if err != nil {
				return err
			}
			authors = append(authors, v)
		}
		b.AuthorRU = strings.Join(authors, ", ")
		var editors []string
		for _, e := range splitNames(b.Editors) {
			v, err := ru(b.Title, e)
			if err != nil {
				return err
			}
			editors = append(editors, v)
		}
		b.EditorsRU = strings.Join(editors, ", ")
	}
	return nil
}

// ApplyAnnotations attaches summaries (book id → text) to the books. An id
// that matches no book is an error: it is almost always a typo.
func (c *Catalog) ApplyAnnotations(raw []byte) error {
	var notes map[string]string
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	if err := dec.Decode(&notes); err != nil {
		return fmt.Errorf("annotations: %w", err)
	}
	for id, text := range notes {
		b, ok := c.byID[id]
		if !ok {
			return fmt.Errorf("annotations: unknown book id %q", id)
		}
		b.Summary = strings.TrimSpace(text)
	}
	return nil
}

// Parse validates a catalog from YAML bytes.
func Parse(raw []byte) (*Catalog, error) {
	var f file
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}

	c := &Catalog{Series: f.Series, byID: map[string]*Book{}}
	seriesByID := map[string]*Series{}
	for _, s := range f.Series {
		if s.ID == "" || s.Title == "" {
			return nil, fmt.Errorf("catalog: series needs id and title: %+v", s)
		}
		if _, dup := seriesByID[s.ID]; dup {
			return nil, fmt.Errorf("catalog: duplicate series %q", s.ID)
		}
		seriesByID[s.ID] = s
	}

	for _, b := range f.Books {
		if b.Title == "" || b.Author == "" || b.Year == 0 {
			return nil, fmt.Errorf("catalog: book needs title, author and year: %+v", b)
		}
		if _, ok := kindLabels[b.Kind]; !ok {
			return nil, fmt.Errorf("catalog: %q: unknown kind %q", b.Title, b.Kind)
		}
		s, ok := seriesByID[b.Series]
		if !ok {
			return nil, fmt.Errorf("catalog: %q: unknown series %q", b.Title, b.Series)
		}
		if b.ID == "" {
			b.ID = Slug(b.Title)
		}
		if _, dup := c.byID[b.ID]; dup {
			return nil, fmt.Errorf("catalog: duplicate book id %q", b.ID)
		}
		c.byID[b.ID] = b
		s.Books = append(s.Books, b)
	}
	return c, nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns a title into a URL- and file-safe identifier.
func Slug(title string) string {
	s := nonSlug.ReplaceAllString(strings.ToLower(title), "-")
	return strings.Trim(s, "-")
}
