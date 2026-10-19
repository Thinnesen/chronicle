package archive

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/Thinnesen/chronicle/internal/auth"
)

func TestSessions(t *testing.T) {
	s := openTest(t)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	// Two stores on one database are two server replicas.
	a, b := s.Sessions(8*time.Hour, time.Hour), s.Sessions(8*time.Hour, time.Hour)
	a.now = func() time.Time { return now }
	b.now = func() time.Time { return now }

	sid, created, err := a.Create(auth.Identity{User: "oidc:alice", Groups: []string{"oidc:team-a"}})
	if err != nil {
		t.Fatal(err)
	}
	var key string
	if err := s.pool.QueryRow(t.Context(), `SELECT key FROM sessions`).Scan(&key); err != nil || key == sid {
		t.Fatalf("the table must hold a hash of the session ID: %v", err)
	}
	got, err := b.Get(sid)
	if err != nil || got.Identity.User != "oidc:alice" || !slices.Equal(got.Identity.Groups, []string{"oidc:team-a"}) || got.CSRF != created.CSRF {
		t.Fatalf("the other replica: %v %+v", err, got)
	}

	// A use within a minute does not write the row.
	now = now.Add(10 * time.Second)
	if _, err := a.Get(sid); err != nil {
		t.Fatal(err)
	}
	var used time.Time
	if err := s.pool.QueryRow(t.Context(), `SELECT last_used FROM sessions`).Scan(&used); err != nil || !used.Equal(now.Add(-10*time.Second)) {
		t.Fatalf("last_used = %v, %v", used, err)
	}

	// Each use moves the idle timeout.
	now = now.Add(50 * time.Minute)
	if _, err := a.Get(sid); err != nil {
		t.Fatalf("a used session must live: %v", err)
	}
	now = now.Add(50 * time.Minute)
	if _, err := b.Get(sid); err != nil {
		t.Fatalf("the use must reset the idle timeout: %v", err)
	}
	now = now.Add(61 * time.Minute)
	if _, err := a.Get(sid); !errors.Is(err, auth.ErrNoSession) {
		t.Fatalf("an idle session must end: %v", err)
	}

	sid, _, _ = a.Create(auth.Identity{User: "oidc:bob"})
	a.Delete(sid)
	if _, err := b.Get(sid); !errors.Is(err, auth.ErrNoSession) {
		t.Fatalf("a logout must end the session on all replicas: %v", err)
	}
	if _, err := a.Get("not-a-session"); !errors.Is(err, auth.ErrNoSession) {
		t.Fatalf("an unknown session: %v", err)
	}
}
