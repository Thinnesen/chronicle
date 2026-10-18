package auth

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Thinnesen/chronicle/internal/auth/oidctest"
)

func TestEntra(t *testing.T) {
	idp := oidctest.New("/tenant-1/v2.0")
	t.Cleanup(idp.Close)
	graphCalls := 0
	idp.Handle("POST /v1.0/me/getMemberObjects", func(w http.ResponseWriter, r *http.Request) {
		graphCalls++
		if r.Header.Get("Authorization") != "Bearer at" {
			http.Error(w, "no token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":["g-1","g-2"]}`))
	})
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cfg := &Config{InsecureCookies: true, OIDC: OIDCConfig{ClientID: "chronicle", RedirectURL: srv.URL + "/auth/callback",
		Entra: &EntraConfig{TenantID: "tenant-1", Authority: idp.URL, GraphURL: idp.URL}}}
	if err := cfg.complete(); err != nil {
		t.Fatal(err)
	}
	o, err := NewOIDC(context.Background(), cfg, NewMemoryStore(time.Hour, time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	mux.Handle("/auth/", o.Handler())

	login := func(claims map[string]any) (map[string]any, int) {
		idp.Claims = claims
		jar, _ := cookiejar.New(nil)
		e := &env{srv: srv, user: &http.Client{Jar: jar}}
		if code, body := e.do(t, http.MethodGet, "/auth/login", nil); code == http.StatusUnauthorized {
			t.Logf("login: %s", body)
		}
		code, _ := e.do(t, http.MethodGet, "/auth/me", nil)
		if code != http.StatusOK {
			return nil, code
		}
		return e.me(t), code
	}

	me, _ := login(map[string]any{"tid": "tenant-1", "oid": "user-1", "groups": []any{"g-9"}})
	if me["user"] != "entra:user-1" || me["groups"].([]any)[0] != "entra:group:g-9" {
		t.Errorf("me = %v", me)
	}

	// Entra sends a source with a legacy endpoint. The server uses Microsoft Graph instead.
	overage := map[string]any{"tid": "tenant-1", "oid": "user-2", "_claim_names": map[string]any{"groups": "src1"},
		"_claim_sources": map[string]any{"src1": map[string]any{"endpoint": "https://graph.windows.net/tenant-1/users/user-2/getMemberObjects"}}}
	for range 2 {
		me, _ = login(overage)
		if groups := me["groups"].([]any); len(groups) != 2 || groups[1] != "entra:group:g-2" {
			t.Errorf("overage groups = %v", groups)
		}
	}
	if graphCalls != 1 {
		t.Errorf("Graph calls = %d, want 1 (the second login uses the cache)", graphCalls)
	}

	if _, code := login(map[string]any{"tid": "tenant-2", "oid": "user-3"}); code != http.StatusUnauthorized {
		t.Errorf("another tenant: %d", code)
	}

	bearer := bearerAuth{o}
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	tok := map[string]any{"tid": "tenant-1", "oid": "user-4", "_claim_names": overage["_claim_names"], "_claim_sources": overage["_claim_sources"]}
	req.Header.Set("Authorization", "Bearer "+idp.IDToken(tok, "chronicle", time.Hour))
	if _, err := bearer.Authenticate(req); err == nil {
		t.Error("a bearer token with a group overage and no cached groups was accepted")
	}
}

func TestEntraConfig(t *testing.T) {
	for _, tid := range []string{"", "common", "organizations"} {
		c := &Config{OIDC: OIDCConfig{ClientID: "c", RedirectURL: "https://x/cb", Entra: &EntraConfig{TenantID: tid}}}
		if err := c.complete(); err == nil {
			t.Errorf("tenant %q was accepted", tid)
		}
	}
	c := &Config{OIDC: OIDCConfig{ClientID: "c", RedirectURL: "https://x/cb", Entra: &EntraConfig{TenantID: "t", GroupsFrom: "roles"}}}
	if err := c.complete(); err != nil || c.OIDC.Issuer != "https://login.microsoftonline.com/t/v2.0" || c.OIDC.GroupsPrefix != "entra:role:" {
		t.Errorf("config = %+v, %v", c.OIDC, err)
	}
}
