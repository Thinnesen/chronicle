package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

var _ = Describe("the size limit of a run", func() {
	ctx := context.Background()

	It("fails a run whose status would be too large, before it creates more Pods", func() {
		key := newWorkflow(ctx, `
entrypoint: main
templates:
- name: main
  steps:
  - - {name: first, template: c}
  - - {name: many, template: c, withSequence: {count: 50}}
- name: c
  container: {image: alpine, command: ["true"]}
`)
		r := testReconciler()
		r.MaxObjectBytes = 8 << 10
		wf := reconcileWith(ctx, r, key)
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowRunning))
		first := podsOf(ctx, wf)
		Expect(first).To(HaveLen(1))
		finishPod(ctx, &first[0], 0)

		wf = reconcileWith(ctx, r, key)
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowError))
		Expect(wf.Status.Message).To(ContainSubstring("the run is too large"))
		Expect(wf.Status.FinishedAt).NotTo(BeNil())
		Expect(podsOf(ctx, wf)).To(HaveLen(1), "no Pod of the 50 items")
	})
})
