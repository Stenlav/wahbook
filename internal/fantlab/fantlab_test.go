package fantlab

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMatch(t *testing.T) {
	hits := []Work{
		{ID: 777965, RusName: `Талларн. "Палач"`, Name: "Tallarn: Executioner", Authors: "John French", Votes: 54},
		{ID: 989154, RusName: "Талларн", Name: "Tallarn", Authors: "John French", Votes: 64},
		{ID: 5, RusName: "Талларн", Name: "Tallarn", Authors: "Someone Else", Votes: 900},
	}
	w, ok := Match("Талларн", "Tallarn", "John French", hits)
	if !ok || w.ID != 989154 {
		t.Fatalf("got %+v ok=%v", w, ok)
	}
	if _, ok := Match("Нет такой", "No Such", "John French", hits); ok {
		t.Error("unexpected match")
	}
	// Anthologies have no single author.
	if w, ok := Match("Талларн", "x", "Various", hits); !ok || w.ID != 5 {
		t.Errorf("anthology match: %+v %v", w, ok)
	}
	// ё/е and punctuation do not matter.
	if _, ok := Match("Ёж", "", "Various", []Work{{ID: 1, RusName: "еж!"}}); !ok {
		t.Error("normalisation failed")
	}
}

func TestSearchAndWork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search-works":
			w.Write([]byte(`[{"work_id":36166,"rusname":"Возвышение Хоруса","name":"Horus Rising","all_autor_name":"Dan Abnett","markcount":1185,"midmark":[7.835],"name_show_im":"роман"},{"work_id":0,"rusname":"review"}]`))
		case "/work/36166":
			w.Write([]byte(`{"title":"Дэн Абнетт «Возвышение Хоруса»","val_voters":1185,"val_midmark":"7.84","work_description":"То было легендарное время.\r\n\r\nВторой абзац."}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), BaseURL: srv.URL}

	hits, err := c.Search("Возвышение Хоруса")
	if err != nil || len(hits) != 1 || hits[0].Votes != 1185 || hits[0].Mark < 7.8 {
		t.Fatalf("search: %+v %v", hits, err)
	}
	w, err := c.WorkByID(36166)
	if err != nil || w.Mark != 7.84 || w.Votes != 1185 || w.Description != "То было легендарное время.\r\n\r\nВторой абзац." {
		t.Fatalf("work: %+v %v", w, err)
	}
	if _, err := c.WorkByID(1); err == nil {
		t.Error("expected error for 404")
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	path := t.TempDir() + "/r.json"
	f, _ := Load(path)
	f.Updated = "2026-10-30"
	f.Books["horus-rising"] = Entry{Work: 36166, Rating: 7.84, Votes: 1185}
	if err := f.Save(path); err != nil {
		t.Fatal(err)
	}
	g, err := Load(path)
	if err != nil || g.Books["horus-rising"].Work != 36166 {
		t.Fatalf("%+v %v", g, err)
	}
}
