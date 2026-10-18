// Package server is the chronicle API server. It serves the Connect-RPC API (gRPC and HTTP/JSON).
// Each call runs with the Kubernetes identity of the caller, through impersonation.
package server

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"
)

// Identity is an authenticated caller, mapped to a Kubernetes user and groups.
type Identity struct {
	User   string
	Groups []string
}

// Authenticator finds the identity of an HTTP request. It returns nil and no error when the request has no credentials.
type Authenticator interface {
	Authenticate(r *http.Request) (*Identity, error)
}

type identityKey struct{}

// WithIdentity returns a context with the identity.
func WithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// IdentityFrom returns the identity of the context, or nil.
func IdentityFrom(ctx context.Context) *Identity {
	id, _ := ctx.Value(identityKey{}).(*Identity)
	return id
}

// authenticate wraps a handler. It puts the identity of each request in the context, or answers 401.
func authenticate(authn []Authenticator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, a := range authn {
			id, err := a.Authenticate(r)
			if err != nil {
				writeError(w, r, connect.NewError(connect.CodeUnauthenticated, err))
				return
			}
			if id != nil {
				next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
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
	http.Error(w, err.Message(), http.StatusUnauthorized)
}
