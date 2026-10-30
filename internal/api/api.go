// Package api is the HTTP layer for accounts and reading progress.
//
// Auth is a bearer token in the Authorization header rather than a cookie:
// the site lives on github.io while the API lives elsewhere, and cross-site
// cookies are blocked by Safari and increasingly by other browsers.
package api

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"wahbook/internal/auth"
)

type Config struct {
	Auth          *auth.Service
	AllowedOrigin string            // exact site origin, e.g. https://user.github.io ("*" only for local dev)
	KnownBook     func(string) bool // validates book ids in progress updates; nil = accept any
}

type server struct {
	cfg     Config
	limiter *limiter
}

// New returns the API handler (all routes live under /api/).
func New(cfg Config) http.Handler {
	s := &server{cfg: cfg, limiter: newLimiter(10, time.Minute)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /api/register", s.limited(s.register))
	mux.HandleFunc("POST /api/login", s.limited(s.login))
	mux.HandleFunc("POST /api/logout", s.authed(s.logout))
	mux.HandleFunc("GET /api/me", s.authed(s.me))
	mux.HandleFunc("GET /api/progress", s.authed(s.getProgress))
	mux.HandleFunc("PUT /api/progress/{bookID}", s.authed(s.putProgress))
	return s.cors(mux)
}

// ── handlers ────────────────────────────────────────────────────────────────

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

func (s *server) register(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !decode(w, r, &in) {
		return
	}
	u, token, err := s.cfg.Auth.Register(in.Email, in.Password, in.Name)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sessionResponse(u, token))
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !decode(w, r, &in) {
		return
	}
	u, token, err := s.cfg.Auth.Login(in.Email, in.Password)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sessionResponse(u, token))
}

func (s *server) logout(w http.ResponseWriter, r *http.Request, _ auth.User) {
	if err := s.cfg.Auth.Logout(bearer(r)); err != nil {
		writeAuthError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) me(w http.ResponseWriter, _ *http.Request, u auth.User) {
	writeJSON(w, http.StatusOK, map[string]any{"user": publicUser(u)})
}

func (s *server) getProgress(w http.ResponseWriter, _ *http.Request, u auth.User) {
	p, err := s.cfg.Auth.Progress(u.ID)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"progress": p})
}

func (s *server) putProgress(w http.ResponseWriter, r *http.Request, u auth.User) {
	id := r.PathValue("bookID")
	if s.cfg.KnownBook != nil && !s.cfg.KnownBook(id) {
		writeError(w, http.StatusNotFound, "unknown_book", "Такой книги нет в каталоге.")
		return
	}
	var in struct {
		Status auth.Status `json:"status"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.cfg.Auth.SetProgress(u.ID, id, in.Status); err != nil {
		writeError(w, http.StatusBadRequest, "bad_status", "Неизвестный статус.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── middleware ──────────────────────────────────────────────────────────────

func (s *server) authed(h func(http.ResponseWriter, *http.Request, auth.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := s.cfg.Auth.Authenticate(bearer(r))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Нужно войти.")
			return
		}
		h(w, r, u)
	}
}

// limited throttles credential endpoints per client IP.
func (s *server) limited(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.limiter.allow(clientIP(r)) {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "rate_limited", "Слишком много попыток. Подождите минуту.")
			return
		}
		h(w, r)
	}
}

func (s *server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (s.cfg.AllowedOrigin == "*" || origin == s.cfg.AllowedOrigin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

// ── helpers ─────────────────────────────────────────────────────────────────

func sessionResponse(u auth.User, token string) map[string]any {
	return map[string]any{"token": token, "user": publicUser(u)}
}

func publicUser(u auth.User) map[string]string {
	return map[string]string{"id": u.ID, "email": u.Email, "name": u.Name}
}

func bearer(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректный запрос.")
		return false
	}
	return true
}

func writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidEmail):
		writeError(w, http.StatusBadRequest, "invalid_email", "Укажите корректный email.")
	case errors.Is(err, auth.ErrWeakPassword):
		writeError(w, http.StatusBadRequest, "weak_password", "Пароль должен быть от 8 до 128 символов.")
	case errors.Is(err, auth.ErrInvalidName):
		writeError(w, http.StatusBadRequest, "invalid_name", "Имя слишком длинное (до 40 символов).")
	case errors.Is(err, auth.ErrEmailTaken):
		writeError(w, http.StatusConflict, "email_taken", "Этот email уже зарегистрирован.")
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Неверный email или пароль.")
	default:
		log.Printf("api: internal error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal", "Внутренняя ошибка сервера.")
	}
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": code, "message": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func clientIP(r *http.Request) string {
	// X-Forwarded-For is only trustworthy behind your own proxy; configure the
	// proxy to overwrite it. Direct connections fall back to RemoteAddr.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// limiter is a small fixed-window per-key rate limiter.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string]*bucket
}

type bucket struct {
	n     int
	reset time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, hits: map[string]*bucket{}}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b := l.hits[key]
	if b == nil || now.After(b.reset) {
		for k, old := range l.hits { // drop stale buckets so the map cannot grow forever
			if now.After(old.reset) {
				delete(l.hits, k)
			}
		}
		b = &bucket{reset: now.Add(l.window)}
		l.hits[key] = b
	}
	b.n++
	return b.n <= l.max
}
