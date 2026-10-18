package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const tokenCacheTTL = time.Minute

// TokenReviewAuthenticator accepts Kubernetes service account tokens that were issued for the given audiences.
// It checks them with a TokenReview. A user creates such a token with "kubectl create token <sa> --audience=chronicle".
type TokenReviewAuthenticator struct {
	Client    client.Client
	Audiences []string

	mu    sync.Mutex
	cache map[[32]byte]cachedIdentity
}

type cachedIdentity struct {
	id      *Identity
	expires time.Time
}

// Authenticate implements Authenticator.
func (a *TokenReviewAuthenticator) Authenticate(r *http.Request) (*Identity, error) {
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return nil, nil
	}
	key := sha256.Sum256([]byte(raw))
	if id, ok := a.cached(key); ok {
		return id, nil
	}
	id, err := a.review(r.Context(), raw)
	if err != nil {
		return nil, err
	}
	a.store(key, id)
	return id, nil
}

func (a *TokenReviewAuthenticator) review(ctx context.Context, raw string) (*Identity, error) {
	tr := &authenticationv1.TokenReview{Spec: authenticationv1.TokenReviewSpec{Token: raw, Audiences: a.Audiences}}
	if err := a.Client.Create(ctx, tr); err != nil {
		return nil, fmt.Errorf("check the token: %w", err)
	}
	if !tr.Status.Authenticated {
		return nil, errors.New("the token is not valid for chronicle: create it with --audience=" + strings.Join(a.Audiences, ","))
	}
	u := tr.Status.User
	if !strings.HasPrefix(u.Username, "system:serviceaccount:") {
		return nil, errors.New("only service account tokens are accepted")
	}
	return &Identity{User: u.Username, Groups: u.Groups, ServiceAccount: true}, nil
}

func (a *TokenReviewAuthenticator) cached(key [32]byte) (*Identity, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, ok := a.cache[key]
	return e.id, ok && time.Now().Before(e.expires)
}

func (a *TokenReviewAuthenticator) store(key [32]byte, id *Identity) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cache == nil || len(a.cache) > 10000 {
		a.cache = map[[32]byte]cachedIdentity{}
	}
	a.cache[key] = cachedIdentity{id: id, expires: time.Now().Add(tokenCacheTTL)}
}

// Impersonable tells if the server may impersonate the identity. Names with the "system:" prefix are
// refused, except the user and the groups of a service account that a TokenReview confirmed.
func (id *Identity) Impersonable() bool {
	isSystem := func(s string) bool { return strings.HasPrefix(s, "system:") }
	if !id.ServiceAccount {
		return !isSystem(id.User) && !slices.ContainsFunc(id.Groups, isSystem)
	}
	parts := strings.Split(id.User, ":")
	if len(parts) != 4 || parts[0] != "system" || parts[1] != "serviceaccount" {
		return false
	}
	allowed := []string{"system:serviceaccounts", "system:serviceaccounts:" + parts[2], "system:authenticated"}
	return !slices.ContainsFunc(id.Groups, func(g string) bool { return isSystem(g) && !slices.Contains(allowed, g) })
}
