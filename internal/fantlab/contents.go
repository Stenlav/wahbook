package fantlab

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// Story is one work inside an anthology or a collection.
type Story struct {
	Work    int      `json:"work"`              // FantLab work id
	Title   string   `json:"title"`             // Russian title, or the original when there is none
	Orig    string   `json:"orig,omitempty"`    // original title, when it differs
	Authors []string `json:"authors,omitempty"` // as spelled on FantLab
	Kind    string   `json:"kind,omitempty"`    // "рассказ", "повесть", "микрорассказ", …
	Year    int      `json:"year,omitempty"`
	Rating  float64  `json:"rating,omitempty"` // plain average of readers' marks
	Votes   int      `json:"votes,omitempty"`
}

type contentsWork struct {
	Children []struct {
		ID       int    `json:"work_id"`
		Name     string `json:"work_name"`
		NameOrig string `json:"work_name_orig"`
		Year     int    `json:"work_year"`
		Type     string `json:"work_type"`
		MidMark  string `json:"val_midmark"`
		Voters   int    `json:"val_voters"`
		Authors  []struct {
			Name string `json:"name"`
		} `json:"authors"`
	} `json:"children"`
}

// Contents returns the works an anthology or collection consists of, in the order FantLab lists them.
func (c *Client) Contents(workID int) ([]Story, error) {
	var w contentsWork
	if err := c.getJSON(fmt.Sprintf("%s/work/%d/extended", c.BaseURL, workID), &w); err != nil {
		return nil, err
	}
	var out []Story
	for _, ch := range w.Children {
		s := Story{Work: ch.ID, Title: ch.Name, Kind: ch.Type, Year: ch.Year, Votes: ch.Voters}
		if s.Title == "" {
			s.Title = ch.NameOrig
		} else if ch.NameOrig != "" && ch.NameOrig != ch.Name {
			s.Orig = ch.NameOrig
		}
		s.Rating, _ = strconv.ParseFloat(ch.MidMark, 64)
		for _, a := range ch.Authors {
			if a.Name != "" {
				s.Authors = append(s.Authors, a.Name)
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// ContentsFile is data/contents.json: a snapshot refreshed by `go run ./cmd/contents`.
type ContentsFile struct {
	Updated string             `json:"updated"` // YYYY-MM-DD
	Books   map[string][]Story `json:"books"`   // by book id
}

func LoadContents(path string) (*ContentsFile, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &ContentsFile{Books: map[string][]Story{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var f ContentsFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	if f.Books == nil {
		f.Books = map[string][]Story{}
	}
	return &f, nil
}

// Entry returns the stories of a book; safe to call on a nil snapshot.
func (f *ContentsFile) Entry(bookID string) ([]Story, bool) {
	if f == nil {
		return nil, false
	}
	s, ok := f.Books[bookID]
	return s, ok && len(s) > 0
}

func (f *ContentsFile) Save(path string) error {
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}
