package catalog

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLoadRealCatalog(t *testing.T) {
	c, err := Load("../../data/books.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Series) == 0 || len(c.Books()) < 90 {
		t.Fatalf("unexpectedly small catalog: %d series, %d books", len(c.Series), len(c.Books()))
	}
	if b, ok := c.Book("horus-rising"); !ok || b.Number != 1 {
		t.Fatalf("horus-rising missing or misnumbered: %+v", b)
	}
	if b, _ := c.Book("the-end-and-the-death-volume-iii"); b == nil || b.HHNumber != 64 {
		t.Fatalf("expected HH number 64, got %+v", b)
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"unknown series":  "series: [{id: a, title: A}]\nbooks: [{series: b, title: T, author: X, year: 2000, kind: novel}]",
		"unknown kind":    "series: [{id: a, title: A}]\nbooks: [{series: a, title: T, author: X, year: 2000, kind: comic}]",
		"duplicate id":    "series: [{id: a, title: A}]\nbooks: [{series: a, title: T, author: X, year: 2000, kind: novel}, {series: a, title: t, author: X, year: 2000, kind: novel}]",
		"unknown field":   "series: [{id: a, title: A, bogus: 1}]",
		"bad translation": "series: [{id: a, title: A}]\nbooks: [{series: a, title: T, author: X, year: 2000, kind: novel, translation: soon}]",
	}
	for name, src := range cases {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestSlug(t *testing.T) {
	if got := Slug("Lion El'Jonson: Lord of the First"); got != "lion-el-jonson-lord-of-the-first" {
		t.Fatalf("got %q", got)
	}
}

// Annotations come from FantLab (cmd/annotations); a few books have none there.
func TestAnnotationsLoaded(t *testing.T) {
	c, err := Load("../../data/books.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for _, b := range c.Books() {
		switch n := utf8.RuneCountInString(b.Summary); {
		case n == 0:
			missing = append(missing, b.ID)
		case n < 20:
			t.Errorf("%s: annotation is suspiciously short (%d characters)", b.ID, n)
		}
	}
	if len(missing) > 5 {
		t.Errorf("%d books without annotation: %v", len(missing), missing)
	}
	if b, _ := c.Book("horus-rising"); !strings.HasPrefix(b.Summary, "То было легендарное время.") {
		t.Errorf("horus-rising annotation: %q", b.Summary)
	}
}

func TestAnnotationsRejectUnknownID(t *testing.T) {
	c, err := Parse([]byte("series: [{id: a, title: A}]\nbooks: [{series: a, title: T, author: X, year: 2000, kind: novel}]"))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ApplyAnnotations([]byte("t: fine\nt0: typo")); err == nil {
		t.Error("unknown id must be an error")
	}
	if err := c.ApplyAnnotations([]byte("t: >-\n  Short text.")); err != nil {
		t.Fatal(err)
	}
	if b, _ := c.Book("t"); b.Summary != "Short text." {
		t.Errorf("summary = %q", b.Summary)
	}
}

func TestRussianAuthorNames(t *testing.T) {
	c, err := Load("../../data/books.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range c.Books() {
		if !b.IsAnthology() && b.AuthorRU == "" {
			t.Errorf("%s: no Russian author name", b.ID)
		}
		if b.Editors != "" && b.EditorsRU == "" {
			t.Errorf("%s: editors without Russian names", b.ID)
		}
	}
	hr, _ := c.Book("horus-rising")
	if hr.AuthorLabel() != "Дэн Абнетт" || hr.AuthorOrig() != "Dan Abnett" {
		t.Errorf("horus-rising: %q / %q", hr.AuthorLabel(), hr.AuthorOrig())
	}
	tales, _ := c.Book("tales-of-heresy")
	if tales.AuthorLabel() != "Антология · ред. Ник Кайм, Линдси Пристли" || tales.AuthorOrig() != "Nick Kyme, Lindsey Priestley" {
		t.Errorf("tales-of-heresy: %q / %q", tales.AuthorLabel(), tales.AuthorOrig())
	}
	if era, _ := c.Book("era-of-ruin"); era.AuthorLabel() != "Антология" || era.AuthorOrig() != "" {
		t.Errorf("era-of-ruin: %q / %q", era.AuthorLabel(), era.AuthorOrig())
	}
}

func TestAuthorsMustBeListed(t *testing.T) {
	c, err := Parse([]byte("series: [{id: a, title: A}]\nbooks: [{series: a, title: T, author: New Author, year: 2000, kind: novel}]"))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ApplyAuthors([]byte("Someone Else: Кто-то")); err == nil {
		t.Error("an author without a Russian name must be an error")
	}
	if err := c.ApplyAuthors([]byte("New Author: Новый Автор")); err != nil {
		t.Fatal(err)
	}
}

func TestPeopleAreAuthorsOrEditors(t *testing.T) {
	c, err := Load("../../data/books.yaml")
	if err != nil {
		t.Fatal(err)
	}
	hr, _ := c.Book("horus-rising")
	if got := hr.People(); len(got) != 1 || got[0] != (Person{Name: "Dan Abnett", RU: "Дэн Абнетт"}) {
		t.Errorf("horus-rising: %+v", got)
	}
	tales, _ := c.Book("tales-of-heresy")
	if got := tales.PeopleKeys(); got != "Nick Kyme|Lindsey Priestley" {
		t.Errorf("tales-of-heresy keys: %q", got)
	}
	if got := tales.People(); got[1].RU != "Линдси Пристли" {
		t.Errorf("tales-of-heresy: %+v", got)
	}
	if era, _ := c.Book("era-of-ruin"); len(era.People()) != 0 || era.PeopleKeys() != "" {
		t.Errorf("an anthology without editors has nobody to filter by: %+v", era.People())
	}
}

func TestCoAuthoredBook(t *testing.T) {
	c, err := Load("../../data/books.yaml")
	if err != nil {
		t.Fatal(err)
	}
	v, ok := c.Book("visions-of-heresy")
	if !ok {
		t.Fatal("visions-of-heresy is missing")
	}
	if got := v.AuthorNames(); len(got) != 2 || got[0] != "Alan Merrett" || got[1] != "Guy Haley" {
		t.Errorf("author names: %v", got)
	}
	if v.AuthorLabel() != "Алан Меррет, Гай Хейли" || v.AuthorOrig() != "Alan Merrett, Guy Haley" {
		t.Errorf("labels: %q / %q", v.AuthorLabel(), v.AuthorOrig())
	}
	if p := v.People(); len(p) != 2 || p[1] != (Person{Name: "Guy Haley", RU: "Гай Хейли"}) {
		t.Errorf("people: %+v", p)
	}
	if v.Kind.Label() != "Артбук" {
		t.Errorf("kind label: %q", v.Kind.Label())
	}
}

func TestRussianOnlyBookAndHandWrittenAnnotation(t *testing.T) {
	c, err := Load("../../data/books.yaml")
	if err != nil {
		t.Fatal(err)
	}
	b, ok := c.Book("gnev-luperkalya")
	if !ok {
		t.Fatal("a book with an explicit id must be found under it")
	}
	if b.Title != "Гнев Луперкаля" || b.TitleRU != "" || b.AuthorLabel() != "Антология" {
		t.Errorf("russian-only book: %+v", b)
	}
	if !strings.HasPrefix(b.Summary, "Ересь Хоруса — эпохальный конфликт") {
		t.Errorf("annotation from annotations_extra.yaml: %q", b.Summary)
	}

	// the hand-written file wins over the generated one
	cat, _ := Parse([]byte("series: [{id: a, title: A}]\nbooks: [{series: a, title: T, author: X, year: 2000, kind: novel}]"))
	cat.ApplyAnnotations([]byte("t: generated"))
	cat.ApplyAnnotations([]byte("t: by hand"))
	if got, _ := cat.Book("t"); got.Summary != "by hand" {
		t.Errorf("summary = %q", got.Summary)
	}
}
