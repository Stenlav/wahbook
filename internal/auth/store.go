package auth

import (
	"errors"
	"time"
)

var (
	ErrNotFound   = errors.New("auth: not found")
	ErrEmailTaken = errors.New("auth: email already registered")
)

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"` // stored lower-cased
	Name         string    `json:"name"`
	PasswordHash string    `json:"passwordHash"`
	CreatedAt    time.Time `json:"createdAt"`
}

// Session is stored by the SHA-256 of its token, so a leaked database
// does not let anyone impersonate a user.
type Session struct {
	TokenHash string    `json:"tokenHash"`
	UserID    string    `json:"userId"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Status is a reading status of a book: "want", "reading" or "read".
type Status string

// Store is the persistence boundary. The file-backed implementation is enough
// for a hobby deployment; swap in SQLite/Postgres by implementing this interface.
type Store interface {
	CreateUser(u User) error // ErrEmailTaken if the email exists
	UserByEmail(email string) (User, error)
	UserByID(id string) (User, error)

	CreateSession(s Session) error
	Session(tokenHash string) (Session, error)
	DeleteSession(tokenHash string) error

	Progress(userID string) (map[string]Status, error)
	SetProgress(userID, bookID string, st Status) error // empty status clears the entry
}
