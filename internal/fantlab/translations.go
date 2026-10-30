package fantlab

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Translation statuses.
const (
	Published = "published" // there is an official Russian edition
	Fan       = "fan"       // Russian text exists only as samizdat or a fan translation
	None      = "none"      // not translated into Russian
)

// Translation says whether and how a work exists in Russian.
type Translation struct {
	Status      string   `json:"status"`
	Publisher   string   `json:"publisher,omitempty"`   // publisher of the latest official edition
	Year        int      `json:"year,omitempty"`        // year of that edition
	Translators []string `json:"translators,omitempty"` // as recorded on FantLab
}

// FantLab groups editions into blocks: 10 books, 30 audiobooks, 32 comics and
// artbooks, 27 samizdat and fanzines, 80 editions in other languages.
const (
	blockSamizdat = "27"
	blockForeign  = "80"
)

type extendedWork struct {
	Blocks map[string]struct {
		List []struct {
			Lang      string `json:"lang_code"`
			Year      int    `json:"year"`
			Publisher string `json:"publisher"`
		} `json:"list"`
	} `json:"editions_blocks"`
	Translations []struct {
		Lang         string `json:"lang_code"`
		Translations []struct {
			Translators []struct {
				Name string `json:"name"`
			} `json:"translators"`
		} `json:"translations"`
	} `json:"translations"`
}

// Translation fetches the Russian editions and translations of a work.
func (c *Client) Translation(workID int) (Translation, error) {
	var w extendedWork
	if err := c.getJSON(fmt.Sprintf("%s/work/%d/extended", c.BaseURL, workID), &w); err != nil {
		return Translation{}, err
	}
	return translationOf(w), nil
}

var bbcode = regexp.MustCompile(`\[/?[a-z]+(=[^\]]*)?\]`)

func translationOf(w extendedWork) Translation {
	t := Translation{Status: None}
	for block, b := range w.Blocks {
		if block == blockForeign {
			continue
		}
		for _, e := range b.List {
			if e.Lang != "ru" {
				continue
			}
			if block == blockSamizdat {
				if t.Status == None {
					t.Status = Fan
				}
				continue
			}
			t.Status = Published
			if e.Year >= t.Year { // the latest official edition tells the reader where to look
				t.Year = e.Year
				t.Publisher = strings.TrimSpace(bbcode.ReplaceAllString(e.Publisher, ""))
			}
		}
	}
	seen := map[string]bool{}
	for _, tr := range w.Translations {
		if tr.Lang != "ru" {
			continue
		}
		if t.Status == None {
			t.Status = Fan // a translation is recorded even though no edition is
		}
		for _, x := range tr.Translations {
			for _, p := range x.Translators {
				if p.Name != "" && !seen[p.Name] {
					seen[p.Name] = true
					t.Translators = append(t.Translators, p.Name)
				}
			}
		}
	}
	sort.Strings(t.Translators)
	return t
}

// TranslationsFile is data/translations.json: a snapshot refreshed by
// `go run ./cmd/translations`.
type TranslationsFile struct {
	Updated string                 `json:"updated"` // YYYY-MM-DD
	Books   map[string]Translation `json:"books"`   // by book id
}

func LoadTranslations(path string) (*TranslationsFile, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &TranslationsFile{Books: map[string]Translation{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var f TranslationsFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	if f.Books == nil {
		f.Books = map[string]Translation{}
	}
	return &f, nil
}

// Entry returns a book's translation record; safe to call on a nil snapshot.
func (f *TranslationsFile) Entry(bookID string) (Translation, bool) {
	if f == nil {
		return Translation{}, false
	}
	t, ok := f.Books[bookID]
	return t, ok
}

func (f *TranslationsFile) Save(path string) error {
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}
