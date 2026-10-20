package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
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
