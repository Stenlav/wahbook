package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wahbook/internal/auth"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	st, _ := auth.NewFileStore("")
	svc, err := auth.NewService(st)
	if err != nil {
		t.Fatal(err)
	}
	h := New(Config{
		Auth:          svc,
		AllowedOrigin: "https://site.example",
		KnownBook:     func(id string) bool { return id == "horus-rising" || id == "false-gods" },
	})
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts
}

func call(t *testing.T, ts *httptest.Server, method, path, token, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestAccountFlow(t *testing.T) {
	ts := newTestServer(t)

	status, out := call(t, ts, "POST", "/api/register", "", `{"email":"Loken@example.com","password":"mournival-1","name":"Локен"}`)
	if status != http.StatusCreated {
		t.Fatalf("register: %d %v", status, out)
	}
	token, _ := out["token"].(string)
	if token == "" {
		t.Fatal("no token returned")
	}

	if status, _ := call(t, ts, "POST", "/api/register", "", `{"email":"loken@example.com","password":"mournival-1"}`); status != http.StatusConflict {
		t.Errorf("duplicate register: %d", status)
	}
	if status, _ := call(t, ts, "POST", "/api/login", "", `{"email":"loken@example.com","password":"nope-nope"}`); status != http.StatusUnauthorized {
		t.Errorf("bad login: %d", status)
	}

	status, out = call(t, ts, "GET", "/api/me", token, "")
	if status != 200 || out["user"].(map[string]any)["email"] != "loken@example.com" {
		t.Fatalf("me: %d %v", status, out)
	}
	if _, leaked := out["user"].(map[string]any)["passwordHash"]; leaked {
		t.Error("password hash leaked in /api/me")
	}
	if status, _ := call(t, ts, "GET", "/api/me", "", ""); status != http.StatusUnauthorized {
		t.Errorf("me without token: %d", status)
	}

	if status, _ := call(t, ts, "PUT", "/api/progress/horus-rising", token, `{"status":"read"}`); status != http.StatusNoContent {
		t.Errorf("put progress: %d", status)
	}
	if status, _ := call(t, ts, "PUT", "/api/progress/no-such-book", token, `{"status":"read"}`); status != http.StatusNotFound {
		t.Errorf("unknown book: %d", status)
	}
	if status, _ := call(t, ts, "PUT", "/api/progress/false-gods", token, `{"status":"sleeping"}`); status != http.StatusBadRequest {
		t.Errorf("bad status: %d", status)
	}
	_, out = call(t, ts, "GET", "/api/progress", token, "")
	if p := out["progress"].(map[string]any); p["horus-rising"] != "read" || len(p) != 1 {
		t.Errorf("progress: %v", p)
	}

	// A second login gets its own token; logging out kills only that one.
	_, out = call(t, ts, "POST", "/api/login", "", `{"email":"loken@example.com","password":"mournival-1"}`)
	token2 := out["token"].(string)
	if status, _ := call(t, ts, "POST", "/api/logout", token2, ""); status != http.StatusNoContent {
		t.Errorf("logout: %d", status)
	}
	if status, _ := call(t, ts, "GET", "/api/me", token2, ""); status != http.StatusUnauthorized {
		t.Errorf("token after logout: %d", status)
	}
	if status, _ := call(t, ts, "GET", "/api/me", token, ""); status != 200 {
		t.Errorf("first session should survive: %d", status)
	}
}

func TestRejectsBadBodies(t *testing.T) {
	ts := newTestServer(t)
	for _, body := range []string{`not json`, `{"email":"a@b.co","password":"long-enough","extra":1}`, ``} {
		if status, _ := call(t, ts, "POST", "/api/register", "", body); status != http.StatusBadRequest {
			t.Errorf("body %q: %d", body, status)
		}
	}
	if status, _ := call(t, ts, "POST", "/api/register", "", `{"email":"bad","password":"long-enough"}`); status != http.StatusBadRequest {
		t.Errorf("bad email: %d", status)
	}
}

func TestCORS(t *testing.T) {
	ts := newTestServer(t)

	req, _ := http.NewRequest("OPTIONS", ts.URL+"/api/login", nil)
	req.Header.Set("Origin", "https://site.example")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("Access-Control-Allow-Origin") != "https://site.example" {
		t.Errorf("preflight from allowed origin: %d %v", resp.StatusCode, resp.Header)
	}

	req, _ = http.NewRequest("GET", ts.URL+"/api/health", nil)
	req.Header.Set("Origin", "https://evil.example")
	resp, _ = http.DefaultClient.Do(req)
	if resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("foreign origin must not get CORS headers")
	}
}

func TestRateLimit(t *testing.T) {
	ts := newTestServer(t)
	var last int
	for i := 0; i < 12; i++ {
		last, _ = call(t, ts, "POST", "/api/login", "", `{"email":"x@y.zz","password":"whatever-1"}`)
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("expected 429 after burst, got %d", last)
	}
}
