package auth

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newService(t *testing.T) *Service {
	t.Helper()
	st, err := NewFileStore("")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(st)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestPasswordHashRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := VerifyPassword("correct horse", h); !ok {
		t.Error("right password rejected")
	}
	if ok, _ := VerifyPassword("wrong horse", h); ok {
		t.Error("wrong password accepted")
	}
	if _, err := VerifyPassword("x", "plaintext"); err == nil {
		t.Error("malformed hash should error")
	}
}

func TestRegisterLoginLogout(t *testing.T) {
	svc := newService(t)

	u, token, err := svc.Register("  Horus@Example.com ", "luna-wolves-1", "Хорус")
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "horus@example.com" {
		t.Errorf("email not normalized: %q", u.Email)
	}
	if u.PasswordHash == "luna-wolves-1" || u.PasswordHash == "" {
		t.Error("password must be stored hashed")
	}
	if got, err := svc.Authenticate(token); err != nil || got.ID != u.ID {
		t.Fatalf("register token should authenticate: %v", err)
	}

	if _, _, err := svc.Register("HORUS@example.com", "another-pass", ""); !errors.Is(err, ErrEmailTaken) {
		t.Errorf("duplicate email: got %v", err)
	}
	if _, _, err := svc.Login("horus@example.com", "bad-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong password: got %v", err)
	}
	if _, _, err := svc.Login("nobody@example.com", "whatever-1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("unknown user: got %v", err)
	}

	_, token2, err := svc.Login("horus@example.com", "luna-wolves-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Logout(token2); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(token2); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("logged-out token still valid: %v", err)
	}
	if _, err := svc.Authenticate(token); err != nil {
		t.Errorf("other session must survive logout: %v", err)
	}
}

func TestRegisterValidation(t *testing.T) {
	svc := newService(t)
	for _, email := range []string{"", "no-at-sign", "a@b", "Name <a@b.co>"} {
		if _, _, err := svc.Register(email, "long-enough-pw", ""); !errors.Is(err, ErrInvalidEmail) {
			t.Errorf("email %q: got %v", email, err)
		}
	}
	if _, _, err := svc.Register("a@b.co", "short", ""); !errors.Is(err, ErrWeakPassword) {
		t.Errorf("short password: got %v", err)
	}
}

func TestSessionExpiry(t *testing.T) {
	svc := newService(t)
	_, token, err := svc.Register("a@b.co", "long-enough-pw", "")
	if err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return time.Now().Add(-2 * SessionTTL) }
	_, old, _ := svc.Login("a@b.co", "long-enough-pw") // issued "in the past"
	svc.now = time.Now
	if _, err := svc.Authenticate(old); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("expired session accepted: %v", err)
	}
	if _, err := svc.Authenticate(token); err != nil {
		t.Errorf("fresh session rejected: %v", err)
	}
}

func TestProgressAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	svc, _ := NewService(st)
	u, _, err := svc.Register("a@b.co", "long-enough-pw", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetProgress(u.ID, "horus-rising", "read"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetProgress(u.ID, "false-gods", "bogus"); err == nil {
		t.Error("bogus status accepted")
	}

	// Reopen from disk.
	st2, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	svc2, _ := NewService(st2)
	if _, _, err := svc2.Login("a@b.co", "long-enough-pw"); err != nil {
		t.Fatalf("login after reload: %v", err)
	}
	p, _ := svc2.Progress(u.ID)
	if p["horus-rising"] != "read" {
		t.Errorf("progress lost on reload: %v", p)
	}
	svc2.SetProgress(u.ID, "horus-rising", "")
	if p, _ := svc2.Progress(u.ID); len(p) != 0 {
		t.Errorf("empty status should clear: %v", p)
	}
}
