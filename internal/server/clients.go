package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/Thinnesen/chronicle/internal/auth"
)

// clientFactory makes Kubernetes clients that impersonate the caller.
// The server never acts with its own permissions for a user.
type clientFactory struct {
	cfg    *rest.Config
	scheme *runtime.Scheme
	mapper meta.RESTMapper
}

func newClientFactory(cfg *rest.Config, scheme *runtime.Scheme) (*clientFactory, error) {
	hc, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, fmt.Errorf("create the HTTP client: %w", err)
	}
	mapper, err := newMapper(cfg, hc)
	if err != nil {
		return nil, err
	}
	return &clientFactory{cfg: cfg, scheme: scheme, mapper: mapper}, nil
}

// clientFor returns a client that impersonates the identity of the context.
func (f *clientFactory) clientFor(ctx context.Context) (client.Client, error) {
	id := auth.IdentityFrom(ctx)
	if id == nil || id.User == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("log in first"))
	}
	// A second check after the claim mapping: the server never impersonates a Kubernetes system name.
	if isSystem(id.User) || slices.ContainsFunc(id.Groups, isSystem) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("system names cannot be used"))
	}
	cfg := rest.CopyConfig(f.cfg)
	cfg.Impersonate = rest.ImpersonationConfig{UserName: id.User, Groups: id.Groups}
	hc, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, fmt.Errorf("create the HTTP client: %w", err)
	}
	return client.New(cfg, client.Options{Scheme: f.scheme, Mapper: f.mapper, HTTPClient: hc})
}

func isSystem(name string) bool { return strings.HasPrefix(name, "system:") }

func newMapper(cfg *rest.Config, hc *http.Client) (meta.RESTMapper, error) {
	m, err := apiutil.NewDynamicRESTMapper(cfg, hc)
	if err != nil {
		return nil, fmt.Errorf("create the REST mapper: %w", err)
	}
	return m, nil
}
