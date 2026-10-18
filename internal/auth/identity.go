package auth

import (
	"context"
	"net/http"
)

// Identity is an authenticated caller, mapped to a Kubernetes user and groups.
type Identity struct {
	User   string   `json:"user"`
	Groups []string `json:"groups,omitempty"`
	// ServiceAccount is true when a TokenReview confirmed the identity as a Kubernetes service account.
	ServiceAccount bool `json:"-"`
}

// Authenticator finds the identity of an HTTP request. It returns nil and no error when the request has no credentials of its kind.
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
