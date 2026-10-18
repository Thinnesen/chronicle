package auth

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// CSRFHeader carries the CSRF token of the session on state-changing requests.
const CSRFHeader = "X-CSRF-Token"

// SessionAuthenticator returns the authenticator for the session cookie of the UI.
func (o *OIDC) SessionAuthenticator() Authenticator { return sessionAuth{o} }

// BearerAuthenticator returns the authenticator for "Authorization: Bearer <ID token>", for the CLI and the SDKs.
func (o *OIDC) BearerAuthenticator() Authenticator { return bearerAuth{o} }

type sessionAuth struct{ o *OIDC }

func (a sessionAuth) Authenticate(r *http.Request) (*Identity, error) {
	if _, err := r.Cookie(SessionCookie(a.o.cfg.InsecureCookies)); err != nil {
		return nil, nil
	}
	return a.o.sessionAuth(r)
}

// sessionAuth checks the session cookie, and the CSRF token on every request that is not GET or HEAD.
func (o *OIDC) sessionAuth(r *http.Request) (*Identity, error) {
	c, err := r.Cookie(SessionCookie(o.cfg.InsecureCookies))
	if err != nil {
		return nil, errors.New("log in first")
	}
	s, err := o.store.Get(c.Value)
	if err != nil {
		return nil, errors.New("the session ended: log in again")
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		token := r.Header.Get(CSRFHeader)
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.CSRF)) != 1 {
			return nil, errors.New("the CSRF token is missing or wrong")
		}
	}
	return &s.Identity, nil
}

type bearerAuth struct{ o *OIDC }

func (a bearerAuth) Authenticate(r *http.Request) (*Identity, error) {
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return nil, nil
	}
	idt, err := a.o.bearer.Verify(r.Context(), raw)
	if err != nil {
		return nil, fmt.Errorf("the bearer token is not valid: %w", err)
	}
	audiences := append([]string{a.o.cfg.OIDC.ClientID}, a.o.cfg.OIDC.ExtraAudiences...)
	if !slices.ContainsFunc(idt.Audience, func(aud string) bool { return slices.Contains(audiences, aud) }) {
		return nil, errors.New("the bearer token is for another audience")
	}
	var claims map[string]any
	if err := idt.Claims(&claims); err != nil {
		return nil, fmt.Errorf("read the claims: %w", err)
	}
	return identityFromClaims(claims, &a.o.cfg.OIDC)
}
