package auth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	loginTTL = 10 * time.Minute
	// maxPending limits the logins that wait for the provider, so that /auth/login cannot fill the memory.
	maxPending = 10000
)

// OIDC handles the login with an OIDC provider and authenticates requests.
type OIDC struct {
	cfg      *Config
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	// bearer accepts ID tokens for the client ID and the extra audiences.
	bearer *oidc.IDTokenVerifier
	client *http.Client
	store  Store
	now    func() time.Time

	mu      sync.Mutex
	pending map[string]pendingLogin
	groups  groupCache
}

type pendingLogin struct {
	verifier, nonce, redirect string
	expires                   time.Time
}

// NewOIDC discovers the provider and returns the login handler.
func NewOIDC(ctx context.Context, cfg *Config, store Store) (*OIDC, error) {
	hc, err := httpClient(cfg.OIDC.CAFile)
	if err != nil {
		return nil, err
	}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, hc), cfg.OIDC.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discover the OIDC provider: %w", err)
	}
	o := &OIDC{
		cfg: cfg, client: hc, store: store, now: time.Now, pending: map[string]pendingLogin{},
		oauth: oauth2.Config{
			ClientID: cfg.OIDC.ClientID, ClientSecret: cfg.OIDC.clientSecret, RedirectURL: cfg.OIDC.RedirectURL,
			Endpoint: provider.Endpoint(), Scopes: cfg.OIDC.Scopes,
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.OIDC.ClientID}),
		bearer:   provider.Verifier(&oidc.Config{SkipClientIDCheck: true}),
	}
	return o, nil
}

func httpClient(caFile string) (*http.Client, error) {
	if caFile == "" {
		return &http.Client{Timeout: 30 * time.Second}, nil
	}
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read the provider CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("the provider CA file has no certificate")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return &http.Client{Timeout: 30 * time.Second, Transport: tr}, nil
}

// Handler serves /auth/login, /auth/callback, /auth/logout, /auth/me, /auth/provider and /auth/cli.
func (o *OIDC) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/login", o.login)
	mux.HandleFunc("GET /auth/callback", o.callback)
	mux.HandleFunc("POST /auth/logout", o.logout)
	mux.HandleFunc("GET /auth/me", o.me)
	mux.HandleFunc("GET /auth/provider", o.provider)
	mux.HandleFunc("GET /auth/cli", o.cli)
	return mux
}

// login starts the authorization code flow with PKCE and a nonce.
func (o *OIDC) login(w http.ResponseWriter, r *http.Request) {
	state, err1 := randomToken()
	nonce, err2 := randomToken()
	if err := errors.Join(err1, err2); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	p := pendingLogin{verifier: oauth2.GenerateVerifier(), nonce: nonce, redirect: safeRedirect(r.URL.Query().Get("redirect")),
		expires: o.now().Add(loginTTL)}
	if !o.addPending(state, p) {
		http.Error(w, "too many logins at the same time: try again later", http.StatusServiceUnavailable)
		return
	}
	// The cookie binds the state to this browser, so that a stolen callback URL does not work in another browser.
	http.SetCookie(w, o.cookie(loginCookie(o.cfg.InsecureCookies), state, loginTTL))
	url := o.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(p.verifier), oidc.Nonce(nonce))
	http.Redirect(w, r, url, http.StatusFound)
}

func (o *OIDC) addPending(state string, p pendingLogin) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	now := o.now()
	for k, v := range o.pending {
		if now.After(v.expires) {
			delete(o.pending, k)
		}
	}
	if len(o.pending) >= maxPending {
		return false
	}
	o.pending[state] = p
	return true
}

func (o *OIDC) takePending(state string) (pendingLogin, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	p, ok := o.pending[state]
	delete(o.pending, state)
	return p, ok && o.now().Before(p.expires)
}

// callback ends the flow: it checks the state, exchanges the code, verifies the ID token and starts a session.
func (o *OIDC) callback(w http.ResponseWriter, r *http.Request) {
	log := logf.FromContext(r.Context()).WithName("login")
	q := r.URL.Query()
	c, err := r.Cookie(loginCookie(o.cfg.InsecureCookies))
	http.SetCookie(w, o.cookie(loginCookie(o.cfg.InsecureCookies), "", -1))
	state := q.Get("state")
	p, ok := o.takePending(state)
	if err != nil || c.Value != state || !ok {
		http.Error(w, "the login expired or did not start in this browser: log in again", http.StatusBadRequest)
		return
	}
	if e := q.Get("error"); e != "" {
		log.Info("The provider refused the login", "error", e)
		http.Error(w, "the identity provider refused the login", http.StatusUnauthorized)
		return
	}
	id, err := o.exchange(r.Context(), q.Get("code"), p)
	if err != nil {
		log.Info("A login failed", "reason", err.Error())
		http.Error(w, "the login failed", http.StatusUnauthorized)
		return
	}
	sid, s, err := o.store.Create(*id)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, o.cookie(SessionCookie(o.cfg.InsecureCookies), sid, time.Until(s.ExpiresAt)))
	log.Info("A user logged in", "user", id.User)
	http.Redirect(w, r, p.redirect, http.StatusFound)
}

func (o *OIDC) exchange(ctx context.Context, code string, p pendingLogin) (*Identity, error) {
	ctx = oidc.ClientContext(ctx, o.client)
	tok, err := o.oauth.Exchange(ctx, code, oauth2.VerifierOption(p.verifier))
	if err != nil {
		return nil, fmt.Errorf("exchange the code: %w", err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok {
		return nil, errors.New("the token response has no ID token")
	}
	idt, err := o.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("verify the ID token: %w", err)
	}
	if idt.Nonce != p.nonce {
		return nil, errors.New("the ID token has a wrong nonce")
	}
	var claims map[string]any
	if err := idt.Claims(&claims); err != nil {
		return nil, fmt.Errorf("read the claims: %w", err)
	}
	if o.cfg.OIDC.Entra != nil {
		if err := o.checkEntra(ctx, claims, tok.AccessToken); err != nil {
			return nil, err
		}
	}
	return identityFromClaims(claims, &o.cfg.OIDC)
}

func (o *OIDC) logout(w http.ResponseWriter, r *http.Request) {
	if _, err := o.sessionAuth(r); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	c, _ := r.Cookie(SessionCookie(o.cfg.InsecureCookies))
	o.store.Delete(c.Value)
	http.SetCookie(w, o.cookie(SessionCookie(o.cfg.InsecureCookies), "", -1))
	w.WriteHeader(http.StatusNoContent)
}

// provider tells the login page which identity provider the button opens: "microsoft" for Entra ID, else "oidc".
// The login redirect shows the provider anyway, so this needs no session.
func (o *OIDC) provider(w http.ResponseWriter, _ *http.Request) {
	name := "oidc"
	if o.cfg.OIDC.Entra != nil {
		name = "microsoft"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"provider": name})
}

// me returns the user, the groups and the CSRF token of the session, for the UI.
func (o *OIDC) me(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(SessionCookie(o.cfg.InsecureCookies))
	var s *Session
	if err == nil {
		s, err = o.store.Get(c.Value)
	}
	if err != nil {
		http.Error(w, "log in first", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"user": s.Identity.User, claimGroups: s.Identity.Groups, "csrfToken": s.CSRF, "expiresAt": s.ExpiresAt,
	})
}

// cli tells the CLI how to log in with the device flow. It has no secret.
func (o *OIDC) cli(w http.ResponseWriter, _ *http.Request) {
	if o.cfg.OIDC.CLIClientID == "" {
		http.Error(w, "the CLI login is not configured: use a service account token", http.StatusNotFound)
		return
	}
	scopes := append([]string{"offline_access"}, o.cfg.OIDC.Scopes...)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"issuer": o.cfg.OIDC.Issuer, "clientID": o.cfg.OIDC.CLIClientID, "scopes": scopes})
}

func (o *OIDC) cookie(name, value string, maxAge time.Duration) *http.Cookie {
	c := &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: !o.cfg.InsecureCookies,
		SameSite: http.SameSiteLaxMode, MaxAge: int(maxAge.Seconds())}
	if maxAge < 0 {
		c.MaxAge = -1
	}
	return c
}

// SessionCookie is the name of the session cookie. The "__Host-" prefix makes the browser keep it on this host only.
func SessionCookie(insecure bool) string {
	if insecure {
		return "chronicle-session"
	}
	return "__Host-chronicle-session"
}

func loginCookie(insecure bool) string {
	if insecure {
		return "chronicle-login"
	}
	return "__Host-chronicle-login"
}

// unsafeRedirect finds a slash or backslash followed by another one, which browsers can read as another host.
var unsafeRedirect = regexp.MustCompile(`[/\\](?:[\s\v]*|\.{1,2})[/\\]`)

// safeRedirect permits only a local path, for example "/runs". Other values become "/".
func safeRedirect(s string) string {
	if !strings.HasPrefix(s, "/") || strings.HasPrefix(s, "//") || unsafeRedirect.MatchString(s) {
		return "/"
	}
	return s
}
