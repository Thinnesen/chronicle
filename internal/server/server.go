package server

import (
	"fmt"
	"net/http"

	"connectrpc.com/connect"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Thinnesen/chronicle/gen/chronicle/v1/chroniclev1connect"
	"github.com/Thinnesen/chronicle/internal/auth"
)

// Options configure the server.
type Options struct {
	// Config is the Kubernetes client configuration of the server. The server impersonates the callers with it.
	Config *rest.Config
	Scheme *runtime.Scheme
	// Authenticators find the identity of a request, in order.
	Authenticators []auth.Authenticator
}

// NewHandler returns the HTTP handler of the API.
func NewHandler(opts Options) (http.Handler, error) {
	clients, err := newClientFactory(opts.Config, opts.Scheme)
	if err != nil {
		return nil, fmt.Errorf("create the client factory: %w", err)
	}
	own, err := client.New(opts.Config, client.Options{Scheme: opts.Scheme, Mapper: clients.mapper})
	if err != nil {
		return nil, fmt.Errorf("create the server client: %w", err)
	}
	mux := http.NewServeMux()
	limit := connect.WithReadMaxBytes(4 << 20)
	workflows := &workflowService{clients: clients}
	path, h := chroniclev1connect.NewWorkflowServiceHandler(workflows, limit)
	mux.Handle(path, authenticate(opts.Authenticators, h))
	path, h = chroniclev1connect.NewLogServiceHandler(&logService{workflows: workflows, open: clients.podLogs}, limit)
	mux.Handle(path, authenticate(opts.Authenticators, h))
	path, h = chroniclev1connect.NewNamespaceServiceHandler(&namespaceService{clients: clients, own: own, cache: map[string]cachedAccess{}}, limit)
	mux.Handle(path, authenticate(opts.Authenticators, h))
	return mux, nil
}
