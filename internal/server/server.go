package server

import (
	"fmt"
	"net/http"

	"connectrpc.com/connect"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"

	"github.com/Thinnesen/chronicle/gen/chronicle/v1/chroniclev1connect"
)

// Options configure the server.
type Options struct {
	// Config is the Kubernetes client configuration of the server. The server impersonates the callers with it.
	Config *rest.Config
	Scheme *runtime.Scheme
	// Authenticators find the identity of a request, in order.
	Authenticators []Authenticator
}

// NewHandler returns the HTTP handler of the API.
func NewHandler(opts Options) (http.Handler, error) {
	clients, err := newClientFactory(opts.Config, opts.Scheme)
	if err != nil {
		return nil, fmt.Errorf("create the client factory: %w", err)
	}
	mux := http.NewServeMux()
	path, h := chroniclev1connect.NewWorkflowServiceHandler(&workflowService{clients: clients}, connect.WithReadMaxBytes(4<<20))
	mux.Handle(path, authenticate(opts.Authenticators, h))
	return mux, nil
}
