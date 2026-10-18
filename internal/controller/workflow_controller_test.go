package controller

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/executor"
)

const testExecutorImage = "example.com/chronicle:test"

var _ = Describe("Workflow Controller", func() {
	ctx := context.Background()
	r := func() *WorkflowReconciler {
		return &WorkflowReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), ExecutorImage: testExecutorImage}
	}

	create := func(spec string) types.NamespacedName {
		wf := &chroniclev1alpha1.Workflow{Name: "wf-" + rand.String(6), Namespace: "default"}
		Expect(yaml.UnmarshalStrict([]byte(spec), &wf.Spec)).To(Succeed())
		Expect(k8sClient.Create(ctx, wf)).To(Succeed())
		DeferCleanup(func() error { return client.IgnoreNotFound(k8sClient.Delete(ctx, wf)) })
		return types.NamespacedName{Name: wf.Name, Namespace: wf.Namespace}
	}

	reconcileOnce := func(key types.NamespacedName) *chroniclev1alpha1.Workflow {
		_, err := r().Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		wf := &chroniclev1alpha1.Workflow{}
		Expect(k8sClient.Get(ctx, key, wf)).To(Succeed())
		return wf
	}

	pods := func(wf *chroniclev1alpha1.Workflow) []corev1.Pod {
		var list corev1.PodList
		Expect(k8sClient.List(ctx, &list, client.InNamespace(wf.Namespace), client.MatchingLabels{LabelWorkflowUID: string(wf.UID)})).To(Succeed())
		return list.Items
	}

	// finish plays the kubelet: it ends the main container with an exit code and a termination message.
	finish := func(pod *corev1.Pod, code int32, msg executor.Message) {
		data, _ := json.Marshal(msg)
		now := metav1.Now()
		pod.Status.Phase = corev1.PodSucceeded
		if code != 0 {
			pod.Status.Phase = corev1.PodFailed
		}
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: mainContainer, State: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{ExitCode: code, Message: string(data), StartedAt: now, FinishedAt: now},
		}}}
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
	}

	It("runs a DAG to Succeeded and passes outputs on", func() {
		key := create(`
entrypoint: main
templates:
- name: main
  dag:
    tasks:
    - {name: a, template: gen}
    - {name: b, template: use, depends: a, arguments: {parameters: [{name: x, value: "{{tasks.a.outputs.parameters.p}}"}]}}
- name: gen
  outputs: {parameters: [{name: p, valueFrom: {path: /tmp/p}}]}
  script: {image: alpine, source: "echo -n hi > /tmp/p"}
- name: use
  inputs: {parameters: [{name: x}]}
  container: {image: alpine, command: [echo], args: ["{{inputs.parameters.x}}"]}
`)
		wf := reconcileOnce(key)
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowRunning))
		list := pods(wf)
		Expect(list).To(HaveLen(1))

		By("checking the hardened Pod")
		pod := list[0]
		Expect(pod.OwnerReferences).To(HaveLen(1))
		Expect(*pod.Spec.SecurityContext.RunAsNonRoot).To(BeTrue())
		Expect(*pod.Spec.AutomountServiceAccountToken).To(BeFalse())
		Expect(pod.Spec.InitContainers[0].Image).To(Equal(testExecutorImage))
		main := pod.Spec.Containers[0]
		Expect(main.Command).To(Equal([]string{execPath, "run", "--", "sh"}))
		Expect(*main.SecurityContext.AllowPrivilegeEscalation).To(BeFalse())
		Expect(main.Env).To(ContainElement(corev1.EnvVar{Name: executor.EnvScript, Value: "echo -n hi > /tmp/p"}))
		Expect(main.Env).To(ContainElement(corev1.EnvVar{Name: executor.EnvOutputs, Value: `[{"name":"p","path":"/tmp/p"}]`}))

		finish(&pod, 0, executor.Message{Parameters: map[string]string{"p": "hi"}})
		wf = reconcileOnce(key)
		list = pods(wf)
		Expect(list).To(HaveLen(2))
		var b corev1.Pod
		for _, p := range list {
			if p.Name != pod.Name {
				b = p
			}
		}
		Expect(b.Spec.Containers[0].Command).To(Equal([]string{execPath, "run", "--", "echo", "hi"}))

		finish(&b, 0, executor.Message{})
		wf = reconcileOnce(key)
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowSucceeded))
		Expect(wf.Status.FinishedAt).NotTo(BeNil())
	})

	It("runs a failing DAG to Failed", func() {
		key := create("entrypoint: main\ntemplates:\n- {name: main, dag: {tasks: [{name: a, template: c}, {name: b, template: c, depends: a}]}}\n- {name: c, container: {image: alpine, command: ['false']}}")
		wf := reconcileOnce(key)
		pod := pods(wf)[0]
		finish(&pod, 1, executor.Message{})
		wf = reconcileOnce(key)
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowFailed))
		Expect(wf.Status.Message).To(ContainSubstring(`task "a" failed`))
		Expect(pods(wf)).To(HaveLen(1))
	})

	It("runs a resource template with the workflow service account", func() {
		key := create(`
entrypoint: main
serviceAccountName: deployer
arguments: {parameters: [{name: cmname, value: cfg}]}
templates:
- name: main
  outputs: {parameters: [{name: uid, valueFrom: {jsonPath: "{.metadata.uid}"}}]}
  resource:
    action: create
    setOwnerReference: true
    successCondition: metadata.name == cfg
    manifest: |
      apiVersion: v1
      kind: ConfigMap
      metadata: {name: "{{workflow.parameters.cmname}}"}
`)
		wf := reconcileOnce(key)
		pod := pods(wf)[0]
		Expect(*pod.Spec.AutomountServiceAccountToken).To(BeTrue())
		Expect(pod.Spec.ServiceAccountName).To(Equal("deployer"))
		Expect(pod.Spec.InitContainers).To(BeEmpty())
		main := pod.Spec.Containers[0]
		Expect(main.Image).To(Equal(testExecutorImage))
		var spec executor.ResourceSpec
		Expect(json.Unmarshal([]byte(main.Env[0].Value), &spec)).To(Succeed())
		Expect(string(spec.Manifest)).To(Equal(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cfg"}}`))
		Expect(spec.Owner.UID).To(Equal(wf.UID))
		Expect(spec.Outputs).To(HaveLen(1))
	})

	It("deletes a finished workflow after its TTL", func() {
		key := create("entrypoint: c\nttlStrategy: {secondsAfterCompletion: 0}\npodGC: {strategy: OnPodCompletion}\ntemplates:\n- {name: c, container: {image: alpine, command: ['true']}}")
		wf := reconcileOnce(key)
		pod := pods(wf)[0]
		finish(&pod, 0, executor.Message{})
		_, err := r().Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, key, &chroniclev1alpha1.Workflow{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(&pod), &corev1.Pod{}))).To(BeTrue())
	})

	It("runs a container without a command directly", func() {
		key := create("entrypoint: c\ntemplates:\n- {name: c, container: {image: alpine, args: [x]}}")
		wf := reconcileOnce(key)
		pod := pods(wf)[0]
		Expect(pod.Spec.InitContainers).To(BeEmpty())
		Expect(pod.Spec.Containers[0].Command).To(BeEmpty())
		Expect(pod.Spec.Containers[0].Args).To(Equal([]string{"x"}))
	})
})
