package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/gen/chronicle/v1/chroniclev1connect"
	"github.com/Thinnesen/chronicle/internal/artifact"
	"github.com/Thinnesen/chronicle/internal/auth"
)

// fakeStores is one repository for all namespaces.
type fakeStores map[string]string

func (f fakeStores) Open(context.Context, string) (ArtifactStore, error) { return f, nil }

func (f fakeStores) Get(_ context.Context, key string) (io.ReadCloser, int64, error) {
	v, ok := f[key]
	if !ok {
		return nil, 0, artifact.ErrNotFound
	}
	return io.NopCloser(strings.NewReader(v)), int64(len(v)), nil
}

func TestArtifactsAndArchivedLogs(t *testing.T) {
	setup(t)
	ctx := context.Background()
	wf := &v1alpha1.Workflow{Name: "files", Namespace: "team-a", Spec: v1alpha1.WorkflowSpec{WorkflowTemplateSpec: v1alpha1.WorkflowTemplateSpec{
		Entrypoint: "c", Templates: []v1alpha1.Template{{Name: "c", Container: &v1alpha1.ContainerTemplate{Image: "x"}}},
	}}}
	if err := admin.Create(ctx, wf); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Delete(ctx, wf) })
	wf.Status.Nodes = map[string]v1alpha1.NodeStatus{"files-1": {ID: "files-1", Name: "files", Type: v1alpha1.NodePod, PodName: "files-1",
		LogKey: "p/main.log", Outputs: &v1alpha1.Outputs{Artifacts: []v1alpha1.Artifact{{Name: "data", Key: "p/data.tgz"}}}}}
	if err := admin.Status().Update(ctx, wf); err != nil {
		t.Fatal(err)
	}
	stores := fakeStores{"p/data.tgz": "archive bytes", "p/main.log": "one\ntwo\nthree\n"}
	h, err := NewHandler(Options{Config: cfg, Scheme: scheme, Authenticators: []auth.Authenticator{headerAuth{}}, Artifacts: stores})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	download := func(user, name string) (*http.Response, string) {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+ArtifactPath+"team-a/files/files-1/"+name, nil)
		req.Header.Set("X-Test-User", user)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		return resp, string(body)
	}
	if resp, body := download("alice", "data"); resp.StatusCode != http.StatusOK || body != "archive bytes" ||
		resp.Header.Get("Content-Disposition") != `attachment; filename="data.tgz"` {
		t.Errorf("download: %d %q %v", resp.StatusCode, body, resp.Header)
	}
	for user, want := range map[string]int{"bob": http.StatusForbidden, "": http.StatusUnauthorized} {
		if resp, _ := download(user, "data"); resp.StatusCode != want {
			t.Errorf("user %q: status %d, want %d", user, resp.StatusCode, want)
		}
	}
	if resp, _ := download("alice", "nope"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown artifact: status %d", resp.StatusCode)
	}

	// The Pod does not exist, so the log comes from the archive.
	c := chroniclev1connect.NewLogServiceClient(srv.Client(), srv.URL)
	req := connect.NewRequest(&chroniclev1.StreamLogsRequest{Namespace: "team-a", Workflow: "files", TailLines: 2})
	req.Header().Set("X-Test-User", "alice")
	s, err := c.StreamLogs(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for s.Receive() {
		if !s.Msg().Archived {
			t.Error("a line from the archive must be marked")
		}
		lines = append(lines, s.Msg().Line)
	}
	if s.Err() != nil || strings.Join(lines, ",") != "two,three" {
		t.Errorf("archived log: %v %q", s.Err(), lines)
	}
}
