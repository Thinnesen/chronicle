// Package server is the chronicle API server. It serves the Connect-RPC API (gRPC and HTTP/JSON).
// Each call runs with the Kubernetes identity of the caller, through impersonation.
package server

import (
	"errors"
	"net/http"

	"connectrpc.com/connect"

	"github.com/Thinnesen/chronicle/internal/auth"
)

// authenticate wraps a handler. It puts the identity of each request in the context, or answers 401.
func authenticate(authn []auth.Authenticator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, a := range authn {
			id, err := a.Authenticate(r)
			if errors.Is(err, auth.ErrUnavailable) {
				// The session may still be valid: the client must try again, not log in again.
				writeError(w, r, connect.NewError(connect.CodeUnavailable, err))
				return
			}
			if err != nil {
				writeError(w, r, connect.NewError(connect.CodeUnauthenticated, err))
				return
			}
			if id != nil {
				next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), id)))
				return
			}
		}
		writeError(w, r, connect.NewError(connect.CodeUnauthenticated, errors.New("log in first")))
	})
}

func writeError(w http.ResponseWriter, r *http.Request, err *connect.Error) {
	ew := connect.NewErrorWriter()
	if ew.IsSupported(r) {
		_ = ew.Write(w, r, err)
		return
	}
	status := http.StatusUnauthorized
	if err.Code() == connect.CodeUnavailable {
		status = http.StatusServiceUnavailable
	}
	http.Error(w, err.Message(), status)
}
