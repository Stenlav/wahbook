package fantlab

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTranslationIsYesOrNo(t *testing.T) {
	cases := map[string]struct {
		body string
		want Translation
	}{
		"an official edition with its translators": {
			`{"editions_blocks":{
			   "10":{"list":[{"lang_code":"ru","year":2006},{"lang_code":"ru","year":2021}]},
			   "80":{"list":[{"lang_code":"en","year":2006}]}},
			 "translations":[{"lang_code":"ru","translations":[{"translators":[{"name":"Юрий Антипов"},{"name":"Ирина Савельева"}]},{"translators":[{"name":"Юрий Антипов"}]}]}]}`,
			Translation{Translated: true, Translators: []string{"Ирина Савельева", "Юрий Антипов"}},
		},
		"samizdat counts as a translation": {
			`{"editions_blocks":{"27":{"list":[{"lang_code":"ru","year":2025}]}}}`,
			Translation{Translated: true},
		},
		"a recorded translation without any edition": {
			`{"translations":[{"lang_code":"ru","translations":[{"translators":[{"name":"Некто"}]}]}]}`,
			Translation{Translated: true, Translators: []string{"Некто"}},
		},
		"an audiobook counts": {
			`{"editions_blocks":{"30":{"list":[{"lang_code":"ru","year":2019}]}}}`,
			Translation{Translated: true},
		},
		"only foreign editions": {
			`{"editions_blocks":{"80":{"list":[{"lang_code":"en","year":2022}]}}}`,
			Translation{},
		},
		"a translation into another language is not ours": {
			`{"translations":[{"lang_code":"de","translations":[{"translators":[{"name":"Hans"}]}]}]}`,
			Translation{},
		},
		"nothing at all": {`{}`, Translation{}},
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
		if got.Translated != tc.want.Translated || len(got.Translators) != len(tc.want.Translators) {
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
	f.Books["horus-rising"] = Translation{Translated: true, Translators: []string{"Ирина Савельева"}}
	if err := f.Save(path); err != nil {
		t.Fatal(err)
	}
	g, err := LoadTranslations(path)
	if tr, ok := g.Entry("horus-rising"); err != nil || !ok || !tr.Translated || tr.Translators[0] != "Ирина Савельева" {
		t.Fatalf("%+v %v", g, err)
	}
	var nilFile *TranslationsFile
	if _, ok := nilFile.Entry("x"); ok {
		t.Error("a nil snapshot has no entries")
	}
}
