package fantlab

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// Translation says whether a work exists in Russian and, if FantLab knows it, who translated it.
// Official editions, samizdat and fan translations all count: for the reader it is "есть / нет".
type Translation struct {
	Translated  bool     `json:"translated"`
	Translators []string `json:"translators,omitempty"` // as recorded on FantLab
}

// blockForeign is FantLab's block of editions in other languages; every other block
// (books, audiobooks, comics, samizdat and fanzines) can hold a Russian edition.
const blockForeign = "80"

type extendedWork struct {
	Blocks map[string]struct {
		List []struct {
			Lang string `json:"lang_code"`
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

func translationOf(w extendedWork) Translation {
	var t Translation
	for block, b := range w.Blocks {
		if block == blockForeign {
			continue
		}
		for _, e := range b.List {
			if e.Lang == "ru" {
				t.Translated = true
			}
		}
	}
	seen := map[string]bool{}
	for _, tr := range w.Translations {
		if tr.Lang != "ru" {
			continue
		}
		t.Translated = true // a translation is recorded even if no edition is
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
