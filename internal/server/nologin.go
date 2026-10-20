package server

import (
	"encoding/json"
	"net/http"
)

// NoLoginHandler answers the login paths of a server without a web login. The UI then tells the user to use the CLI
// with a service account token, instead of reading the UI page as a session.
func NoLoginHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/auth/provider" {
			_ = json.NewEncoder(w).Encode(map[string]string{"provider": "none"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "this server has no web login: use the CLI with a service account token"})
	})
}
