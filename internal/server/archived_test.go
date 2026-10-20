package server

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/internal/archive"
	"github.com/Thinnesen/chronicle/internal/auth"
)

type fakeReader struct {
	rows []archive.Row
	down bool // down makes List fail, as with a database that does not answer.
}

func (f fakeReader) List(_ context.Context, q archive.Query) ([]archive.Row, error) {
	if f.down {
		return nil, errors.New("connection refused")
	}
	var out []archive.Row
	for _, r := range f.rows {
		after := q.After == nil || r.CreatedAt.Before(q.After.CreatedAt) || r.CreatedAt.Equal(q.After.CreatedAt) && r.Name > q.After.Name
		if r.Namespace == q.Namespace && after && len(out) < q.Limit {
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
	// Page by page, one run at a time, the list has the same runs in the same order.
	var paged []string
	for token := ""; ; {
		resp, err := alice.ListWorkflows(ctx, connect.NewRequest(&chroniclev1.ListWorkflowsRequest{Namespace: "team-a", Limit: 1, PageToken: token}))
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range resp.Msg.Workflows {
			paged = append(paged, w.Name)
		}
		if token = resp.Msg.NextPageToken; token == "" {
			break
		}
	}
	if len(paged) != len(list.Msg.Workflows) || paged[len(paged)-1] != "gone" {
		t.Errorf("pages %v, list %d runs", paged, len(list.Msg.Workflows))
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
	// A retry continues the Workflow object, which is gone. The answer tells to re-run instead.
	_, err = alice.RetryWorkflow(ctx, connect.NewRequest(&chroniclev1.RetryWorkflowRequest{Namespace: "team-a", Name: "gone"}))
	if code(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "re-run") {
		t.Errorf("retry an archived run: %v", err)
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

func TestListWithoutTheArchive(t *testing.T) {
	setup(t)
	h, err := NewHandler(Options{Config: cfg, Scheme: scheme, Authenticators: []auth.Authenticator{headerAuth{}}, Archive: fakeReader{down: true}})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	// The live runs still come, with a mark that the archive did not answer.
	list, err := newClient(srv, "alice").ListWorkflows(context.Background(), connect.NewRequest(&chroniclev1.ListWorkflowsRequest{Namespace: "team-a"}))
	if err != nil || !list.Msg.ArchiveUnavailable {
		t.Errorf("list: %v %+v", err, list)
	}
}
