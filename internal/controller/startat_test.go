package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

var _ = Describe("Start time", func() {
	ctx := context.Background()
	spec := func(at time.Time) string {
		return `{entrypoint: main, startAt: "` + at.UTC().Format(time.RFC3339) + `", templates: [{name: main, container: {image: alpine, command: [run]}}]}`
	}

	It("waits until the start time, then starts", func() {
		r := testReconciler()
		key := newWorkflow(ctx, spec(time.Now().Add(time.Hour)))
		res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(BeNumerically("~", time.Hour, time.Minute))
		wf := &chroniclev1alpha1.Workflow{}
		Expect(k8sClient.Get(ctx, key, wf)).To(Succeed())
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowPending))
		Expect(wf.Status.Message).To(HavePrefix("the run starts at "))
		Expect(podsOf(ctx, wf)).To(BeEmpty())

		wf.Spec.StartAt = &metav1.Time{Time: time.Now().Add(-time.Second)}
		Expect(k8sClient.Update(ctx, wf)).To(Succeed())
		wf = reconcileWith(ctx, r, key)
		Expect(podsOf(ctx, wf)).To(HaveLen(1))
		Expect(wf.Status.Message).To(BeEmpty())
	})

	It("ends a waiting run that is stopped", func() {
		r := testReconciler()
		key := newWorkflow(ctx, spec(time.Now().Add(time.Hour)))
		wf := reconcileWith(ctx, r, key)
		wf.Spec.Shutdown = chroniclev1alpha1.ShutdownStop
		Expect(k8sClient.Update(ctx, wf)).To(Succeed())
		Eventually(func() chroniclev1alpha1.WorkflowPhase { return reconcileWith(ctx, r, key).Status.Phase }).
			Should(Equal(chroniclev1alpha1.WorkflowFailed))
		Expect(podsOf(ctx, wf)).To(BeEmpty())
	})
})
