package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
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

	// A lock without holders goes away, so that the Leases of old lock names do not pile up.
	It("deletes the Lease of a lock when its last holder gives it back", func() {
		a := run()
		Expect(step(a).Status.Phase).To(Equal(chroniclev1alpha1.WorkflowRunning))
		lease := &coordinationv1.Lease{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: lockPrefix + lock}, lease)).To(Succeed())
		succeed(step(a))
		Expect(step(a).Status.Phase).To(Equal(chroniclev1alpha1.WorkflowSucceeded))
		err := k8sClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: lockPrefix + lock}, lease)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "error: %v", err)
	})

	// whenBusy: Skip ends a run whose lock is full, for example a cron run that must not pile up.
	It("skips a run whose lock is busy", func() {
		a := run()
		Expect(step(a).Status.Phase).To(Equal(chroniclev1alpha1.WorkflowRunning))
		wf := &chroniclev1alpha1.Workflow{Name: "sync-" + rand.String(6), Namespace: "default"}
		Expect(yaml.UnmarshalStrict([]byte("entrypoint: c\nsynchronization: {mutex: {name: "+lock+"}, whenBusy: Skip}\ntemplates: [{name: c, container: {image: alpine, command: [run]}}]"), &wf.Spec)).To(Succeed())
		Expect(k8sClient.Create(ctx, wf)).To(Succeed())
		DeferCleanup(func() error { return client.IgnoreNotFound(k8sClient.Delete(ctx, wf)) })
		skipped := step(types.NamespacedName{Name: wf.Name, Namespace: wf.Namespace})
		Expect(skipped.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowSucceeded))
		Expect(skipped.Status.Message).To(HavePrefix("skipped: the lock " + lock + " is busy"))
		Expect(meta.IsStatusConditionTrue(skipped.Status.Conditions, conditionSkipped)).To(BeTrue())
		succeed(step(a))
		step(a)
	})

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
		running := step(b)
		Expect(running.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowRunning))
		Expect(running.Status.Message).To(BeEmpty(), "the run holds the lock now")
	})

	It("wakes the runs that wait for the lock when a holder gives it back", func() {
		a, b := run(), run()
		step(a)
		step(b)
		succeed(step(a))
		r := &WorkflowReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), ExecutorImage: testExecutorImage, wake: make(chan event.GenericEvent, 8)}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: a})
		Expect(err).NotTo(HaveOccurred())
		var woken []string
		for len(r.wake) > 0 {
			woken = append(woken, (<-r.wake).Object.GetName())
		}
		Expect(woken).To(ConsistOf(b.Name))
	})

	It("stops a run that waits for the lock", func() {
		e, f := run(), run()
		Expect(step(e).Status.Phase).To(Equal(chroniclev1alpha1.WorkflowRunning))
		Expect(step(f).Status.Phase).To(Equal(chroniclev1alpha1.WorkflowPending))
		patch := client.RawPatch(types.MergePatchType, []byte(`{"spec":{"shutdown":"Stop"}}`))
		Expect(k8sClient.Patch(ctx, &chroniclev1alpha1.Workflow{Name: f.Name, Namespace: f.Namespace}, patch)).To(Succeed())
		stopped := step(f)
		Expect(stopped.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowFailed))
		Expect(stopped.Status.Message).To(ContainSubstring("stopped while it waited for the lock"))
		Expect(stopped.Status.FinishedAt).NotTo(BeNil())
	})

	It("does not keep the lock for a holder that was deleted", func() {
		c, d := run(), run()
		Expect(step(c).Status.Phase).To(Equal(chroniclev1alpha1.WorkflowRunning))
		Expect(step(d).Status.Phase).To(Equal(chroniclev1alpha1.WorkflowPending))
		Expect(k8sClient.Delete(ctx, &chroniclev1alpha1.Workflow{Name: c.Name, Namespace: c.Namespace})).To(Succeed())
		Expect(step(d).Status.Phase).To(Equal(chroniclev1alpha1.WorkflowRunning))
	})
})
