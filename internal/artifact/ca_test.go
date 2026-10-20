package artifact

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCABundle talks to an S3 endpoint with a certificate from a private CA.
func TestCABundle(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"0"`)
	}))
	t.Cleanup(srv.Close)
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
	endpoint := strings.TrimPrefix(srv.URL, "https://")

	if _, err := FromConfigMap(map[string]string{"endpoint": endpoint, "bucket": "b", "caBundle": "no certificate"}); err == nil ||
		!strings.Contains(err.Error(), "caBundle has no PEM certificate") {
		t.Fatalf("a bad bundle: %v", err)
	}
	for _, tt := range []struct {
		ca, err string
	}{{ca, ""}, {"", "certificate"}} {
		r, err := FromConfigMap(map[string]string{"endpoint": endpoint, "bucket": "runs", "region": "us-east-1", "caBundle": tt.ca})
		if err != nil {
			t.Fatal(err)
		}
		s, err := Open(r, "key", "secret")
		if err != nil {
			t.Fatal(err)
		}
		err = s.Put(context.Background(), "k", strings.NewReader("x"), 1, "text/plain")
		if tt.err == "" && err != nil || tt.err != "" && (err == nil || !strings.Contains(err.Error(), tt.err)) {
			t.Errorf("with CA %t: err = %v, want %q", tt.ca != "", err, tt.err)
		}
	}
}
