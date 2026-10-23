package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Thinnesen/chronicle/internal/archive"
)

// fakeOpenTasks is a task queue with open tasks that records the cancelled ones.
type fakeOpenTasks struct {
	open      []archive.Task
	cancelled map[string]string
}

func (q *fakeOpenTasks) OpenTasks(context.Context, int) ([]archive.Task, error) { return q.open, nil }

func (q *fakeOpenTasks) Cancel(_ context.Context, id, reason string) error {
	q.cancelled[id] = reason
	return nil
}

var _ = Describe("The task sweeper", func() {
	ctx := context.Background()

	It("cancels the open tasks of deleted runs, by UID", func() {
		key := newWorkflow(ctx, "entrypoint: c\ntemplates:\n- {name: c, task: {name: greet}}\n")
		live := reconcileWith(ctx, testReconciler(), key)
		task := func(id, workflow, uid string) archive.Task {
			return archive.Task{ID: id, Namespace: "default", Workflow: workflow, WorkflowUID: uid}
		}
		q := &fakeOpenTasks{cancelled: map[string]string{}, open: []archive.Task{
			task("t-live", live.Name, string(live.UID)),
			task("t-gone", "deleted-run", "00000000-0000-0000-0000-000000000001"),
			// An older run with the same name: the name exists again, but not the run.
			task("t-older", live.Name, "00000000-0000-0000-0000-000000000002"),
		}}
		s := &TaskSweeper{Reader: k8sClient, Tasks: q}
		Expect(s.sweep(ctx)).To(Succeed())
		Expect(q.cancelled).To(Equal(map[string]string{"t-gone": runDeleted, "t-older": runDeleted}))
	})
})
