package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

var _ = Describe("The Pod GC", func() {
	ctx := context.Background()

	It("keeps the Pod of a finished node until the status with the result is saved", func() {
		key := newWorkflow(ctx, "entrypoint: c\npodGC: {strategy: OnPodCompletion}\ntemplates:\n- {name: c, container: {image: alpine, command: ['true']}}\n")
		wf := reconcileWith(ctx, testReconciler(), key)
		pod := podsOf(ctx, wf)[0]
		finishPod(ctx, &pod)

		// The cache had an old Workflow, so the status update gets a conflict.
		conflict := testReconciler()
		conflict.Client = conflictClient{k8sClient}
		_, err := conflict.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&pod), &corev1.Pod{})).To(Succeed())

		wf = reconcileWith(ctx, testReconciler(), key)
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowSucceeded))
		Expect(wf.Status.Nodes[wf.Name].Phase).To(Equal(chroniclev1alpha1.NodeSucceeded))
		reconcileWith(ctx, testReconciler(), key)
		Expect(podsOf(ctx, wf)).To(BeEmpty())
	})
})

// conflictClient is a client whose status updates get a conflict.
type conflictClient struct{ client.Client }

func (c conflictClient) Status() client.SubResourceWriter { return conflictWriter{c.Client.Status()} }

type conflictWriter struct{ client.SubResourceWriter }

func (conflictWriter) Update(_ context.Context, obj client.Object, _ ...client.SubResourceUpdateOption) error {
	return apierrors.NewConflict(schema.GroupResource{Resource: "workflows"}, obj.GetName(), nil)
}
