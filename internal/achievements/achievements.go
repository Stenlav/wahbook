// Package achievements turns the rules in data/achievements.yaml into plain
// data for the page: which books each achievement counts and how many of them
// each level needs. The browser only counts the books the reader has marked.
package achievements

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"wahbook/internal/catalog"
	"wahbook/internal/fantlab"
)

// ── rules file ──────────────────────────────────────────────────────────────

// Need is a number of books, or "all" for every book the achievement can count.
type Need struct {
	N   int
	All bool
}

func (n *Need) UnmarshalYAML(v *yaml.Node) error {
	if v.Kind == yaml.ScalarNode && v.Value == "all" {
		n.All = true
		return nil
	}
	if err := v.Decode(&n.N); err != nil {
		return fmt.Errorf("line %d: need must be a number or \"all\"", v.Line)
	}
	return nil
}

type Config struct {
	Ranks             []RankDef          `yaml:"ranks"`
	Achievements      []Def              `yaml:"achievements"`
	AuthorCollections *AuthorCollections `yaml:"author_collections"`
}

type RankDef struct {
	At    Need   `yaml:"at"`
	Title string `yaml:"title"`
}

type Def struct {
	ID          string     `yaml:"id"`
	Group       string     `yaml:"group"`
	Title       string     `yaml:"title"`
	Description string     `yaml:"description"`
	Secret      bool       `yaml:"secret"`
	Status      string     `yaml:"status"` // which mark counts: read (default), want or reading
	From        From       `yaml:"from"`
	Levels      []LevelDef `yaml:"levels"`
}

// From says which books an achievement counts. Exactly one field is set.
type From struct {
	Series        string   `yaml:"series"`          // every book of a series
	Books         []string `yaml:"books"`           // a hand-picked list
	Kind          string   `yaml:"kind"`            // every book of a kind (anthology, novella, …)
	RatingAtLeast float64  `yaml:"rating_at_least"` // books rated at least this by readers
	VotesBelow    int      `yaml:"votes_below"`     // little-known books: fewer ratings than this
	Authors       string   `yaml:"authors"`         // "best": the most books by one author
	Years         bool     `yaml:"years"`           // distinct years of publication
	Any           bool     `yaml:"any"`             // any book
}

type LevelDef struct {
	Need Need   `yaml:"need"`
	Tier string `yaml:"tier"` // bronze, silver or gold
}

// AuthorCollections generates "read everything by <author>" for each author
// with at least MinBooks books.
type AuthorCollections struct {
	MinBooks    int    `yaml:"min_books"`
	GoldFrom    int    `yaml:"gold_from"` // gold when the author has at least this many books, otherwise silver
	Group       string `yaml:"group"`
	Title       string `yaml:"title"`       // {author} is replaced by the Russian name
	Description string `yaml:"description"` // {author}, {n}
}

// Load reads and parses a rules file.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

func Parse(raw []byte) (*Config, error) {
	var c Config
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("achievements: %w", err)
	}
	return &c, nil
}

// ── resolved form, embedded into the page ───────────────────────────────────

type Level struct {
	Need int    `json:"need"`
	Tier string `json:"tier"`
}

// Achievement is ready for the browser. Progress is computed from Groups:
//
//	mode "count": the number of marked books in Groups[0]
//	mode "best":  the largest number of marked books in any one group
//	mode "cover": the number of groups with at least one marked book
type Achievement struct {
	ID          string     `json:"id"`
	Group       string     `json:"group"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Secret      bool       `json:"secret,omitempty"`
	Status      string     `json:"status"`
	Mode        string     `json:"mode"`
	Groups      [][]string `json:"groups"`
	Levels      []Level    `json:"levels"`
}

type Rank struct {
	At    int    `json:"at"`
	Title string `json:"title"`
}

type Page struct {
	Ranks        []Rank        `json:"ranks"`
	Achievements []Achievement `json:"achievements"`
	Total        int           `json:"total"` // books in the catalog
}

// Inputs is what the rules are resolved against.
type Inputs struct {
	Catalog  *catalog.Catalog
	Ratings  *fantlab.File // may be nil: rating-based rules then find no books
	MinVotes int           // ratings with fewer votes are not trusted
}

var tiers = map[string]bool{"bronze": true, "silver": true, "gold": true}

// Resolve checks the rules and expands them into a Page.
func (c *Config) Resolve(in Inputs) (*Page, error) {
	books := in.Catalog.Books()
	page := &Page{Total: len(books)}
	seen := map[string]bool{}

	// ranks
	prev := 0
	for _, r := range c.Ranks {
		at := r.At.N
		if r.At.All {
			at = len(books)
		}
		if r.Title == "" || at <= prev {
			return nil, fmt.Errorf("achievements: rank %q must have a title and a higher threshold than the previous one", r.Title)
		}
		page.Ranks = append(page.Ranks, Rank{At: at, Title: r.Title})
		prev = at
	}
	if len(page.Ranks) == 0 {
		return nil, fmt.Errorf("achievements: no ranks")
	}

	add := func(a Achievement) error {
		if seen[a.ID] {
			return fmt.Errorf("achievements: duplicate id %q", a.ID)
		}
		seen[a.ID] = true
		page.Achievements = append(page.Achievements, a)
		return nil
	}

	for _, d := range c.Achievements {
		a, err := c.resolveDef(d, in, books)
		if err != nil {
			return nil, fmt.Errorf("achievements: %q: %w", d.ID, err)
		}
		if err := add(a); err != nil {
			return nil, err
		}
	}

	if ac := c.AuthorCollections; ac != nil {
		for _, name := range authorsByBooks(books) {
			ids := authorBooks(books, name)
			if len(ids) < ac.MinBooks {
				continue
			}
			ru := name
			if b, _ := in.Catalog.Book(ids[0]); b != nil {
				for _, p := range b.People() {
					if p.Name == name {
						ru = p.RU
					}
				}
			}
			tier := "silver"
			if len(ids) >= ac.GoldFrom {
				tier = "gold"
			}
			r := strings.NewReplacer("{author}", ru, "{n}", fmt.Sprint(len(ids)))
			err := add(Achievement{
				ID: "author-" + catalog.Slug(name), Group: ac.Group,
				Title: r.Replace(ac.Title), Description: r.Replace(ac.Description),
				Status: "read", Mode: "count", Groups: [][]string{ids},
				Levels: []Level{{Need: len(ids), Tier: tier}},
			})
			if err != nil {
				return nil, err
			}
		}
	}
	return page, nil
}

func (c *Config) resolveDef(d Def, in Inputs, books []*catalog.Book) (Achievement, error) {
	if d.ID == "" || d.Title == "" || d.Description == "" || d.Group == "" {
		return Achievement{}, fmt.Errorf("id, group, title and description are required")
	}
	status := d.Status
	if status == "" {
		status = "read"
	}
	if status != "read" && status != "want" && status != "reading" {
		return Achievement{}, fmt.Errorf("unknown status %q", status)
	}

	groups, mode, err := selectBooks(d.From, in, books)
	if err != nil {
		return Achievement{}, err
	}

	// the most a reader can reach, to reject levels nobody could earn
	limit := 0
	switch mode {
	case "count":
		limit = len(groups[0])
	case "cover":
		limit = len(groups)
	case "best":
		for _, g := range groups {
			limit = max(limit, len(g))
		}
	}

	if len(d.Levels) == 0 {
		return Achievement{}, fmt.Errorf("no levels")
	}
	var levels []Level
	prev := 0
	for _, l := range d.Levels {
		need := l.Need.N
		if l.Need.All {
			if mode == "best" {
				return Achievement{}, fmt.Errorf(`"all" makes no sense for "best author"`)
			}
			need = limit
		}
		switch {
		case !tiers[l.Tier]:
			return Achievement{}, fmt.Errorf("unknown tier %q", l.Tier)
		case need <= prev:
			return Achievement{}, fmt.Errorf("levels must be increasing (%d after %d)", need, prev)
		case need > limit:
			return Achievement{}, fmt.Errorf("level needs %d but only %d can be counted", need, limit)
		}
		levels = append(levels, Level{Need: need, Tier: l.Tier})
		prev = need
	}
	return Achievement{
		ID: d.ID, Group: d.Group, Title: d.Title, Description: d.Description, Secret: d.Secret,
		Status: status, Mode: mode, Groups: groups, Levels: levels,
	}, nil
}

// selectBooks resolves the From selector into groups of book ids and a mode.
func selectBooks(f From, in Inputs, books []*catalog.Book) ([][]string, string, error) {
	n := 0
	for _, set := range []bool{f.Series != "", len(f.Books) > 0, f.Kind != "", f.RatingAtLeast > 0, f.VotesBelow > 0, f.Authors != "", f.Years, f.Any} {
		if set {
			n++
		}
	}
	if n != 1 {
		return nil, "", fmt.Errorf("`from` must set exactly one source, got %d", n)
	}

	var ids []string
	switch {
	case f.Series != "":
		for _, s := range in.Catalog.Series {
			if s.ID == f.Series {
				for _, b := range s.Books {
					ids = append(ids, b.ID)
				}
			}
		}
		if len(ids) == 0 {
			return nil, "", fmt.Errorf("unknown or empty series %q", f.Series)
		}
	case len(f.Books) > 0:
		for _, id := range f.Books {
			if _, ok := in.Catalog.Book(id); !ok {
				return nil, "", fmt.Errorf("unknown book %q", id)
			}
			ids = append(ids, id)
		}
	case f.Kind != "":
		if catalog.Kind(f.Kind).Label() == "" {
			return nil, "", fmt.Errorf("unknown kind %q", f.Kind)
		}
		for _, b := range books {
			if string(b.Kind) == f.Kind {
				ids = append(ids, b.ID)
			}
		}
	case f.RatingAtLeast > 0:
		for _, b := range books {
			if e, ok := in.Ratings.Entry(b.ID); ok && e.Votes >= in.MinVotes && e.Rating >= f.RatingAtLeast {
				ids = append(ids, b.ID)
			}
		}
	case f.VotesBelow > 0:
		for _, b := range books {
			if e, ok := in.Ratings.Entry(b.ID); ok && e.Votes >= in.MinVotes && e.Votes < f.VotesBelow {
				ids = append(ids, b.ID)
			}
		}
	case f.Any:
		for _, b := range books {
			ids = append(ids, b.ID)
		}
	case f.Authors != "":
		if f.Authors != "best" {
			return nil, "", fmt.Errorf(`authors must be "best"`)
		}
		var groups [][]string
		for _, name := range authorsByBooks(books) {
			groups = append(groups, authorBooks(books, name))
		}
		return groups, "best", nil
	case f.Years:
		byYear := map[int][]string{}
		var years []int
		for _, b := range books {
			if _, ok := byYear[b.Year]; !ok {
				years = append(years, b.Year)
			}
			byYear[b.Year] = append(byYear[b.Year], b.ID)
		}
		sort.Ints(years)
		var groups [][]string
		for _, y := range years {
			groups = append(groups, byYear[y])
		}
		return groups, "cover", nil
	}
	if len(ids) == 0 {
		return nil, "", fmt.Errorf("the selector matches no books")
	}
	return [][]string{ids}, "count", nil
}

// authorsByBooks lists single authors (not anthologies), most books first.
func authorsByBooks(books []*catalog.Book) []string {
	count := map[string]int{}
	for _, b := range books {
		for _, name := range b.AuthorNames() {
			count[name]++
		}
	}
	names := make([]string, 0, len(count))
	for n := range count {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if count[names[i]] != count[names[j]] {
			return count[names[i]] > count[names[j]]
		}
		return names[i] < names[j]
	})
	return names
}

func authorBooks(books []*catalog.Book, name string) []string {
	var ids []string
	for _, b := range books {
		for _, n := range b.AuthorNames() {
			if n == name {
				ids = append(ids, b.ID)
			}
		}
	}
	return ids
}
