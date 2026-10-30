package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MinPasswordLen = 8
	MaxPasswordLen = 128 // argon2 on huge inputs is a cheap DoS vector
	SessionTTL     = 30 * 24 * time.Hour
)

// Errors a caller can show to the user.
var (
	ErrInvalidEmail       = errors.New("auth: invalid email")
	ErrWeakPassword       = errors.New("auth: password must be 8–128 characters")
	ErrInvalidName        = errors.New("auth: name too long")
	ErrInvalidCredentials = errors.New("auth: invalid email or password")
	ErrUnauthenticated    = errors.New("auth: not authenticated")
)

// Service holds the account logic; it knows nothing about HTTP.
type Service struct {
	store Store
	now   func() time.Time

	// dummyHash lets Login spend the same time for unknown emails as for
	// known ones, so response timing does not reveal who is registered.
	dummyHash string
}

func NewService(store Store) (*Service, error) {
	dummy, err := HashPassword("not-a-real-password")
	if err != nil {
		return nil, err
	}
	return &Service{store: store, now: time.Now, dummyHash: dummy}, nil
}

// Register creates an account and signs the user in.
func (s *Service) Register(email, password, name string) (User, string, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return User{}, "", err
	}
	if n := utf8.RuneCountInString(password); n < MinPasswordLen || n > MaxPasswordLen {
		return User{}, "", ErrWeakPassword
	}
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > 40 {
		return User{}, "", ErrInvalidName
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, "", err
	}
	u := User{ID: randomID(16), Email: email, Name: name, PasswordHash: hash, CreatedAt: s.now().UTC()}
	if err := s.store.CreateUser(u); err != nil {
		return User{}, "", err
	}
	token, err := s.newSession(u.ID)
	return u, token, err
}

// Login verifies credentials and returns a fresh session token.
func (s *Service) Login(email, password string) (User, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	u, err := s.store.UserByEmail(email)
	if err != nil {
		VerifyPassword(password, s.dummyHash) // equalize timing
		return User{}, "", ErrInvalidCredentials
	}
	ok, err := VerifyPassword(password, u.PasswordHash)
	if err != nil || !ok {
		return User{}, "", ErrInvalidCredentials
	}
	token, err := s.newSession(u.ID)
	return u, token, err
}

// Authenticate resolves a session token to its user.
func (s *Service) Authenticate(token string) (User, error) {
	sess, err := s.store.Session(hashToken(token))
	if err != nil {
		return User{}, ErrUnauthenticated
	}
	u, err := s.store.UserByID(sess.UserID)
	if err != nil {
		return User{}, ErrUnauthenticated
	}
	return u, nil
}

func (s *Service) Logout(token string) error {
	return s.store.DeleteSession(hashToken(token))
}

func (s *Service) Progress(userID string) (map[string]Status, error) {
	return s.store.Progress(userID)
}

func (s *Service) SetProgress(userID, bookID string, st Status) error {
	switch st {
	case "", "want", "reading", "read":
		return s.store.SetProgress(userID, bookID, st)
	}
	return errors.New("auth: unknown status")
}

func (s *Service) newSession(userID string) (string, error) {
	token := randomID(32)
	err := s.store.CreateSession(Session{
		TokenHash: hashToken(token),
		UserID:    userID,
		ExpiresAt: s.now().Add(SessionTTL),
	})
	return token, err
}

func normalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || len(email) > 254 || !strings.Contains(email[strings.LastIndex(email, "@"):], ".") {
		return "", ErrInvalidEmail
	}
	return email, nil
}

func randomID(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		panic(err) // the OS random source failing is not recoverable
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
