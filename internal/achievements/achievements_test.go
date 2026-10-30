package achievements

import (
	"strings"
	"testing"

	"wahbook/internal/catalog"
	"wahbook/internal/fantlab"
)

const minVotes = 5

func realInputs(t *testing.T) (*Config, Inputs) {
	t.Helper()
	cat, err := catalog.Load("../../data/books.yaml")
	if err != nil {
		t.Fatal(err)
	}
	ratings, err := fantlab.Load("../../data/ratings.json")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load("../../data/achievements.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return cfg, Inputs{Catalog: cat, Ratings: ratings, MinVotes: minVotes}
}

func TestRealRulesResolve(t *testing.T) {
	cfg, in := realInputs(t)
	page, err := cfg.Resolve(in)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Achievement{}
	for _, a := range page.Achievements {
		byID[a.ID] = a
	}

	if page.Total != len(in.Catalog.Books()) || page.Ranks[len(page.Ranks)-1].At != page.Total {
		t.Errorf("the last rank must need every book: total %d, ranks %+v", page.Total, page.Ranks)
	}
	if got := byID["chronicles-of-heresy"].Levels; len(got) != 3 || got[2].Need != 54 {
		t.Errorf("chronicles-of-heresy levels: %+v", got)
	}
	if a := byID["besieged-terra"]; a.Mode != "count" || len(a.Groups[0]) != 14 || a.Levels[2].Need != 14 {
		t.Errorf("besieged-terra: %+v", a)
	}
	if a := byID["favourite-author"]; a.Mode != "best" || len(a.Groups) < 10 {
		t.Errorf("favourite-author: mode %s, %d groups", a.Mode, len(a.Groups))
	}
	if a := byID["chronicler-of-eras"]; a.Mode != "cover" || a.Levels[2].Need != len(a.Groups) {
		t.Errorf("chronicler-of-eras: %+v", a.Levels)
	}
	if a := byID["planner"]; a.Status != "want" {
		t.Errorf("planner counts %q marks", a.Status)
	}
	if !byID["lorgars-word"].Secret {
		t.Error("lorgars-word must be secret")
	}

	// generated "complete works": Abnett has nine books, all novels of the cycle
	abnett, ok := byID["author-dan-abnett"]
	if !ok {
		t.Fatal("no collection for Dan Abnett")
	}
	if abnett.Title != "Полное собрание: Дэн Абнетт" || len(abnett.Groups[0]) != 9 || abnett.Levels[0].Tier != "gold" {
		t.Errorf("author-dan-abnett: %+v", abnett)
	}
	if _, ok := byID["author-ben-counter"]; ok {
		t.Error("an author with two books must not get a collection")
	}
}

func TestRankThresholdsAreAscending(t *testing.T) {
	cfg, in := realInputs(t)
	page, _ := cfg.Resolve(in)
	for i := 1; i < len(page.Ranks); i++ {
		if page.Ranks[i].At <= page.Ranks[i-1].At {
			t.Errorf("ranks out of order: %+v", page.Ranks)
		}
	}
}

func TestInvalidRules(t *testing.T) {
	cat, err := catalog.Parse([]byte(`
series: [{id: s, title: S}]
books:
  - {series: s, title: A, author: X, year: 2000, kind: novel}
  - {series: s, title: B, author: X, year: 2001, kind: novella}
`))
	if err != nil {
		t.Fatal(err)
	}
	in := Inputs{Catalog: cat, MinVotes: minVotes}
	ranks := "ranks: [{at: 1, title: R}, {at: all, title: Top}]\n"
	cases := map[string]string{
		"unknown book":        `{id: a, group: g, title: T, description: D, from: {books: [nope]}, levels: [{need: 1, tier: gold}]}`,
		"unknown series":      `{id: a, group: g, title: T, description: D, from: {series: nope}, levels: [{need: 1, tier: gold}]}`,
		"no source":           `{id: a, group: g, title: T, description: D, from: {}, levels: [{need: 1, tier: gold}]}`,
		"two sources":         `{id: a, group: g, title: T, description: D, from: {series: s, any: true}, levels: [{need: 1, tier: gold}]}`,
		"level too high":      `{id: a, group: g, title: T, description: D, from: {series: s}, levels: [{need: 3, tier: gold}]}`,
		"levels not rising":   `{id: a, group: g, title: T, description: D, from: {series: s}, levels: [{need: 2, tier: gold}, {need: 1, tier: gold}]}`,
		"unknown tier":        `{id: a, group: g, title: T, description: D, from: {series: s}, levels: [{need: 1, tier: platinum}]}`,
		"unknown kind":        `{id: a, group: g, title: T, description: D, from: {kind: comic}, levels: [{need: 1, tier: gold}]}`,
		"unknown status":      `{id: a, group: g, title: T, description: D, status: lost, from: {any: true}, levels: [{need: 1, tier: gold}]}`,
		"all for best":        `{id: a, group: g, title: T, description: D, from: {authors: best}, levels: [{need: all, tier: gold}]}`,
		"no group":            `{id: a, title: T, description: D, from: {any: true}, levels: [{need: 1, tier: gold}]}`,
		"rating matches none": `{id: a, group: g, title: T, description: D, from: {rating_at_least: 9}, levels: [{need: 1, tier: gold}]}`,
	}
	for name, def := range cases {
		cfg, err := Parse([]byte(ranks + "achievements: [" + def + "]"))
		if err != nil {
			t.Errorf("%s: parse: %v", name, err)
			continue
		}
		if _, err := cfg.Resolve(in); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}

	dup, _ := Parse([]byte(ranks + "achievements: [{id: a, group: g, title: T, description: D, from: {any: true}, levels: [{need: 1, tier: gold}]}, {id: a, group: g, title: T, description: D, from: {any: true}, levels: [{need: 1, tier: gold}]}]"))
	if _, err := dup.Resolve(in); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("duplicate ids: %v", err)
	}
	if _, err := Parse([]byte("ranks: []\nbogus: 1")); err == nil {
		t.Error("unknown field must be rejected")
	}
	badRanks, _ := Parse([]byte("ranks: [{at: 5, title: A}, {at: 3, title: B}]"))
	if _, err := badRanks.Resolve(in); err == nil {
		t.Error("descending ranks must be rejected")
	}
}
