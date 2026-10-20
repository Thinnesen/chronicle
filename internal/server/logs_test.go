package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/gen/chronicle/v1/chroniclev1connect"
	"github.com/Thinnesen/chronicle/internal/auth"
)

func TestStreamLogs(t *testing.T) {
	setup(t)
	ctx := context.Background()
	wf := &v1alpha1.Workflow{Name: "logs", Namespace: "team-a", Spec: v1alpha1.WorkflowSpec{WorkflowTemplateSpec: v1alpha1.WorkflowTemplateSpec{
		Entrypoint: "c", Templates: []v1alpha1.Template{{Name: "c", Container: &v1alpha1.ContainerTemplate{Image: "x"}}},
	}}}
	if err := admin.Create(ctx, wf); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Delete(ctx, wf) })
	wf.Status.Nodes = map[string]v1alpha1.NodeStatus{
		"logs": {ID: "logs", Name: "logs", Type: v1alpha1.NodeDAG},
		// logs.b started first, so its lines come first.
		"logs-1": {ID: "logs-1", Name: "logs.a", Type: v1alpha1.NodePod, PodName: "logs-1", Phase: v1alpha1.NodeSucceeded, StartedAt: &metav1.Time{Time: time.Unix(20, 0)}},
		"logs-2": {ID: "logs-2", Name: "logs.b", Type: v1alpha1.NodePod, PodName: "logs-2", Phase: v1alpha1.NodeRunning, StartedAt: &metav1.Time{Time: time.Unix(10, 0)}},
		// A step that has not started has no log yet, so a log without follow leaves it out.
		"logs-3": {ID: "logs-3", Name: "logs.c", Type: v1alpha1.NodePod, PodName: "logs-3", Phase: v1alpha1.NodePending, StartedAt: &metav1.Time{Time: time.Unix(30, 0)}},
		// The init container of logs.d failed, so its main container never started and has no log.
		"logs-4": {ID: "logs-4", Name: "logs.d", Type: v1alpha1.NodePod, PodName: "logs-4", Phase: v1alpha1.NodeError, Message: `input artifact "x": not found`,
			StartedAt: &metav1.Time{Time: time.Unix(40, 0)}},
	}
	if err := admin.Status().Update(ctx, wf); err != nil {
		t.Fatal(err)
	}

	clients, err := newClientFactory(cfg, scheme)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var tails []int64
	fake := func(_ context.Context, _, pod string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
		mu.Lock()
		if opts.TailLines != nil {
			tails = append(tails, *opts.TailLines)
		}
		mu.Unlock()
		if pod == "logs-4" {
			return nil, apierrors.NewBadRequest(`container "main" in pod "logs-4" is waiting to start: PodInitializing`)
		}
		return io.NopCloser(strings.NewReader("2026-01-01T00:00:00.5Z hello from " + pod + "\nno time here\n")), nil
	}
	path, h := chroniclev1connect.NewLogServiceHandler(&logService{workflows: &workflowService{clients: clients}, open: fake})
	mux := http.NewServeMux()
	mux.Handle(path, authenticate([]auth.Authenticator{headerAuth{}}, h))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	stream := func(user, node string) ([]*chroniclev1.StreamLogsResponse, error) {
		c := chroniclev1connect.NewLogServiceClient(srv.Client(), srv.URL)
		req := connect.NewRequest(&chroniclev1.StreamLogsRequest{Namespace: "team-a", Workflow: "logs", Node: node, TailLines: 5})
		req.Header().Set("X-Test-User", user)
		s, err := c.StreamLogs(ctx, req)
		if err != nil {
			return nil, err
		}
		var out []*chroniclev1.StreamLogsResponse
		for s.Receive() {
			out = append(out, s.Msg())
		}
		return out, s.Err()
	}

	all, err := stream("alice", "")
	if err != nil || len(all) != 4 {
		t.Fatalf("all nodes: %v, %d lines", err, len(all))
	}
	if all[0].NodeName != "logs.b" {
		t.Errorf("the first line is from %s, want the node that started first", all[0].NodeName)
	}
	one, err := stream("alice", "logs.b")
	if err != nil || len(one) != 2 || one[0].Line != "hello from logs-2" || one[0].Time.AsTime().Nanosecond() != 5e8 || one[1].Time != nil {
		t.Fatalf("one node: %v, %+v", err, one)
	}
	if _, err := stream("alice", "logs.d"); code(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), `the step did not start: input artifact "x": not found`) {
		t.Errorf("a step whose main container never started: %v", err)
	}
	if len(tails) != 5 || tails[0] != 5 {
		t.Errorf("tail lines = %v", tails)
	}
	if _, err := stream("alice", "logs.c"); code(err) != connect.CodeFailedPrecondition {
		t.Errorf("a step that has not started: %v", err)
	}
	if _, err := stream("alice", "nope"); code(err) != connect.CodeNotFound {
		t.Errorf("unknown node: %v", err)
	}
	if _, err := stream("bob", ""); code(err) != connect.CodePermissionDenied {
		t.Errorf("no permission: %v", err)
	}
}
