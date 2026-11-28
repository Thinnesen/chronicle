package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/schedule"
)

var _ = Describe("A run that ends before it starts", func() {
	ctx := context.Background()

	It("is archived and deleted after its TTL", func() {
		fake := &fakeArchive{}
		r := testReconciler()
		r.Archive = fake
		key := newWorkflow(ctx, "workflowTemplateRef: {name: no-such-template}\nttlStrategy: {secondsAfterCompletion: 0}")
		Eventually(func() bool {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
			return apierrors.IsNotFound(k8sClient.Get(ctx, key, &chroniclev1alpha1.Workflow{}))
		}).Should(BeTrue())
		Expect(fake.saved).To(HaveLen(1))
	})
})

var _ = Describe("The TTL delete", func() {
	ctx := context.Background()

	It("does not delete a run that changed after the read", func() {
		key := newWorkflow(ctx, "workflowTemplateRef: {name: no-such-template}\nttlStrategy: {secondsAfterCompletion: 0}")
		var wf chroniclev1alpha1.Workflow
		Expect(k8sClient.Get(ctx, key, &wf)).To(Succeed())
		now := metav1.Now()
		wf.Status.Phase, wf.Status.FinishedAt = chroniclev1alpha1.WorkflowError, &now
		Expect(k8sClient.Status().Update(ctx, &wf)).To(Succeed())
		stale := wf.DeepCopy()

		By("changing the run after the read, as a retry does")
		wf.Labels = map[string]string{"changed": "true"}
		Expect(k8sClient.Update(ctx, &wf)).To(Succeed())

		_, err := testReconciler().applyTTL(ctx, stale, ctrl.Result{})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, key, &chroniclev1alpha1.Workflow{})).To(Succeed())

		By("deleting it as an old run of a CronWorkflow from the same stale read")
		cr := &CronWorkflowReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		cw := &chroniclev1alpha1.CronWorkflow{Namespace: key.Namespace}
		Expect(cr.apply(ctx, cw, []chroniclev1alpha1.Workflow{*stale}, schedule.Plan{Delete: []string{key.Name}})).To(Succeed())
		Expect(k8sClient.Get(ctx, key, &chroniclev1alpha1.Workflow{})).To(Succeed())
	})
})
