package server

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/internal/auth"
)

const (
	accessCacheTTL = 30 * time.Second
	// accessWorkers limits the access reviews that run at the same time for one call.
	accessWorkers = 16
)

// namespaceService finds the namespaces that a caller can use, with access reviews as the caller.
type namespaceService struct {
	clients *clientFactory
	// own is the client of the server. It only lists namespace names.
	own client.Reader

	mu    sync.Mutex
	cache map[string]cachedAccess
}

type cachedAccess struct {
	namespaces []*chroniclev1.NamespaceAccess
	expires    time.Time
}

func (s *namespaceService) ListNamespaces(ctx context.Context, _ *connect.Request[chroniclev1.ListNamespacesRequest]) (*connect.Response[chroniclev1.ListNamespacesResponse], error) {
	result, err := s.access(ctx)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&chroniclev1.ListNamespacesResponse{Namespaces: result}), nil
}

// access returns the namespaces where the caller can list Workflows.
func (s *namespaceService) access(ctx context.Context) ([]*chroniclev1.NamespaceAccess, error) {
	c, err := s.clients.clientFor(ctx)
	if err != nil {
		return nil, err
	}
	key := cacheKey(auth.IdentityFrom(ctx))
	if hit, ok := s.cached(key); ok {
		return hit, nil
	}
	var list corev1.NamespaceList
	if err := s.own.List(ctx, &list); err != nil {
		return nil, toConnect(err)
	}
	result := make([]*chroniclev1.NamespaceAccess, len(list.Items))
	var wg sync.WaitGroup
	sem := make(chan struct{}, accessWorkers)
	for i, ns := range list.Items {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			if allowed(ctx, c, ns.Name, "list") {
				result[i] = &chroniclev1.NamespaceAccess{Name: ns.Name, CanSubmit: allowed(ctx, c, ns.Name, "create")}
			}
		})
	}
	wg.Wait()
	result = slices.DeleteFunc(result, func(a *chroniclev1.NamespaceAccess) bool { return a == nil })
	s.store(key, result)
	return result, nil
}

// allowed asks the API server if the caller (the impersonated client) can do the verb on Workflows in the namespace.
func allowed(ctx context.Context, c client.Client, namespace, verb string) bool {
	r := &authorizationv1.SelfSubjectAccessReview{Spec: authorizationv1.SelfSubjectAccessReviewSpec{
		ResourceAttributes: &authorizationv1.ResourceAttributes{
			Namespace: namespace, Verb: verb, Group: "chronicle.thinnesen.com", Resource: "workflows",
		},
	}}
	return c.Create(ctx, r) == nil && r.Status.Allowed
}

func cacheKey(id *auth.Identity) string {
	groups := slices.Clone(id.Groups)
	slices.Sort(groups)
	return id.User + "\x00" + strings.Join(groups, "\x00")
}

func (s *namespaceService) cached(key string) ([]*chroniclev1.NamespaceAccess, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.cache[key]
	return e.namespaces, ok && time.Now().Before(e.expires)
}

func (s *namespaceService) store(key string, namespaces []*chroniclev1.NamespaceAccess) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, e := range s.cache {
		if now.After(e.expires) {
			delete(s.cache, k)
		}
	}
	s.cache[key] = cachedAccess{namespaces: namespaces, expires: now.Add(accessCacheTTL)}
}
