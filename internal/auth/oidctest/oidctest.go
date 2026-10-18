// Package oidctest is a small OIDC provider for tests. It signs ID tokens with RS256 and checks PKCE.
package oidctest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
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

	mu sync.Mutex
	// Claims are the claims of the next login.
	Claims map[string]any
	// WrongNonce makes the next ID tokens carry a wrong nonce.
	WrongNonce bool
	codes      map[string]grant
}

type grant struct {
	nonce, challenge, clientID string
	claims                     map[string]any
}

// New starts a provider. Close it after the test.
func New() *Provider {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	p := &Provider{key: key, codes: map[string]grant{}, Claims: map[string]any{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("GET /keys", p.keys)
	mux.HandleFunc("GET /authorize", p.authorize)
	mux.HandleFunc("POST /token", p.token)
	p.Server = httptest.NewServer(mux)
	return p
}

// Handle adds a handler to the provider, for example a mock Graph API.
func (p *Provider) Handle(pattern string, h http.HandlerFunc) {
	p.Config.Handler.(*http.ServeMux).HandleFunc(pattern, h)
}

func (p *Provider) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer": p.URL, "authorization_endpoint": p.URL + "/authorize", "token_endpoint": p.URL + "/token",
		"jwks_uri": p.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"},
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
	code := rand.Text()
	p.mu.Lock()
	p.codes[code] = grant{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), clientID: q.Get("client_id"), claims: maps.Clone(p.Claims)}
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
	c["iss"], c["aud"], c["iat"], c["exp"] = p.URL, audience, now.Unix(), now.Add(ttl).Unix()
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
