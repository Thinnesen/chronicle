package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
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
	cfg, err := f.configFor(ctx)
	if err != nil {
		return nil, err
	}
	hc, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, fmt.Errorf("create the HTTP client: %w", err)
	}
	return client.New(cfg, client.Options{Scheme: f.scheme, Mapper: f.mapper, HTTPClient: hc})
}

// podLogs opens the log of the main container of a Pod as the caller.
func (f *clientFactory) podLogs(ctx context.Context, namespace, pod string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
	cfg, err := f.configFor(ctx)
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create the clientset: %w", err)
	}
	return cs.CoreV1().Pods(namespace).GetLogs(pod, opts).Stream(ctx)
}

// configFor returns a configuration that impersonates the identity of the context.
func (f *clientFactory) configFor(ctx context.Context) (*rest.Config, error) {
	id := auth.IdentityFrom(ctx)
	if id == nil || id.User == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("log in first"))
	}
	// A second check after the claim mapping: the server never impersonates a Kubernetes system name,
	// except a service account that a TokenReview confirmed.
	if !id.Impersonable() {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("system names cannot be used"))
	}
	cfg := rest.CopyConfig(f.cfg)
	cfg.Impersonate = rest.ImpersonationConfig{UserName: id.User, Groups: id.Groups}
	return cfg, nil
}

func newMapper(cfg *rest.Config, hc *http.Client) (meta.RESTMapper, error) {
	m, err := apiutil.NewDynamicRESTMapper(cfg, hc)
	if err != nil {
		return nil, fmt.Errorf("create the REST mapper: %w", err)
	}
	return m, nil
}
