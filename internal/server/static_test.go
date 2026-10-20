package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestUIHandler(t *testing.T) {
	files := fstest.MapFS{
		"index.html":      {Data: []byte(`<html><meta name="csp-nonce" content="__CSP_NONCE__">app</html>`)},
		"assets/app-1.js": {Data: []byte("js")},
		"favicon.svg":     {Data: []byte("<svg/>")},
	}
	h := UIHandler(files, "/")
	tests := []struct {
		path, body, cache string
	}{
		{"/", "app", "no-cache"},
		{"/ns/team-a/runs/x", "app", "no-cache"},
		{"/assets/app-1.js", "js", "immutable"},
		{"/favicon.svg", "<svg/>", ""},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
		if !strings.Contains(rec.Body.String(), tt.body) || !strings.Contains(rec.Header().Get("Cache-Control"), tt.cache) {
			t.Errorf("%s: %d %q, cache %q", tt.path, rec.Code, rec.Body.String(), rec.Header().Get("Cache-Control"))
		}
		if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Errorf("%s: no CSP", tt.path)
		}
	}
	checkNonce(t, h)
	rec := httptest.NewRecorder()
	UIHandler(nil, "/").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("no UI: %d", rec.Code)
	}
}

// checkNonce checks that each page has a new style nonce. It also checks that the page and the CSP have the same nonce.
func checkNonce(t *testing.T, h http.Handler) {
	t.Helper()
	seen := map[string]bool{}
	for range 2 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		body := rec.Body.String()
		start := strings.Index(body, `content="`) + len(`content="`)
		nonce := body[start : start+strings.Index(body[start:], `"`)]
		if len(nonce) < 16 || nonce == "__CSP_NONCE__" || seen[nonce] {
			t.Fatalf("nonce %q in %q", nonce, body)
		}
		seen[nonce] = true
		if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "style-src 'self' 'nonce-"+nonce+"'") {
			t.Errorf("CSP %q has no nonce %q", csp, nonce)
		}
	}
}
