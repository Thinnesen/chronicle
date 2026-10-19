package server

import (
	"fmt"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
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
	// Archive holds finished runs whose Workflow was deleted, or is nil.
	Archive ArchiveReader
	// Interceptors run around each API call after the audit, for example for traces.
	Interceptors []connect.Interceptor
	// Tasks is the task queue of the task workers, or nil without the archive database.
	Tasks TaskStore
	// Artifacts opens the artifact repositories. The default reads the ConfigMap and the Secret of each namespace.
	Artifacts ArtifactStores
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
	if opts.Artifacts == nil {
		opts.Artifacts = &repositoryStores{own: own}
	}
	mux := http.NewServeMux()
	audit := &auditor{log: ctrl.Log.WithName("audit"), events: own}
	interceptors := append([]connect.Interceptor{audit.interceptor()}, opts.Interceptors...)
	limit := connect.WithHandlerOptions(connect.WithReadMaxBytes(4<<20), connect.WithInterceptors(interceptors...))
	workflows := &workflowService{clients: clients, archive: opts.Archive}
	path, h := chroniclev1connect.NewWorkflowServiceHandler(workflows, limit)
	mux.Handle(path, authenticate(opts.Authenticators, h))
	path, h = chroniclev1connect.NewLogServiceHandler(&logService{workflows: workflows, open: clients.podLogs, stores: opts.Artifacts}, limit)
	mux.Handle(path, authenticate(opts.Authenticators, h))
	if opts.Tasks != nil {
		path, h = chroniclev1connect.NewTaskServiceHandler(&taskService{workflows: workflows, store: opts.Tasks, allowed: map[string]time.Time{}}, limit)
		mux.Handle(path, authenticate(opts.Authenticators, h))
	}
	mux.Handle("GET "+ArtifactPath+"{namespace}/{workflow}/{node}/{name}",
		authenticate(opts.Authenticators, &artifactHandler{workflows: workflows, stores: opts.Artifacts}))
	path, h = chroniclev1connect.NewTemplateServiceHandler(&templateService{clients: clients}, limit)
	mux.Handle(path, authenticate(opts.Authenticators, h))
	path, h = chroniclev1connect.NewCronServiceHandler(&cronService{clients: clients}, limit)
	mux.Handle(path, authenticate(opts.Authenticators, h))
	path, h = chroniclev1connect.NewNamespaceServiceHandler(&namespaceService{clients: clients, own: own, cache: map[string]cachedAccess{}}, limit)
	mux.Handle(path, authenticate(opts.Authenticators, h))
	return mux, nil
}
