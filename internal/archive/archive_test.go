package archive

import (
	"context"
	"os"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// openTest opens the archive of $CHRONICLE_TEST_POSTGRES with empty tables. See "make postgres-up".
func openTest(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("CHRONICLE_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("CHRONICLE_TEST_POSTGRES is not set: run make postgres-up")
	}
	ctx := context.Background()
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if _, err := s.pool.Exec(ctx, `TRUNCATE workflow_runs, workflow_tries, tasks, sessions, step_cache`); err != nil {
		t.Fatal(err)
	}
	return s
}

func run(uid, name string, phase v1alpha1.WorkflowPhase, created time.Time, labels map[string]string) *v1alpha1.Workflow {
	start := metav1.NewTime(created.Add(time.Second))
	return &v1alpha1.Workflow{
		UID: types.UID(uid), Name: name, Namespace: "team-a", Labels: labels, CreationTimestamp: metav1.NewTime(created),
		Status: v1alpha1.WorkflowStatus{Phase: phase, StartedAt: &start, Message: "msg",
			Nodes: map[string]v1alpha1.NodeStatus{name: {ID: name, Name: name, Type: v1alpha1.NodePod, Phase: v1alpha1.NodeSucceeded}}},
	}
}

func TestStore(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	runs := []*v1alpha1.Workflow{
		run("11111111-1111-1111-1111-111111111111", "a", v1alpha1.WorkflowSucceeded, t0, map[string]string{"team": "data"}),
		run("22222222-2222-2222-2222-222222222222", "b", v1alpha1.WorkflowFailed, t0.Add(time.Hour), nil),
		run("33333333-3333-3333-3333-333333333333", "a", v1alpha1.WorkflowFailed, t0.Add(2*time.Hour), map[string]string{"team": "data"}),
	}
	for _, wf := range runs {
		if err := s.Save(ctx, wf, 1, 2); err != nil {
			t.Fatal(err)
		}
	}
	// A retried run is saved again with its new phase.
	runs[1].Status.Phase = v1alpha1.WorkflowSucceeded
	if err := s.Save(ctx, runs[1], 2, 2); err != nil {
		t.Fatal(err)
	}

	all, err := s.List(ctx, Query{Namespace: "team-a", Limit: 10})
	if err != nil || len(all) != 3 || all[0].UID != "33333333-3333-3333-3333-333333333333" || all[1].Phase != "Succeeded" || all[1].NodesDone != 2 {
		t.Fatalf("list: %v %+v", err, all)
	}
	data, err := s.List(ctx, Query{Namespace: "team-a", Labels: map[string]string{"team": "data"}, Phases: []string{"Succeeded"}, Limit: 10})
	if err != nil || len(data) != 1 || data[0].Name != "a" || data[0].Labels["team"] != "data" || data[0].StartedAt == nil {
		t.Fatalf("filtered: %v %+v", err, data)
	}
	page, err := s.List(ctx, Query{Namespace: "team-a", Limit: 1, After: &Position{CreatedAt: all[0].CreatedAt, Name: all[0].Name}})
	if err != nil || len(page) != 1 || page[0].Name != "b" {
		t.Fatalf("page: %v %+v", err, page)
	}
	other, err := s.List(ctx, Query{Namespace: "team-b", Limit: 10})
	if err != nil || len(other) != 0 {
		t.Fatalf("another namespace: %v %+v", err, other)
	}

	wf, err := s.Get(ctx, "team-a", "a")
	if err != nil || string(wf.UID) != "33333333-3333-3333-3333-333333333333" || len(wf.Status.Nodes) != 1 {
		t.Fatalf("get the newest run: %v %+v", err, wf)
	}
	if _, err := s.Get(ctx, "team-a", "nope"); err != ErrNotFound {
		t.Errorf("get a missing run: %v", err)
	}

	// A second open finds the migrations done.
	again, err := Open(ctx, os.Getenv("CHRONICLE_TEST_POSTGRES"))
	if err != nil {
		t.Fatal(err)
	}
	again.Close()
}
