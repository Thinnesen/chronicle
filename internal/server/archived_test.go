package server

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/internal/archive"
	"github.com/Thinnesen/chronicle/internal/auth"
)

type fakeReader struct{ rows []archive.Row }

func (f fakeReader) List(_ context.Context, q archive.Query) ([]archive.Row, error) {
	var out []archive.Row
	for _, r := range f.rows {
		if r.Namespace == q.Namespace {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f fakeReader) Get(_ context.Context, namespace, name string) (*v1alpha1.Workflow, error) {
	for _, r := range f.rows {
		if r.Namespace == namespace && r.Name == name {
			return &v1alpha1.Workflow{Name: name, Namespace: namespace, CreationTimestamp: metav1.NewTime(r.CreatedAt),
				Status: v1alpha1.WorkflowStatus{Phase: v1alpha1.WorkflowPhase(r.Phase)}}, nil
		}
	}
	return nil, archive.ErrNotFound
}

func TestArchivedRuns(t *testing.T) {
	setup(t)
	ctx := context.Background()
	old := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	reader := fakeReader{rows: []archive.Row{{UID: "aaaaaaaa-0000-0000-0000-000000000001", Namespace: "team-a", Name: "gone", Phase: "Succeeded", CreatedAt: old}}}
	h, err := NewHandler(Options{Config: cfg, Scheme: scheme, Authenticators: []auth.Authenticator{headerAuth{}}, Archive: reader})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	alice := newClient(srv, "alice")

	list, err := alice.ListWorkflows(ctx, connect.NewRequest(&chroniclev1.ListWorkflowsRequest{Namespace: "team-a"}))
	if err != nil {
		t.Fatal(err)
	}
	last := list.Msg.Workflows[len(list.Msg.Workflows)-1]
	if last.Name != "gone" || !last.Archived {
		t.Errorf("the oldest run must be the archived one: %+v", last)
	}
	sel, err := alice.ListWorkflows(ctx, connect.NewRequest(&chroniclev1.ListWorkflowsRequest{Namespace: "team-a", LabelSelector: "team!=b"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range sel.Msg.Workflows {
		if w.Archived {
			t.Error("a selector that is not an equality must not read the archive")
		}
	}

	got, err := alice.GetWorkflow(ctx, connect.NewRequest(&chroniclev1.GetWorkflowRequest{Namespace: "team-a", Name: "gone"}))
	if err != nil || !got.Msg.Summary.Archived || got.Msg.Summary.Phase != "Succeeded" {
		t.Errorf("get an archived run: %v %+v", err, got)
	}
	if _, err := newClient(srv, "bob").GetWorkflow(ctx, connect.NewRequest(&chroniclev1.GetWorkflowRequest{Namespace: "team-a", Name: "gone"})); code(err) != connect.CodePermissionDenied {
		t.Errorf("bob must not read the archive: %v", err)
	}
	if _, err := alice.GetWorkflow(ctx, connect.NewRequest(&chroniclev1.GetWorkflowRequest{Namespace: "team-a", Name: "never"})); code(err) != connect.CodeNotFound {
		t.Errorf("a run that is nowhere: %v", err)
	}
}

func TestEqualityLabels(t *testing.T) {
	if m, ok := equalityLabels("a=1,b==2"); !ok || m["a"] != "1" || m["b"] != "2" {
		t.Errorf("equality: %v %v", m, ok)
	}
	for _, sel := range []string{"a!=1", "a in (1,2)", "a", "!a", "a=("} {
		if _, ok := equalityLabels(sel); ok {
			t.Errorf("%q was taken as equality", sel)
		}
	}
	if m, ok := equalityLabels(""); !ok || len(m) != 0 {
		t.Errorf("empty: %v %v", m, ok)
	}
}
