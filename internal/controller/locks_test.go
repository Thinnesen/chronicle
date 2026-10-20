package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
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

var _ = Describe("a semaphore with a limit from a ConfigMap", func() {
	ctx := context.Background()

	limits := func(labeled bool, limit string) string {
		cm := &corev1.ConfigMap{Name: "limits-" + rand.String(6), Namespace: "default", Data: map[string]string{"deploy": limit}}
		if labeled {
			cm.Labels = map[string]string{LabelParameters: "true"}
		}
		Expect(k8sClient.Create(ctx, cm)).To(Succeed())
		DeferCleanup(func() error { return k8sClient.Delete(ctx, cm) })
		return cm.Name
	}
	spec := func(lock, cm string) string {
		return `
entrypoint: main
synchronization: {semaphore: {name: ` + lock + `, configMapKeyRef: {name: ` + cm + `, key: deploy}}}
templates:
- {name: main, container: {image: alpine, command: [deploy]}}
`
	}

	It("reads the limit each time a run asks for the lock", func() {
		lock, cm := "sem-"+rand.String(5), limits(true, "1")
		r := testReconciler()
		first := reconcileWith(ctx, r, newWorkflow(ctx, spec(lock, cm)))
		Expect(first.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowRunning))
		second := reconcileWith(ctx, r, newWorkflow(ctx, spec(lock, cm)))
		Expect(second.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowPending))
		Expect(second.Status.Message).To(ContainSubstring("waiting for the lock " + lock))

		// The platform team raises the limit; the waiting run takes the lock at its next try.
		var c corev1.ConfigMap
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: cm}, &c)).To(Succeed())
		c.Data["deploy"] = "2"
		Expect(k8sClient.Update(ctx, &c)).To(Succeed())
		second = reconcileWith(ctx, r, client.ObjectKeyFromObject(second))
		Expect(second.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowRunning))
	})

	It("fails a run when the ConfigMap has no label", func() {
		wf := reconcileWith(ctx, testReconciler(), newWorkflow(ctx, spec("sem-"+rand.String(5), limits(false, "1"))))
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowError))
		Expect(wf.Status.Message).To(ContainSubstring("has no label"))
	})
})
