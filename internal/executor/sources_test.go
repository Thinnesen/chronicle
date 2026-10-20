package executor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSources writes a raw artifact and downloads an http artifact without an artifact repository.
// A failed download names the URL without its query, because a signed URL has its token there.
func TestSources(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tool" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("#!/bin/sh\necho tool ran\n"))
	}))
	defer srv.Close()
	raw, mode := "a: 1", int32(0o755)
	spec, _ := json.Marshal(ArtifactSpec{Inputs: []File{
		{Name: "cfg", Path: "$DIR/conf/cfg.yaml", Raw: &raw},
		{Name: "tool", Path: "$DIR/bin/tool", URL: srv.URL + "/tool", Mode: &mode},
	}})
	code, out, msg := run(t, `cat $DIR/conf/cfg.yaml && echo && $DIR/bin/tool`, map[string]string{EnvArtifacts: string(spec)})
	if code != 0 || msg.Error != "" || out != "a: 1\ntool ran\n" {
		t.Fatalf("code %d, output %q, message %+v", code, out, msg)
	}

	spec, _ = json.Marshal(ArtifactSpec{Inputs: []File{{Name: "gone", Path: "$DIR/x", URL: srv.URL + "/gone?sig=secret-token"}}})
	code, _, msg = run(t, "true", map[string]string{EnvArtifacts: string(spec)})
	if code == 0 || !strings.Contains(msg.Error, "404") || !strings.Contains(msg.Error, "/gone") || strings.Contains(msg.Error, "secret-token") {
		t.Errorf("code %d, error %q", code, msg.Error)
	}
}
