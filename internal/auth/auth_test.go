package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Thinnesen/chronicle/internal/auth/oidctest"
)

type env struct {
	idp  *oidctest.Provider
	o    *OIDC
	srv  *httptest.Server
	user *http.Client
}

// newEnv starts a provider and a server with the login handler and an API that echoes the identity.
func newEnv(t *testing.T, insecure bool) *env {
	t.Helper()
	return newEnvWithStore(t, insecure, NewMemoryStore(time.Hour, time.Hour))
}

func newEnvWithStore(t *testing.T, insecure bool, store Store) *env {
	t.Helper()
	idp := oidctest.New("")
	t.Cleanup(idp.Close)
	idp.Claims = map[string]any{"email": "alice@example.com", "email_verified": true, "groups": []any{"team-a", "admins"}}
	e := &env{idp: idp}
	mux := http.NewServeMux()
	e.srv = httptest.NewServer(mux)
	t.Cleanup(e.srv.Close)
	cfg := &Config{InsecureCookies: insecure, OIDC: OIDCConfig{Issuer: idp.Issuer, ClientID: "chronicle", RedirectURL: e.srv.URL + "/auth/callback",
		ExtraAudiences: []string{"chronicle-cli"}}}
	if err := cfg.complete(); err != nil {
		t.Fatal(err)
	}
	o, err := NewOIDC(context.Background(), cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	e.o = o
	mux.Handle("/auth/", o.Handler())
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		for _, a := range []Authenticator{o.SessionAuthenticator(), o.BearerAuthenticator()} {
			id, err := a.Authenticate(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}
			if id != nil {
				_ = json.NewEncoder(w).Encode(id)
				return
			}
		}
		http.Error(w, "no credentials", http.StatusUnauthorized)
	})
	jar, _ := cookiejar.New(nil)
	e.user = &http.Client{Jar: jar}
	return e
}

func (e *env) do(t *testing.T, method, path string, header map[string]string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := e.user.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var b strings.Builder
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	b.Write(buf[:n])
	return resp.StatusCode, b.String()
}

func (e *env) me(t *testing.T) map[string]any {
	t.Helper()
	code, body := e.do(t, http.MethodGet, "/auth/me", nil)
	if code != http.StatusOK {
		t.Fatalf("me: %d %s", code, body)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(body), &m)
	return m
}

func TestLogin(t *testing.T) {
	e := newEnv(t, true)
	if code, _ := e.do(t, http.MethodGet, "/auth/login?redirect=/runs", nil); code != http.StatusNotFound {
		// The redirect goes to /runs, which this test server does not serve.
		t.Fatalf("login ended with %d", code)
	}
	me := e.me(t)
	if me["user"] != "oidc:alice@example.com" || len(me["groups"].([]any)) != 2 || me["groups"].([]any)[0] != "oidc:team-a" {
		t.Fatalf("me = %v", me)
	}
	csrf := me["csrfToken"].(string)

	if code, body := e.do(t, http.MethodGet, "/api", nil); code != http.StatusOK || !strings.Contains(body, "oidc:alice@example.com") {
		t.Errorf("GET with the session: %d %s", code, body)
	}
	if code, _ := e.do(t, http.MethodPost, "/api", nil); code != http.StatusUnauthorized {
		t.Errorf("POST without a CSRF token: %d", code)
	}
	if code, _ := e.do(t, http.MethodPost, "/api", map[string]string{CSRFHeader: "wrong"}); code != http.StatusUnauthorized {
		t.Errorf("POST with a wrong CSRF token: %d", code)
	}
	if code, _ := e.do(t, http.MethodPost, "/api", map[string]string{CSRFHeader: csrf}); code != http.StatusOK {
		t.Errorf("POST with the CSRF token: %d", code)
	}
	if code, _ := e.do(t, http.MethodPost, "/auth/logout", map[string]string{CSRFHeader: csrf}); code != http.StatusNoContent {
		t.Fatalf("logout: %d", code)
	}
	if code, _ := e.do(t, http.MethodGet, "/auth/me", nil); code != http.StatusUnauthorized {
		t.Errorf("me after the logout: %d", code)
	}
}

func TestLoginAttacks(t *testing.T) {
	e := newEnv(t, true)
	if code, _ := e.do(t, http.MethodGet, "/auth/callback?code=x&state=y", nil); code != http.StatusBadRequest {
		t.Errorf("a callback that did not start here: %d", code)
	}
	e.idp.WrongNonce = true
	if code, _ := e.do(t, http.MethodGet, "/auth/login", nil); code != http.StatusUnauthorized {
		t.Errorf("a wrong nonce: %d", code)
	}
	e.idp.WrongNonce = false
	e.idp.Claims = map[string]any{"email": "eve@example.com", "email_verified": false}
	if code, _ := e.do(t, http.MethodGet, "/auth/login", nil); code != http.StatusUnauthorized {
		t.Errorf("an unverified email: %d", code)
	}
}

func TestSecureCookies(t *testing.T) {
	e := newEnv(t, false)
	e.user.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, _ := http.NewRequest(http.MethodGet, e.srv.URL+"/auth/login", nil)
	resp, err := e.user.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	c := resp.Header.Get("Set-Cookie")
	for _, want := range []string{"__Host-chronicle-login=", "Path=/", "HttpOnly", "Secure", "SameSite=Lax"} {
		if !strings.Contains(c, want) {
			t.Errorf("Set-Cookie %q has no %q", c, want)
		}
	}
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "code_challenge_method=S256") || !strings.Contains(loc, "nonce=") {
		t.Errorf("authorize URL = %s", loc)
	}
}

func TestBearer(t *testing.T) {
	e := newEnv(t, true)
	claims := map[string]any{"email": "bot@example.com"}
	tests := []struct {
		name  string
		token string
		code  int
	}{
		{"client audience", e.idp.IDToken(claims, "chronicle", time.Hour), http.StatusOK},
		{"extra audience", e.idp.IDToken(claims, "chronicle-cli", time.Hour), http.StatusOK},
		{"other audience", e.idp.IDToken(claims, "other", time.Hour), http.StatusUnauthorized},
		{"expired", e.idp.IDToken(claims, "chronicle", -time.Minute), http.StatusUnauthorized},
		{"garbage", "abc.def.ghi", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if code, body := e.do(t, http.MethodPost, "/api", map[string]string{"Authorization": "Bearer " + tt.token}); code != tt.code {
				t.Errorf("code %d, body %s", code, body)
			}
		})
	}
}

func TestIdentityFromClaims(t *testing.T) {
	o := &OIDCConfig{UsernameClaim: "sub", GroupsClaim: "groups", UsernamePrefix: "oidc:", GroupsPrefix: "oidc:"}
	id, err := identityFromClaims(map[string]any{"sub": "u1", "groups": "one"}, o)
	if err != nil || id.User != "oidc:u1" || len(id.Groups) != 1 {
		t.Errorf("identity = %+v, %v", id, err)
	}
	if _, err := identityFromClaims(map[string]any{}, o); err == nil {
		t.Error("an empty user name was accepted")
	}
	o.UsernamePrefix = ""
	if _, err := identityFromClaims(map[string]any{"sub": "system:admin"}, o); err == nil {
		t.Error("a system user name was accepted")
	}
}

func TestSafeRedirect(t *testing.T) {
	for in, want := range map[string]string{
		"/runs?x=1": "/runs?x=1", "": "/", "https://evil.example": "/", "//evil.example": "/",
		"/\\evil.example": "/", "/./\\evil": "/", "runs": "/",
	} {
		if got := safeRedirect(in); got != want {
			t.Errorf("safeRedirect(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMemoryStore(t *testing.T) {
	m := NewMemoryStore(time.Hour, 10*time.Minute)
	now := time.Now()
	m.now = func() time.Time { return now }
	sid, _, _ := m.Create(Identity{User: "u"})
	now = now.Add(9 * time.Minute)
	if _, err := m.Get(sid); err != nil {
		t.Fatal("a used session ended")
	}
	now = now.Add(10 * time.Minute)
	if _, err := m.Get(sid); err == nil {
		t.Error("an idle session did not end")
	}
	sid, _, _ = m.Create(Identity{User: "u"})
	for range 7 {
		now = now.Add(9 * time.Minute)
		_, _ = m.Get(sid)
	}
	if _, err := m.Get(sid); err == nil {
		t.Error("a session lived longer than its TTL")
	}
}

// downStore is a session store whose database does not answer.
type downStore struct{ Store }

func (downStore) Create(Identity) (string, *Session, error) {
	return "", nil, errors.New("connection refused")
}

func TestLoginWithoutTheSessionStore(t *testing.T) {
	e := newEnvWithStore(t, true, downStore{NewMemoryStore(time.Hour, time.Hour)})
	code, body := e.do(t, http.MethodGet, "/auth/login", nil)
	if code != http.StatusServiceUnavailable || !strings.Contains(body, "try again") {
		t.Errorf("login: %d %q", code, body)
	}
}

// flakyStore is a session store whose database stops to answer when down is set.
type flakyStore struct {
	Store
	down *bool
}

func (f flakyStore) Get(id string) (*Session, error) {
	if *f.down {
		return nil, errors.New("connection refused")
	}
	return f.Store.Get(id)
}

// A store that does not answer is not the end of the session: the user keeps the session and tries again later.
func TestSessionStoreOutage(t *testing.T) {
	down := false
	e := newEnvWithStore(t, true, flakyStore{NewMemoryStore(time.Hour, time.Hour), &down})
	e.do(t, http.MethodGet, "/auth/login", nil)
	down = true
	if code, _ := e.do(t, http.MethodGet, "/auth/me", nil); code != http.StatusServiceUnavailable {
		t.Errorf("me during the outage: %d", code)
	}
	if code, body := e.do(t, http.MethodGet, "/api", nil); code != http.StatusUnauthorized || !strings.Contains(body, "try again") {
		t.Errorf("API during the outage: %d %q", code, body)
	}
	down = false
	if code, _ := e.do(t, http.MethodGet, "/auth/me", nil); code != http.StatusOK {
		t.Errorf("me after the outage: %d", code)
	}
}
