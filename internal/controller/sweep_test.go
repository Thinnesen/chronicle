package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Thinnesen/chronicle/internal/archive"
)

// fakeOpenTasks is a task queue with runs that have open tasks. It records the cancelled runs.
type fakeOpenTasks struct {
	open      []archive.OpenRun
	cancelled map[string]string
}

func (q *fakeOpenTasks) OpenRuns(context.Context) ([]archive.OpenRun, error) { return q.open, nil }

func (q *fakeOpenTasks) CancelRun(_ context.Context, workflowUID, reason string) error {
	q.cancelled[workflowUID] = reason
	return nil
}

var _ = Describe("The task sweeper", func() {
	ctx := context.Background()

	It("cancels the open tasks of deleted runs, by UID", func() {
		key := newWorkflow(ctx, "entrypoint: c\ntemplates:\n- {name: c, task: {name: greet}}\n")
		live := reconcileWith(ctx, testReconciler(), key)
		gone, older := "00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000002"
		q := &fakeOpenTasks{cancelled: map[string]string{}, open: []archive.OpenRun{
			{Namespace: "default", Workflow: live.Name, WorkflowUID: string(live.UID)},
			{Namespace: "default", Workflow: "deleted-run", WorkflowUID: gone},
			// An older run with the same name: the name exists again, but not the run.
			{Namespace: "default", Workflow: live.Name, WorkflowUID: older},
		}}
		s := &TaskSweeper{Reader: k8sClient, Tasks: q}
		Expect(s.sweep(ctx)).To(Succeed())
		Expect(q.cancelled).To(Equal(map[string]string{gone: runDeleted, older: runDeleted}))
	})
})
