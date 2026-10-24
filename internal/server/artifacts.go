package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"connectrpc.com/connect"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Thinnesen/chronicle/internal/artifact"
)

// ArtifactPath is the URL path prefix of artifact downloads: /artifacts/{namespace}/{workflow}/{node}/{name}.
const ArtifactPath = "/artifacts/"

// ArtifactStores opens the artifact repository of a namespace.
type ArtifactStores interface {
	Open(ctx context.Context, namespace string) (ArtifactStore, error)
}

// ArtifactStore reads objects from a repository.
type ArtifactStore interface {
	Get(ctx context.Context, key string) (io.ReadCloser, int64, error)
}

// repositoryStores reads the ConfigMap and the Secret of a namespace with the identity of the server.
// The server calls it only after an access review of the caller.
type repositoryStores struct {
	own client.Client
}

func (s *repositoryStores) Open(ctx context.Context, namespace string) (ArtifactStore, error) {
	key := client.ObjectKey{Namespace: namespace, Name: artifact.Name}
	var cm corev1.ConfigMap
	if err := s.own.Get(ctx, key, &cm); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("the namespace has no artifact repository"))
		}
		return nil, fmt.Errorf("read the artifact repository: %w", err)
	}
	repo, err := artifact.FromConfigMap(cm.Data)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	var secret corev1.Secret
	if err := s.own.Get(ctx, key, &secret); err != nil {
		return nil, fmt.Errorf("read the artifact repository credentials: %w", err)
	}
	return artifact.Open(repo, string(secret.Data[artifact.SecretAccessKey]), string(secret.Data[artifact.SecretSecretKey]))
}

// openStored opens an object of a namespace repository. A missing object is NotFound.
func openStored(ctx context.Context, stores ArtifactStores, namespace, key string) (io.ReadCloser, int64, error) {
	if stores == nil {
		return nil, 0, connect.NewError(connect.CodeNotFound, errors.New("the server has no artifact repositories"))
	}
	store, err := stores.Open(ctx, namespace)
	if err != nil {
		return nil, 0, err
	}
	rc, size, err := store.Get(ctx, key)
	if errors.Is(err, artifact.ErrNotFound) {
		return nil, 0, connect.NewError(connect.CodeNotFound, err)
	}
	if err != nil {
		return nil, 0, connect.NewError(connect.CodeUnavailable, err)
	}
	return rc, size, nil
}

// artifactHandler serves artifact downloads. Getting the run as the caller is the access check.
type artifactHandler struct {
	workflows *workflowService
	stores    ArtifactStores
}

func (h *artifactHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace, name := r.PathValue("namespace"), r.PathValue("name")
	wf, _, err := h.workflows.getAny(ctx, namespace, r.PathValue("workflow"), r.URL.Query().Get("uid"))
	if err != nil {
		httpError(w, err)
		return
	}
	key := ""
	if n, ok := wf.Status.Nodes[r.PathValue("node")]; ok && n.Outputs != nil {
		for _, a := range n.Outputs.Artifacts {
			if a.Name == name {
				key = a.Key
			}
		}
	}
	if key == "" {
		http.Error(w, "the node has no artifact with this name", http.StatusNotFound)
		return
	}
	rc, size, err := openStored(ctx, h.stores, namespace, key)
	if err != nil {
		httpError(w, err)
		return
	}
	defer func() { _ = rc.Close() }()
	hdr := w.Header()
	hdr.Set("Content-Type", "application/gzip")
	hdr.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.tgz"`, name))
	hdr.Set("Content-Length", strconv.FormatInt(size, 10))
	hdr.Set("Cache-Control", "private, no-store")
	hdr.Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, rc)
}

// httpError writes a Connect error as a plain HTTP error.
func httpError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch connect.CodeOf(err) {
	case connect.CodeNotFound:
		status = http.StatusNotFound
	case connect.CodePermissionDenied:
		status = http.StatusForbidden
	case connect.CodeInvalidArgument:
		status = http.StatusBadRequest
	case connect.CodeUnavailable, connect.CodeFailedPrecondition:
		status = http.StatusServiceUnavailable
	}
	msg := "the server could not read the artifact"
	if ce, ok := errors.AsType[*connect.Error](err); ok && status != http.StatusInternalServerError {
		msg = ce.Message()
	} else {
		ctrl.Log.WithName("artifacts").Error(err, "Failed to serve an artifact")
	}
	http.Error(w, msg, status)
}

// canReadLogs tells if the caller may read Pod logs in a namespace.
func canReadLogs(ctx context.Context, c client.Client, namespace string) bool {
	r := &authorizationv1.SelfSubjectAccessReview{Spec: authorizationv1.SelfSubjectAccessReviewSpec{
		ResourceAttributes: &authorizationv1.ResourceAttributes{Namespace: namespace, Verb: "get", Resource: "pods", Subresource: "log"},
	}}
	return c.Create(ctx, r) == nil && r.Status.Allowed
}
