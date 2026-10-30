package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileStore keeps everything in memory and, when a path is given, persists it
// as one JSON file (written atomically after every change). With an empty path
// it is a pure in-memory store, which is what the tests use.
type FileStore struct {
	mu   sync.Mutex
	path string
	d    fileData
}

type fileData struct {
	Users    map[string]User              `json:"users"`    // by id
	Sessions map[string]Session           `json:"sessions"` // by token hash
	Progress map[string]map[string]Status `json:"progress"` // user id -> book id -> status
}

func NewFileStore(path string) (*FileStore, error) {
	s := &FileStore{path: path, d: fileData{
		Users:    map[string]User{},
		Sessions: map[string]Session{},
		Progress: map[string]map[string]Status{},
	}}
	if path == "" {
		return s, nil
	}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.d); err != nil {
		return nil, err
	}
	if s.d.Users == nil {
		s.d.Users = map[string]User{}
	}
	if s.d.Sessions == nil {
		s.d.Sessions = map[string]Session{}
	}
	if s.d.Progress == nil {
		s.d.Progress = map[string]map[string]Status{}
	}
	return s, nil
}

// save must be called with s.mu held.
func (s *FileStore) save() error {
	if s.path == "" {
		return nil
	}
	raw, err := json.MarshalIndent(s.d, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *FileStore) CreateUser(u User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.d.Users {
		if existing.Email == u.Email {
			return ErrEmailTaken
		}
	}
	s.d.Users[u.ID] = u
	return s.save()
}

func (s *FileStore) UserByEmail(email string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.d.Users {
		if u.Email == email {
			return u, nil
		}
	}
	return User{}, ErrNotFound
}

func (s *FileStore) UserByID(id string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.d.Users[id]; ok {
		return u, nil
	}
	return User{}, ErrNotFound
}

func (s *FileStore) CreateSession(sess Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for h, old := range s.d.Sessions { // opportunistic cleanup of expired sessions
		if now.After(old.ExpiresAt) {
			delete(s.d.Sessions, h)
		}
	}
	s.d.Sessions[sess.TokenHash] = sess
	return s.save()
}

func (s *FileStore) Session(tokenHash string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.d.Sessions[tokenHash]
	if !ok || time.Now().After(sess.ExpiresAt) {
		return Session{}, ErrNotFound
	}
	return sess, nil
}

func (s *FileStore) DeleteSession(tokenHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.d.Sessions, tokenHash)
	return s.save()
}

func (s *FileStore) Progress(userID string) (map[string]Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]Status, len(s.d.Progress[userID]))
	for k, v := range s.d.Progress[userID] {
		out[k] = v
	}
	return out, nil
}

func (s *FileStore) SetProgress(userID, bookID string, st Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st == "" {
		delete(s.d.Progress[userID], bookID)
		return s.save()
	}
	if s.d.Progress[userID] == nil {
		s.d.Progress[userID] = map[string]Status{}
	}
	s.d.Progress[userID][bookID] = st
	return s.save()
}
