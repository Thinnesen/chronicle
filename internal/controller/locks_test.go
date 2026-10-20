package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/rand"
)

var _ = Describe("template locks", func() {
	ctx := context.Background()

	It("lets one node of the template run at a time across runs", func() {
		lock := "deploy-" + rand.String(5)
		spec := `
entrypoint: main
templates:
- name: main
  synchronization: {mutex: {name: ` + lock + `}}
  container: {image: alpine, command: [deploy]}
`
		r := testReconciler()
		first, second := newWorkflow(ctx, spec), newWorkflow(ctx, spec)
		wf1 := reconcileWith(ctx, r, first) // takes the lock
		Expect(podsOf(ctx, wf1)).To(BeEmpty())
		wf1 = reconcileWith(ctx, r, first) // starts the node
		pods1 := podsOf(ctx, wf1)
		Expect(pods1).To(HaveLen(1))

		reconcileWith(ctx, r, second) // asks for the lock
		wf2 := reconcileWith(ctx, r, second)
		Expect(podsOf(ctx, wf2)).To(BeEmpty())
		Expect(wf2.Status.Nodes[wf2.Name].Message).To(Equal("waiting for the lock " + lock))

		finishPod(ctx, &pods1[0], 0)
		reconcileWith(ctx, r, first) // the node ends and gives the lock back
		reconcileWith(ctx, r, second)
		wf2 = reconcileWith(ctx, r, second)
		Expect(podsOf(ctx, wf2)).To(HaveLen(1))
	})
})
