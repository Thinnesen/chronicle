package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

var _ = Describe("A Pod that Kubernetes refuses", func() {
	ctx := context.Background()

	It("ends the node with the reason instead of a silent retry loop", func() {
		// Kubernetes refuses limits below the requests. The Pod can never start.
		key := newWorkflow(ctx, `
entrypoint: main
templates:
- name: main
  container:
    image: alpine
    command: ["true"]
    resources: {requests: {cpu: "2"}, limits: {cpu: "1"}}
`)
		r := testReconciler()
		wf := reconcileWith(ctx, r, key)
		Expect(podsOf(ctx, wf)).To(BeEmpty())
		node := wf.Status.Nodes[wf.Name]
		Expect(node.Phase).To(Equal(chroniclev1alpha1.NodeError))
		Expect(node.Message).To(ContainSubstring("must be less than or equal to cpu limit"))

		wf = reconcileWith(ctx, r, key)
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowError))
		Expect(wf.Status.Nodes[wf.Name].Message).To(ContainSubstring("must be less than or equal to cpu limit"))
	})
})

var _ = Describe("A task queue that does not answer", func() {
	ctx := context.Background()

	It("shows the wait in the node and tries again soon, without the error backoff", func() {
		queue := &fakeQueue{down: true}
		r := testReconciler()
		r.Tasks = queue
		key := newWorkflow(ctx, `
entrypoint: greet
templates:
- {name: greet, task: {name: greet}}
`)
		res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(And(BeNumerically(">", 0), BeNumerically("<=", refusedRetry)))
		wf := &chroniclev1alpha1.Workflow{}
		Expect(k8sClient.Get(ctx, key, wf)).To(Succeed())
		node := wf.Status.Nodes[wf.Name]
		Expect(node.Phase).To(Equal(chroniclev1alpha1.NodePending))
		Expect(node.Message).To(Equal("waiting: the task queue does not answer"))

		queue.down = false
		wf = reconcileWith(ctx, r, key)
		Expect(queue.added).To(HaveLen(1))
		Expect(wf.Status.Nodes[wf.Name].Message).To(BeEmpty())

		// The run reads its open task from the queue. An outage now also waits without the error backoff.
		queue.down = true
		res, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(refusedRetry))
	})
})
