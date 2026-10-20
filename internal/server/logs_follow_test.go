package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/gen/chronicle/v1/chroniclev1connect"
	"github.com/Thinnesen/chronicle/internal/auth"
)

// TestFollowLogs follows a run in which a step starts after the request. The stream waits for the step and ends with
// the run.
func TestFollowLogs(t *testing.T) {
	setup(t)
	followPoll = 50 * time.Millisecond
	ctx := context.Background()
	wf := &v1alpha1.Workflow{Name: "follow", Namespace: "team-a", Spec: v1alpha1.WorkflowSpec{WorkflowTemplateSpec: v1alpha1.WorkflowTemplateSpec{
		Entrypoint: "c", Templates: []v1alpha1.Template{{Name: "c", Container: &v1alpha1.ContainerTemplate{Image: "x"}}},
	}}}
	if err := admin.Create(ctx, wf); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Delete(ctx, wf) })
	setNodes := func(phase v1alpha1.WorkflowPhase, second v1alpha1.NodePhase) {
		t.Helper()
		if err := admin.Get(ctx, client.ObjectKeyFromObject(wf), wf); err != nil {
			t.Fatal(err)
		}
		wf.Status.Phase = phase
		wf.Status.Nodes = map[string]v1alpha1.NodeStatus{
			"f-1": {ID: "f-1", Name: "follow.a", Type: v1alpha1.NodePod, PodName: "f-1", Phase: v1alpha1.NodeRunning},
			"f-2": {ID: "f-2", Name: "follow.b", Type: v1alpha1.NodePod, PodName: "f-2", Phase: second},
		}
		if err := admin.Status().Update(ctx, wf); err != nil {
			t.Fatal(err)
		}
	}
	setNodes(v1alpha1.WorkflowRunning, v1alpha1.NodePending)

	clients, err := newClientFactory(cfg, scheme)
	if err != nil {
		t.Fatal(err)
	}
	fake := func(_ context.Context, _, pod string, _ *corev1.PodLogOptions) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("line of " + pod + "\n")), nil
	}
	path, h := chroniclev1connect.NewLogServiceHandler(&logService{workflows: &workflowService{clients: clients}, open: fake})
	mux := http.NewServeMux()
	mux.Handle(path, authenticate([]auth.Authenticator{headerAuth{}}, h))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	req := connect.NewRequest(&chroniclev1.StreamLogsRequest{Namespace: "team-a", Workflow: "follow", Follow: true})
	req.Header().Set("X-Test-User", "alice")
	s, err := chroniclev1connect.NewLogServiceClient(srv.Client(), srv.URL).StreamLogs(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Receive() || s.Msg().Line != "line of f-1" {
		t.Fatalf("first line: %v %+v", s.Err(), s.Msg())
	}
	setNodes(v1alpha1.WorkflowSucceeded, v1alpha1.NodeSucceeded)
	if !s.Receive() || s.Msg().Line != "line of f-2" {
		t.Fatalf("the step that started later: %v %+v", s.Err(), s.Msg())
	}
	if s.Receive() || s.Err() != nil {
		t.Errorf("the stream did not end with the run: %v %+v", s.Err(), s.Msg())
	}
}
