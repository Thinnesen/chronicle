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
		"index.html":      {Data: []byte("<html>app</html>")},
		"assets/app-1.js": {Data: []byte("js")},
		"favicon.svg":     {Data: []byte("<svg/>")},
	}
	h := UIHandler(files)
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
	rec := httptest.NewRecorder()
	UIHandler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("no UI: %d", rec.Code)
	}
}
