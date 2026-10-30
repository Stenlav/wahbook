package fantlab

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"children":[
		  {"work_id":777901,"work_name":"Братство Бури","work_name_orig":"Brotherhood of the Storm","work_year":2012,"work_type":"повесть","val_midmark":"7.38","val_voters":65,"authors":[{"name":"Крис Райт"}]},
		  {"work_id":2,"work_name":"Lupus Daemonis","work_name_orig":"Lupus Daemonis","work_year":2021,"work_type":"рассказ","val_midmark":"7.06","val_voters":34,"authors":[{"name":"Грэм Макнилл"}]},
		  {"work_id":3,"work_name":"","work_name_orig":"Terminus","work_year":2021,"work_type":"рассказ","val_midmark":"0","val_voters":0,"authors":[]}
		]}`))
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), BaseURL: srv.URL}

	got, err := c.Contents(1)
	if err != nil || len(got) != 3 {
		t.Fatalf("%+v %v", got, err)
	}
	if s := got[0]; s.Title != "Братство Бури" || s.Orig != "Brotherhood of the Storm" || s.Kind != "повесть" || s.Rating != 7.38 || s.Votes != 65 || s.Authors[0] != "Крис Райт" {
		t.Errorf("first story: %+v", s)
	}
	if got[1].Orig != "" {
		t.Errorf("the original title must be omitted when it equals the title: %+v", got[1])
	}
	if got[2].Title != "Terminus" || got[2].Rating != 0 || len(got[2].Authors) != 0 {
		t.Errorf("a story with only an original title: %+v", got[2])
	}
}

func TestContentsSnapshotRoundTrip(t *testing.T) {
	path := t.TempDir() + "/c.json"
	f, _ := LoadContents(path)
	f.Updated = "2026-10-30"
	f.Books["cthonia-s-reckoning"] = []Story{{Work: 1, Title: "Сыны Хтонии", Authors: []string{"Джон Френч"}, Votes: 20, Rating: 7.4}}
	if err := f.Save(path); err != nil {
		t.Fatal(err)
	}
	g, err := LoadContents(path)
	if s, ok := g.Entry("cthonia-s-reckoning"); err != nil || !ok || s[0].Title != "Сыны Хтонии" {
		t.Fatalf("%+v %v", g, err)
	}
	if _, ok := g.Entry("nothing"); ok {
		t.Error("an unknown book has no contents")
	}
	var nilFile *ContentsFile
	if _, ok := nilFile.Entry("x"); ok {
		t.Error("a nil snapshot has no entries")
	}
}
