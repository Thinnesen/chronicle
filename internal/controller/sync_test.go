package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

var _ = Describe("Workflow synchronization", func() {
	ctx := context.Background()
	lock := "deploy-" + rand.String(6)
	spec := "entrypoint: c\nsynchronization: {mutex: {name: " + lock + "}}\ntemplates: [{name: c, container: {image: alpine, command: [run]}}]"

	run := func() types.NamespacedName {
		wf := &chroniclev1alpha1.Workflow{Name: "sync-" + rand.String(6), Namespace: "default"}
		Expect(yaml.UnmarshalStrict([]byte(spec), &wf.Spec)).To(Succeed())
		Expect(k8sClient.Create(ctx, wf)).To(Succeed())
		DeferCleanup(func() error { return client.IgnoreNotFound(k8sClient.Delete(ctx, wf)) })
		return types.NamespacedName{Name: wf.Name, Namespace: wf.Namespace}
	}
	step := func(key types.NamespacedName) *chroniclev1alpha1.Workflow {
		r := &WorkflowReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), ExecutorImage: testExecutorImage}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		wf := &chroniclev1alpha1.Workflow{}
		Expect(k8sClient.Get(ctx, key, wf)).To(Succeed())
		return wf
	}
	succeed := func(wf *chroniclev1alpha1.Workflow) {
		var list corev1.PodList
		Expect(k8sClient.List(ctx, &list, client.InNamespace(wf.Namespace), client.MatchingLabels{LabelWorkflowUID: string(wf.UID)})).To(Succeed())
		Expect(list.Items).To(HaveLen(1))
		pod := &list.Items[0]
		pod.Status.Phase = corev1.PodSucceeded
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: mainContainer, State: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}}}
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
	}

	It("runs one holder of a mutex at a time and passes the lock on", func() {
		a, b := run(), run()
		Expect(step(a).Status.Phase).To(Equal(chroniclev1alpha1.WorkflowRunning))
		waiting := step(b)
		Expect(waiting.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowPending))
		Expect(waiting.Status.Message).To(ContainSubstring("which " + a.Name + " holds"))

		succeed(step(a))
		done := step(a)
		Expect(done.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowSucceeded))
		Expect(meta.IsStatusConditionTrue(done.Status.Conditions, conditionLocked)).To(BeFalse())
		Expect(step(b).Status.Phase).To(Equal(chroniclev1alpha1.WorkflowRunning))
	})

	It("does not keep the lock for a holder that was deleted", func() {
		c, d := run(), run()
		Expect(step(c).Status.Phase).To(Equal(chroniclev1alpha1.WorkflowRunning))
		Expect(step(d).Status.Phase).To(Equal(chroniclev1alpha1.WorkflowPending))
		Expect(k8sClient.Delete(ctx, &chroniclev1alpha1.Workflow{Name: c.Name, Namespace: c.Namespace})).To(Succeed())
		Expect(step(d).Status.Phase).To(Equal(chroniclev1alpha1.WorkflowRunning))
	})
})
