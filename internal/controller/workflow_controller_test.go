package controller

import (
	"context"
	"encoding/json"
	"errors"

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
	"github.com/Thinnesen/chronicle/internal/artifact"
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

	It("resolves template references once and fails on a missing one", func() {
		lib := &chroniclev1alpha1.WorkflowTemplate{Name: "lib-" + rand.String(6), Namespace: "default", Spec: chroniclev1alpha1.WorkflowTemplateSpec{
			Entrypoint: "say",
			Arguments:  chroniclev1alpha1.Arguments{Parameters: []chroniclev1alpha1.Parameter{{Name: "word", Value: new("hi")}}},
			Templates: []chroniclev1alpha1.Template{{Name: "say", Container: &chroniclev1alpha1.ContainerTemplate{
				Image: "alpine", Command: []string{"echo", "{{workflow.parameters.word}}"}}}},
		}}
		Expect(k8sClient.Create(ctx, lib)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, lib)

		key := create("workflowTemplateRef: {name: " + lib.Name + "}\narguments: {parameters: [{name: word, value: hello}]}")
		wf := reconcileOnce(key)
		Expect(wf.Status.StoredSpec).NotTo(BeNil())
		Expect(pods(wf)[0].Spec.Containers[0].Command[3:]).To(Equal([]string{"echo", "hello"}))

		By("changing the template after the start")
		lib.Spec.Templates[0].Container.Image = "changed"
		Expect(k8sClient.Update(ctx, lib)).To(Succeed())
		wf = reconcileOnce(key)
		Expect(wf.Status.StoredSpec.Templates[0].Container.Image).To(Equal("alpine"))

		key = create("entrypoint: m\ntemplates: [{name: m, dag: {tasks: [{name: a, templateRef: {name: nope, template: x, clusterScope: true}}]}}]")
		wf = reconcileOnce(key)
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowError))
		Expect(wf.Status.Message).To(ContainSubstring(`ClusterWorkflowTemplate "nope"`))
	})

	It("retries the failed nodes of a finished run", func() {
		key := create("entrypoint: main\ntemplates:\n- {name: main, dag: {tasks: [{name: a, template: c}, {name: b, template: c, depends: a}]}}\n- {name: c, container: {image: alpine, command: ['true']}}")
		wf := reconcileOnce(key)
		first := pods(wf)[0]
		finish(&first, 1, executor.Message{})
		wf = reconcileOnce(key)
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowFailed))

		By("asking for a retry")
		wf.Spec.Restart = &chroniclev1alpha1.Restart{Generation: 1}
		Expect(k8sClient.Update(ctx, wf)).To(Succeed())
		wf = reconcileOnce(key)
		Expect(wf.Status.Restarts).To(Equal(int32(1)))
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowRunning))
		list := pods(wf)
		Expect(list).To(HaveLen(1))
		Expect(list[0].Name).To(HaveSuffix("-r1"))

		retried := list[0]
		finish(&retried, 0, executor.Message{})
		wf = reconcileOnce(key)
		next := pods(wf)
		Expect(next).To(HaveLen(2))
		for i := range next {
			if next[i].Name != retried.Name {
				finish(&next[i], 0, executor.Message{})
			}
		}
		wf = reconcileOnce(key)
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowSucceeded))

		By("refusing a retry without a failed node")
		wf.Spec.Restart = &chroniclev1alpha1.Restart{Generation: 2}
		Expect(k8sClient.Update(ctx, wf)).To(Succeed())
		wf = reconcileOnce(key)
		Expect(wf.Status.Restarts).To(Equal(int32(2)))
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowSucceeded))
		Expect(wf.Status.Conditions).To(ContainElement(HaveField("Message", ContainSubstring("no node failed"))))
	})

	It("archives a finished run before its TTL deletes it", func() {
		fake := &fakeArchive{failures: 1}
		withArchive := func() *WorkflowReconciler { rec := r(); rec.Archive = fake; return rec }
		key := create("entrypoint: c\nttlStrategy: {secondsAfterCompletion: 0}\ntemplates:\n- {name: c, container: {image: alpine, command: ['true']}}")
		_, err := withArchive().Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		wf := &chroniclev1alpha1.Workflow{}
		Expect(k8sClient.Get(ctx, key, wf)).To(Succeed())
		pod := pods(wf)[0]
		finish(&pod, 0, executor.Message{})

		By("failing the first save: the run stays")
		_, err = withArchive().Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).To(MatchError(ContainSubstring("archive the run")))
		Expect(k8sClient.Get(ctx, key, wf)).To(Succeed())
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowSucceeded))

		By("saving on the next try, then deleting the run")
		_, err = withArchive().Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(fake.saved).To(HaveLen(1))
		Expect(fake.saved[0]).To(Equal([2]int{1, 1}))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, key, wf))).To(BeTrue())
	})

	It("passes an artifact through the repository of the namespace", func() {
		cm := &corev1.ConfigMap{Name: artifact.Name, Namespace: "default",
			Data: map[string]string{"endpoint": "s3.example.com:443", "bucket": "runs", "archiveLogs": "true"}}
		Expect(k8sClient.Create(ctx, cm)).To(Succeed())
		DeferCleanup(func() error { return k8sClient.Delete(ctx, cm) })
		key := create(`
entrypoint: main
templates:
- name: main
  dag:
    tasks:
    - {name: a, template: make}
    - {name: b, template: use, depends: a, arguments: {artifacts: [{name: data, from: "{{tasks.a.outputs.artifacts.data}}"}]}}
- {name: make, outputs: {artifacts: [{name: data, path: /tmp/out}]}, container: {image: alpine, command: [run]}}
- {name: use, inputs: {artifacts: [{name: data, path: /tmp/in}]}, container: {image: alpine, command: [run]}}
`)
		wf := reconcileOnce(key)
		a := pods(wf)[0]
		prefix := "default/" + string(wf.UID) + "/" + a.Name
		Expect(a.Annotations).To(HaveKeyWithValue(AnnotationArtifactPrefix, prefix))
		env := a.Spec.Containers[0].Env
		Expect(env).To(ContainElement(HaveField("ValueFrom.SecretKeyRef.Name", artifact.Name)))
		Expect(env).To(ContainElement(HaveField("Value", ContainSubstring(`"outputs":[{"name":"data","path":"/tmp/out"}]`))))

		finish(&a, 0, executor.Message{Artifacts: []string{"data"}, Log: true})
		wf = reconcileOnce(key)
		Expect(wf.Status.Nodes[a.Name].LogKey).To(Equal(prefix + "/main.log"))
		var b corev1.Pod
		for _, p := range pods(wf) {
			if p.Name != a.Name {
				b = p
			}
		}
		Expect(b.Spec.Containers[0].Env).To(ContainElement(HaveField("Value",
			ContainSubstring(`"inputs":[{"name":"data","path":"/tmp/in","key":"`+prefix+`/data.tgz"}]`))))
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

// fakeArchive fails the first saves, then records the node counts of each save.
type fakeArchive struct {
	failures int
	saved    [][2]int
}

func (f *fakeArchive) Save(_ context.Context, _ *chroniclev1alpha1.Workflow, done, total int) error {
	if f.failures > 0 {
		f.failures--
		return errors.New("the database is down")
	}
	f.saved = append(f.saved, [2]int{done, total})
	return nil
}
