package archive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Thinnesen/chronicle/internal/auth"
)

// sessionTimeout limits each database call of a session, because the auth.Store methods have no context.
const sessionTimeout = 5 * time.Second

// Sessions keeps the login sessions of the UI in the archive database. All server replicas share them, and a
// restart keeps them. It implements auth.Store.
type Sessions struct {
	store     *Store
	ttl, idle time.Duration
	now       func() time.Time
}

// Sessions returns a session store with a maximum life and an idle timeout.
func (s *Store) Sessions(ttl, idle time.Duration) *Sessions {
	return &Sessions{store: s, ttl: ttl, idle: idle, now: time.Now}
}

// Create implements auth.Store. It also deletes the sessions that have ended.
func (s *Sessions) Create(id auth.Identity) (string, *auth.Session, error) {
	sid, err := auth.NewToken()
	if err != nil {
		return "", nil, err
	}
	csrf, err := auth.NewToken()
	if err != nil {
		return "", nil, err
	}
	identity, err := json.Marshal(id)
	if err != nil {
		return "", nil, fmt.Errorf("encode the identity: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), sessionTimeout)
	defer cancel()
	now := s.now()
	if _, err := s.store.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= $1 OR last_used <= $2`, now, now.Add(-s.idle)); err != nil {
		return "", nil, fmt.Errorf("delete the ended sessions: %w", err)
	}
	expires := now.Add(s.ttl)
	if _, err := s.store.pool.Exec(ctx, `INSERT INTO sessions (key, identity, csrf, expires_at, last_used) VALUES ($1, $2, $3, $4, $5)`,
		auth.SessionKey(sid), identity, csrf, expires, now); err != nil {
		return "", nil, fmt.Errorf("save the session: %w", err)
	}
	return sid, &auth.Session{Identity: id, CSRF: csrf, ExpiresAt: expires}, nil
}

// Get implements auth.Store. It marks a live session as used.
func (s *Sessions) Get(sessionID string) (*auth.Session, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sessionTimeout)
	defer cancel()
	now := s.now()
	var identity []byte
	sess := &auth.Session{}
	err := s.store.pool.QueryRow(ctx, `UPDATE sessions SET last_used = $2 WHERE key = $1 AND expires_at > $2 AND last_used > $3
RETURNING identity, csrf, expires_at`, auth.SessionKey(sessionID), now, now.Add(-s.idle)).Scan(&identity, &sess.CSRF, &sess.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, auth.ErrNoSession
	}
	if err != nil {
		return nil, fmt.Errorf("read the session: %w", err)
	}
	if err := json.Unmarshal(identity, &sess.Identity); err != nil {
		return nil, fmt.Errorf("decode the identity: %w", err)
	}
	return sess, nil
}

// Delete implements auth.Store.
func (s *Sessions) Delete(sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), sessionTimeout)
	defer cancel()
	// A failed delete leaves a session that ends with its life or its idle timeout.
	_, _ = s.store.pool.Exec(ctx, `DELETE FROM sessions WHERE key = $1`, auth.SessionKey(sessionID))
}
