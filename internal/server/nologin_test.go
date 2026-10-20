package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNoLoginHandler(t *testing.T) {
	for path, want := range map[string]string{"/auth/provider": `"provider":"none"`, "/auth/me": "no web login"} {
		rec := httptest.NewRecorder()
		NoLoginHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if !strings.Contains(rec.Body.String(), want) || rec.Header().Get("Content-Type") != "application/json" {
			t.Errorf("%s: %d %q", path, rec.Code, rec.Body.String())
		}
	}
}
