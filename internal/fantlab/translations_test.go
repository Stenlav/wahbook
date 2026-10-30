package fantlab

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTranslationStatuses(t *testing.T) {
	cases := map[string]struct {
		body string
		want Translation
	}{
		"official edition wins over samizdat": {
			`{"editions_blocks":{
			   "10":{"list":[{"lang_code":"ru","year":2006,"publisher":"[pub=14]Азбука-классика[/pub]"},{"lang_code":"ru","year":2021,"publisher":"[pub=4932]Фантастика Книжный Клуб[/pub]"}]},
			   "27":{"list":[{"lang_code":"ru","year":2025,"publisher":"[pub=1]Libhammer[/pub]"}]},
			   "80":{"list":[{"lang_code":"en","year":2006,"publisher":"The Black Library"}]}},
			 "translations":[{"lang_code":"ru","translations":[{"translators":[{"name":"Юрий Антипов"},{"name":"Ирина Савельева"}]},{"translators":[{"name":"Юрий Антипов"}]}]}]}`,
			Translation{Status: Published, Publisher: "Фантастика Книжный Клуб", Year: 2021, Translators: []string{"Ирина Савельева", "Юрий Антипов"}},
		},
		"samizdat only": {
			`{"editions_blocks":{"27":{"list":[{"lang_code":"ru","year":2025,"publisher":"[pub=1]Libhammer[/pub]"}]}}}`,
			Translation{Status: Fan},
		},
		"a recorded translation without any edition": {
			`{"translations":[{"lang_code":"ru","translations":[{"translators":[{"name":"Некто"}]}]}]}`,
			Translation{Status: Fan, Translators: []string{"Некто"}},
		},
		"only foreign editions": {
			`{"editions_blocks":{"80":{"list":[{"lang_code":"en","year":2022}]}}}`,
			Translation{Status: None},
		},
		"an audiobook is an official edition": {
			`{"editions_blocks":{"30":{"list":[{"lang_code":"ru","year":2019,"publisher":"[pub=7]Звук[/pub]"}]}}}`,
			Translation{Status: Published, Publisher: "Звук", Year: 2019},
		},
		"nothing at all": {`{}`, Translation{Status: None}},
	}
	for name, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(tc.body)) }))
		c := &Client{HTTP: srv.Client(), BaseURL: srv.URL}
		got, err := c.Translation(1)
		srv.Close()
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got.Status != tc.want.Status || got.Publisher != tc.want.Publisher || got.Year != tc.want.Year || len(got.Translators) != len(tc.want.Translators) {
			t.Errorf("%s: got %+v, want %+v", name, got, tc.want)
			continue
		}
		for i := range got.Translators {
			if got.Translators[i] != tc.want.Translators[i] {
				t.Errorf("%s: translators %v, want %v", name, got.Translators, tc.want.Translators)
			}
		}
	}
}

func TestTranslationsSnapshotRoundTrip(t *testing.T) {
	path := t.TempDir() + "/t.json"
	f, _ := LoadTranslations(path)
	f.Updated = "2026-10-30"
	f.Books["horus-rising"] = Translation{Status: Published, Publisher: "Фантастика Книжный Клуб", Year: 2021}
	if err := f.Save(path); err != nil {
		t.Fatal(err)
	}
	g, err := LoadTranslations(path)
	if tr, ok := g.Entry("horus-rising"); err != nil || !ok || tr.Year != 2021 {
		t.Fatalf("%+v %v", g, err)
	}
	var nilFile *TranslationsFile
	if _, ok := nilFile.Entry("x"); ok {
		t.Error("a nil snapshot has no entries")
	}
}
