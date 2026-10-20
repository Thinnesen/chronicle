package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

// newWorkflow creates a Workflow with the spec in the default namespace and deletes it after the test.
func newWorkflow(ctx context.Context, spec string) types.NamespacedName {
	wf := &chroniclev1alpha1.Workflow{Name: "wf-" + rand.String(6), Namespace: "default"}
	Expect(yaml.UnmarshalStrict([]byte(spec), &wf.Spec)).To(Succeed())
	Expect(k8sClient.Create(ctx, wf)).To(Succeed())
	DeferCleanup(func() error { return client.IgnoreNotFound(k8sClient.Delete(ctx, wf)) })
	return types.NamespacedName{Name: wf.Name, Namespace: wf.Namespace}
}

// reconcileWith runs one reconcile and returns the Workflow after it.
func reconcileWith(ctx context.Context, r *WorkflowReconciler, key types.NamespacedName) *chroniclev1alpha1.Workflow {
	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
	Expect(err).NotTo(HaveOccurred())
	wf := &chroniclev1alpha1.Workflow{}
	Expect(k8sClient.Get(ctx, key, wf)).To(Succeed())
	return wf
}

// podsOf lists the Pods of a Workflow.
func podsOf(ctx context.Context, wf *chroniclev1alpha1.Workflow) []corev1.Pod {
	var list corev1.PodList
	Expect(k8sClient.List(ctx, &list, client.InNamespace(wf.Namespace), client.MatchingLabels{LabelWorkflowUID: string(wf.UID)})).To(Succeed())
	return list.Items
}

// finishPod ends the main container of a Pod with an exit code.
func finishPod(ctx context.Context, pod *corev1.Pod, code int32) {
	now := metav1.Now()
	pod.Status.Phase = corev1.PodSucceeded
	if code != 0 {
		pod.Status.Phase = corev1.PodFailed
	}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: mainContainer, State: corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{ExitCode: code, StartedAt: now, FinishedAt: now},
	}}}
	Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
}

func testReconciler() *WorkflowReconciler {
	return &WorkflowReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), ExecutorImage: testExecutorImage}
}
