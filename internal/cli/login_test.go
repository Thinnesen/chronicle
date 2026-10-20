package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeOIDC is an identity provider with the device flow. It gives the ID token id-1 for the device code and id-2 for
// the refresh token.
func fakeOIDC(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	reply := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("/auth/cli", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, map[string]any{"issuer": srv.URL, "clientID": "chronicle-cli", "scopes": []string{"openid", "offline_access"}})
	})
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, map[string]any{"issuer": srv.URL, "authorization_endpoint": srv.URL + "/auth", "token_endpoint": srv.URL + "/token",
			"device_authorization_endpoint": srv.URL + "/device", "jwks_uri": srv.URL + "/keys"})
	})
	mux.HandleFunc("/device", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, map[string]any{"device_code": "dev", "user_code": "ABCD-EFGH", "verification_uri": srv.URL + "/verify", "interval": 1, "expires_in": 60})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch {
		case r.Form.Get("device_code") == "dev":
			reply(w, map[string]any{"access_token": "a1", "token_type": "Bearer", "id_token": "id-1", "refresh_token": "r1", "expires_in": 30})
		case r.Form.Get("refresh_token") == "r1":
			reply(w, map[string]any{"access_token": "a2", "token_type": "Bearer", "id_token": "id-2", "expires_in": 3600})
		default:
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		}
	})
	return srv
}

// TestDeviceLogin logs in with the device flow, keeps the ID token, and renews it before it ends.
func TestDeviceLogin(t *testing.T) {
	srv := fakeOIDC(t)
	path := filepath.Join(t.TempDir(), "config.json")
	var out bytes.Buffer
	cmd := NewCommand()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--config", path, "login", "--server", srv.URL + "/", "-n", "team-a"})
	cmd.SetContext(context.Background())
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "type the code ABCD-EFGH") || !strings.Contains(out.String(), "Logged in to "+srv.URL+".") {
		t.Errorf("output:\n%s", out.String())
	}
	cfg, err := loadConfig(path)
	if err != nil || cfg.Token != "id-1" || cfg.RefreshToken != "r1" || cfg.Issuer != srv.URL || cfg.Namespace != "team-a" {
		t.Fatalf("config %+v, %v", cfg, err)
	}
	// The ID token ends in 30 s, so the next call renews it.
	tok, err := (&app{cfg: cfg, configPath: path}).token(context.Background())
	if err != nil || tok != "id-2" || time.Until(cfg.Expiry) < 59*time.Minute {
		t.Errorf("renewed token %q, expiry %v, %v", tok, cfg.Expiry, err)
	}
	if saved, err := loadConfig(path); err != nil || saved.Token != "id-2" || saved.RefreshToken != "r1" {
		t.Errorf("saved config %+v, %v", saved, err)
	}
}
