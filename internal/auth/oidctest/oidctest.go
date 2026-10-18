// Package oidctest is a small OIDC provider for tests. It signs ID tokens with RS256 and checks PKCE.
package oidctest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// Provider is a test OIDC provider. The authorize endpoint logs in the user with Claims at once.
type Provider struct {
	*httptest.Server
	key *rsa.PrivateKey

	// Issuer is the issuer URL: the server URL and the path of New.
	Issuer string

	mu sync.Mutex
	// Claims are the claims of the next login.
	Claims map[string]any
	// Users are claims by user name. When it is not empty, the authorize endpoint shows a form to pick a user.
	Users map[string]map[string]any
	// WrongNonce makes the next ID tokens carry a wrong nonce.
	WrongNonce bool
	codes      map[string]grant
	mux        *http.ServeMux
}

type grant struct {
	nonce, challenge, clientID string
	claims                     map[string]any
}

// New starts a provider with the issuer at the server URL and the path, for example "/tenant/v2.0". Close it after the test.
func New(path string) *Provider {
	p := NewHandler(path)
	p.Server = httptest.NewServer(p.mux)
	p.Issuer = p.URL + path
	return p
}

// NewHandler returns a provider without a server. Set Issuer, then serve Mux.
func NewHandler(path string) *Provider {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	p := &Provider{key: key, codes: map[string]grant{}, Claims: map[string]any{}, mux: http.NewServeMux()}
	p.mux.HandleFunc("GET "+path+"/.well-known/openid-configuration", p.discovery)
	p.mux.HandleFunc("GET "+path+"/keys", p.keys)
	p.mux.HandleFunc("GET "+path+"/authorize", p.authorize)
	p.mux.HandleFunc("POST "+path+"/authorize", p.authorize)
	p.mux.HandleFunc("POST "+path+"/token", p.token)
	return p
}

// Mux is the HTTP handler of the provider.
func (p *Provider) Mux() *http.ServeMux { return p.mux }

// Handle adds a handler to the provider, for example a mock Graph API.
func (p *Provider) Handle(pattern string, h http.HandlerFunc) {
	p.mux.HandleFunc(pattern, h)
}

func (p *Provider) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer": p.Issuer, "authorization_endpoint": p.Issuer + "/authorize", "token_endpoint": p.Issuer + "/token",
		"jwks_uri": p.Issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"},
		"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
	})
}

func (p *Provider) keys(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &p.key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
}

func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		http.Error(w, "PKCE with S256 is required", http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	claims := maps.Clone(p.Claims)
	if len(p.Users) > 0 {
		user, ok := p.Users[r.FormValue("user")]
		if r.Method == http.MethodGet || !ok {
			p.mu.Unlock()
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprintf(w, `<form method="post" action="%s"><input name="user"></form>`, html.EscapeString(r.URL.String()))
			return
		}
		claims = maps.Clone(user)
	}
	code := rand.Text()
	p.codes[code] = grant{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), clientID: q.Get("client_id"), claims: claims}
	p.mu.Unlock()
	back, _ := url.Parse(q.Get("redirect_uri"))
	v := back.Query()
	v.Set("code", code)
	v.Set("state", q.Get("state"))
	back.RawQuery = v.Encode()
	http.Redirect(w, r, back.String(), http.StatusFound)
}

func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	p.mu.Lock()
	g, ok := p.codes[r.Form.Get("code")]
	delete(p.codes, r.Form.Get("code"))
	wrongNonce := p.WrongNonce
	p.mu.Unlock()
	sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
	if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}
	claims := maps.Clone(g.claims)
	claims["nonce"] = g.nonce
	if wrongNonce {
		claims["nonce"] = "wrong"
	}
	writeJSON(w, map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 300, "id_token": p.IDToken(claims, g.clientID, time.Hour)})
}

// IDToken returns a signed ID token with the claims, for an audience, that expires after ttl.
func (p *Provider) IDToken(claims map[string]any, audience string, ttl time.Duration) string {
	c := maps.Clone(claims)
	now := time.Now()
	c["iss"], c["aud"], c["iat"], c["exp"] = p.Issuer, audience, now.Unix(), now.Add(ttl).Unix()
	if _, ok := c["sub"]; !ok {
		c["sub"] = "subject-1"
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: p.key}, (&jose.SignerOptions{}).WithHeader("kid", "k1"))
	if err != nil {
		panic(err)
	}
	payload, _ := json.Marshal(c)
	sig, err := signer.Sign(payload)
	if err != nil {
		panic(err)
	}
	s, _ := sig.CompactSerialize()
	return s
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
