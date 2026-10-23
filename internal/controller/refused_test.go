package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

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
