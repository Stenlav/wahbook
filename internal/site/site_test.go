package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wahbook/internal/achievements"
	"wahbook/internal/catalog"
	"wahbook/internal/fantlab"
)

func TestBuild(t *testing.T) {
	cat, err := catalog.Load("../../data/books.yaml")
	if err != nil {
		t.Fatal(err)
	}
	covers := t.TempDir()
	os.WriteFile(filepath.Join(covers, "horus-rising.jpg"), []byte("jpeg"), 0o644)
	out := filepath.Join(t.TempDir(), "dist")

	ratings := &fantlab.File{Updated: "2026-10-30", Books: map[string]fantlab.Entry{
		"horus-rising": {Work: 36166, Rating: 7.84, Votes: 1185},
		"false-gods":   {Work: 36311, Rating: 10, Votes: 1}, // too few votes to show
	}}
	if err := Build(Config{Catalog: cat, OutDir: out, CoversDir: covers, BasePath: "wahbook", APIBase: "https://api.example.com/", Ratings: ratings}); err != nil {
		t.Fatal(err)
	}

	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	index := read("index.html")
	for _, want := range []string{
		`href="/wahbook/static/style.css"`,       // base path applied
		`src="/wahbook/covers/horus-rising.jpg"`, // existing cover linked
		`class="ph"`,                             // placeholder for books without a cover
		`id="siege"`, `id="primarchs"`,           // series sections
		`"https://api.example.com"`, // trailing slash trimmed, passed to JS
		"Horus Rising",
		"★ 7.84",                              // rating shown
		`href="https://fantlab.ru/work36166"`, // direct FantLab link
		"30.10.2026",
		`id="f-sort"`, `<option value="rating">Лучшие сначала</option>`, // sort selector
		`data-year="2006"`, `data-title="Возвышение Хоруса"`, `data-by="Дэн Абнетт"`, // what the sort reads from a card
		`<option value="Dan Abnett">Дэн Абнетт (9)</option>`,        // author filter: authors with their book counts
		`<option value="Nick Kyme">Ник Кайм (11)</option>`,          // an author who also edited three anthologies
		`<option value="Alan Merrett">Алан Меррет (1)</option>`,     // a co-author gets his own entry
		`<option value="Guy Haley">Гай Хейли (9)</option>`,          // eight novels and the co-authored artbook
		`<option value="Laurie Goulding">Лори Голдинг (4)</option>`, // editor only
		`data-authors="Nick Kyme|Lindsey Priestley"`,
		`<p class="by">Дэн Абнетт</p>`,              // Russian author first…
		`<p class="by-orig">Dan Abnett</p>`,         // …original spelling under it
		`Антология · ред. Ник Кайм, Линдси Пристли`, // editors of an anthology
		`data-summary="То было легендарное время.`,  // annotation reaches the card
	} {
		if !strings.Contains(index, want) {
			t.Errorf("index.html lacks %q", want)
		}
	}
	if strings.Contains(index, "★ 10.00") {
		t.Error("a rating with a single vote must not be shown")
	}
	for _, gone := range []string{"goodreads", "lexicanum", "data-wiki", "Русская вики", `id="bd-links"`, "на FantLab"} {
		if strings.Contains(index, gone) {
			t.Errorf("index.html must not mention %q", gone)
		}
	}
	// Series rating: false-gods has too few votes, so the heresy series is rated by horus-rising alone.
	if !strings.Contains(index, `★ 7.84 <small>средняя оценка серии · оценено книг: 1 из 54</small>`) {
		t.Error("series rating missing or wrong")
	}
	if strings.Count(index, `class="series-rate"`) != 1 {
		t.Error("series without rated books must not show a rating")
	}
	for _, f := range []string{"404.html", "catalog.json", "static/app.js", "covers/horus-rising.jpg", ".nojekyll"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
}

func TestInitials(t *testing.T) {
	cases := map[string]string{
		"Horus Rising":                        "HR",
		"The Flight of the Eisenstein":        "TFE",
		"Roboute Guilliman: Lord of Ultramar": "RG",
		"A Thousand Sons":                     "ATS",
	}
	for in, want := range cases {
		if got := initials(in); got != want {
			t.Errorf("initials(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSeriesRatingIsMeanOfRatedBooks(t *testing.T) {
	cat, err := catalog.Parse([]byte(`
series: [{id: s, title: S}, {id: empty, title: E}]
books:
  - {series: s, title: A, author: X, year: 2000, kind: novel}
  - {series: s, title: B, author: X, year: 2000, kind: novel}
  - {series: s, title: C, author: X, year: 2000, kind: novel}
  - {series: empty, title: D, author: X, year: 2000, kind: novel}
`))
	if err != nil {
		t.Fatal(err)
	}
	ratings := &fantlab.File{Books: map[string]fantlab.Entry{
		"a": {Work: 1, Rating: 8.00, Votes: 100},
		"b": {Work: 2, Rating: 7.00, Votes: 6},
		"c": {Work: 3, Rating: 10, Votes: 2}, // below the vote threshold: ignored
	}}
	out := filepath.Join(t.TempDir(), "dist")
	if err := Build(Config{Catalog: cat, OutDir: out, CoversDir: t.TempDir(), BasePath: "/", Ratings: ratings}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(out, "index.html"))
	if !strings.Contains(string(raw), "★ 7.50 <small>средняя оценка серии · оценено книг: 2 из 3</small>") {
		t.Errorf("mean of 8.00 and 7.00 should be 7.50 over 2 of 3 books")
	}
	if strings.Count(string(raw), `class="series-rate"`) != 1 {
		t.Error("the series without ratings must show none")
	}
}

func TestAchievementsAreEmbeddedForTheBrowser(t *testing.T) {
	cat, err := catalog.Load("../../data/books.yaml")
	if err != nil {
		t.Fatal(err)
	}
	rules, err := achievements.Parse([]byte(`
ranks: [{at: 1, title: Неофит}, {at: all, title: Примарх}]
achievements:
  - {id: trilogy, group: Серии, title: Трилогия, description: Первые три книги., from: {books: [horus-rising, false-gods, galaxy-in-flames]}, levels: [{need: all, tier: silver}]}
`))
	if err != nil {
		t.Fatal(err)
	}
	build := func(r *achievements.Config) string {
		out := filepath.Join(t.TempDir(), "dist")
		if err := Build(Config{Catalog: cat, OutDir: out, CoversDir: t.TempDir(), BasePath: "/", Achievements: r}); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(filepath.Join(out, "index.html"))
		return string(raw)
	}

	with := build(rules)
	for _, want := range []string{`id="ach-data"`, `id="ach-btn"`, `id="ach-dialog"`, `"id":"trilogy"`, `"title":"Неофит"`, `"total":126`} {
		if !strings.Contains(with, want) {
			t.Errorf("index.html lacks %s", want)
		}
	}
	without := build(nil)
	if strings.Contains(without, "ach-data") || strings.Contains(without, `id="ach-btn"`) {
		t.Error("without rules there must be no achievements UI")
	}

	// A broken rule fails the build instead of shipping a page that cannot work.
	bad, _ := achievements.Parse([]byte(`ranks: [{at: 1, title: A}]
achievements:
  - {id: x, group: g, title: T, description: D, from: {books: [no-such-book]}, levels: [{need: 1, tier: gold}]}
`))
	if err := Build(Config{Catalog: cat, OutDir: filepath.Join(t.TempDir(), "d"), CoversDir: t.TempDir(), BasePath: "/", Achievements: bad}); err == nil {
		t.Error("a rule that names an unknown book must fail the build")
	}
}

func TestTranslationStatusOnCards(t *testing.T) {
	snap := &fantlab.TranslationsFile{Updated: "2026-10-30", Books: map[string]fantlab.Translation{
		"horus-rising": {Status: fantlab.Published, Publisher: "Фантастика Книжный Клуб", Year: 2021, Translators: []string{"Ирина Савельева"}},
		"warhawk":      {Status: fantlab.Fan, Translators: []string{"Некто Переводчик"}},
		"dreadwing":    {Status: fantlab.None},
		"scars":        {Status: fantlab.Published, Publisher: "Азбука"},
	}}
	cat, err := catalog.Load("../../data/books.yaml")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "dist")
	if err := Build(Config{Catalog: cat, OutDir: out, CoversDir: t.TempDir(), BasePath: "/", Translations: snap}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(out, "index.html"))
	index := string(raw)
	for _, want := range []string{
		`<p class="tr tr-published" title="Издана на русском: Фантастика Книжный Клуб, 2021; перевод: Ирина Савельева">На русском</p>`,
		`<p class="tr tr-fan"`, `>Самиздат</p>`, `>Нет перевода</p>`,
		`data-tr="published"`, `data-tr="fan"`, `data-tr="none"`,
		`id="f-tr"`, `<option value="fan">Только самиздат</option>`,
		`title="Издана на русском: Азбука"`, // no year known: nothing is invented
	} {
		if !strings.Contains(index, want) {
			t.Errorf("index.html lacks %q", want)
		}
	}
	if strings.Contains(index, "Некто Переводчик") {
		t.Error("translators of a fan translation must not be listed")
	}
	// A book with neither a snapshot entry nor an override has an unknown status and gets no line.
	if strings.Contains(index, `<p class="tr tr-"`) {
		t.Error("an unknown status must not render a line")
	}
	// `translation:` in books.yaml wins over the snapshot; both books without FantLab data are covered by overrides.
	for id, want := range map[string]string{"lupercal-s-war": fantlab.Fan, "gnev-luperkalya": fantlab.Fan, "wrath-of-terra": fantlab.None} {
		b, ok := cat.Book(id)
		if !ok {
			t.Fatalf("%s missing", id)
		}
		if status, _, _ := translationText(b, nil); status != want {
			t.Errorf("%s: override gives %q, want %q", id, status, want)
		}
	}
}

func TestEveryBookHasATranslationStatus(t *testing.T) {
	cat, err := catalog.Load("../../data/books.yaml")
	if err != nil {
		t.Fatal(err)
	}
	snap, err := fantlab.LoadTranslations("../../data/translations.json")
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, b := range cat.Books() {
		status, label, note := translationText(b, snap)
		if status == "" || label == "" || note == "" {
			t.Errorf("%s has no translation status: run `go run ./cmd/translations` or set `translation:` in books.yaml", b.ID)
		}
		counts[status]++
	}
	if counts[fantlab.Published] < 80 || counts[fantlab.Fan] < 10 || counts[fantlab.None] < 3 {
		t.Errorf("suspicious distribution: %v", counts)
	}
}
