package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// ErrNoSession means that the session does not exist or has ended.
var ErrNoSession = errors.New("no session")

// Session is a logged-in browser.
type Session struct {
	Identity  Identity
	CSRF      string
	ExpiresAt time.Time
	lastUsed  time.Time
}

// Store keeps sessions on the server, so that a logout ends a session at once.
type Store interface {
	// Create starts a session and returns the secret session ID for the cookie.
	Create(id Identity) (string, *Session, error)
	// Get returns a live session and marks it as used.
	Get(sessionID string) (*Session, error)
	Delete(sessionID string)
}

// MemoryStore keeps sessions in memory. Use it with one server replica. Sessions end when the server restarts.
type MemoryStore struct {
	ttl, idle time.Duration
	now       func() time.Time
	mu        sync.Mutex
	// sessions are keyed by a hash of the session ID, so that the memory never holds a usable cookie value.
	sessions map[string]*Session
}

// NewMemoryStore returns a store with a maximum life and an idle timeout.
func NewMemoryStore(ttl, idle time.Duration) *MemoryStore {
	return &MemoryStore{ttl: ttl, idle: idle, now: time.Now, sessions: map[string]*Session{}}
}

// Create implements Store.
func (m *MemoryStore) Create(id Identity) (string, *Session, error) {
	sid, err := randomToken()
	if err != nil {
		return "", nil, err
	}
	csrf, err := randomToken()
	if err != nil {
		return "", nil, err
	}
	now := m.now()
	s := &Session{Identity: id, CSRF: csrf, ExpiresAt: now.Add(m.ttl), lastUsed: now}
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, old := range m.sessions {
		if m.ended(old, now) {
			delete(m.sessions, k)
		}
	}
	m.sessions[hashKey(sid)] = s
	return sid, s, nil
}

// Get implements Store.
func (m *MemoryStore) Get(sessionID string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := hashKey(sessionID)
	s, ok := m.sessions[k]
	now := m.now()
	if !ok || m.ended(s, now) {
		delete(m.sessions, k)
		return nil, ErrNoSession
	}
	s.lastUsed = now
	copied := *s
	return &copied, nil
}

// Delete implements Store.
func (m *MemoryStore) Delete(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, hashKey(sessionID))
}

func (m *MemoryStore) ended(s *Session, now time.Time) bool {
	return !now.Before(s.ExpiresAt) || now.Sub(s.lastUsed) >= m.idle
}

func hashKey(sessionID string) string {
	h := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(h[:])
}

// randomToken returns 32 random bytes as URL-safe base64.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
