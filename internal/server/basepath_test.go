package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestBasePath(t *testing.T) {
	files := fstest.MapFS{"index.html": {Data: []byte(`<html><head><base href="/" /></head>app</html>`)}, "assets/a.js": {Data: []byte("js")}}
	inner := http.NewServeMux()
	inner.Handle("/", UIHandler(files, "/chronicle/"))
	inner.HandleFunc("/chronicle.v1.WorkflowService/ListWorkflows", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("api")) })
	h := WithBasePath("/chronicle/", inner)
	for _, tt := range []struct {
		path, want string
		code       int
	}{
		{"/chronicle/", `<base href="/chronicle/" />`, http.StatusOK},
		{"/chronicle/ns/team-a", `<base href="/chronicle/" />`, http.StatusOK},
		{"/chronicle/assets/a.js", "js", http.StatusOK},
		{"/chronicle/chronicle.v1.WorkflowService/ListWorkflows", "api", http.StatusOK},
		{"/chronicle", "", http.StatusMovedPermanently},
		// A login without a target goes to "/". It belongs to the UI under the base path.
		{"/", "", http.StatusFound},
		{"/other", "", http.StatusNotFound},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
		if rec.Code != tt.code || !strings.Contains(rec.Body.String(), tt.want) {
			t.Errorf("%s: %d %q", tt.path, rec.Code, rec.Body.String())
		}
	}
	if got := NormalizeBasePath("chronicle"); got != "/chronicle/" {
		t.Errorf("NormalizeBasePath = %q", got)
	}
}
