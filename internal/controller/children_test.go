package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

var _ = Describe("Child runs", func() {
	ctx := context.Background()
	parentSpec := func(template string) string {
		return `
entrypoint: main
templates:
- name: main
  dag: {tasks: [{name: a, template: etl}, {name: b, template: c, depends: a, arguments: {parameters: [{name: x, value: "{{tasks.a.outputs.parameters.rows}}"}]}}]}
- name: etl
  outputs: {parameters: [{name: rows}]}
  childWorkflow: {workflowTemplateRef: {name: ` + template + `}, arguments: {parameters: [{name: day, value: "{{workflow.name}}"}]}}
- name: c
  inputs: {parameters: [{name: x}]}
  container: {image: alpine, command: [echo, "{{inputs.parameters.x}}"]}
`
	}
	newTemplate := func() string {
		t := &chroniclev1alpha1.WorkflowTemplate{Name: "etl-" + rand.String(6), Namespace: "default"}
		Expect(yaml.UnmarshalStrict([]byte(`{entrypoint: c, arguments: {parameters: [{name: day}]}, templates: [{name: c, container: {image: alpine}}]}`), &t.Spec)).To(Succeed())
		Expect(k8sClient.Create(ctx, t)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, t)
		return t.Name
	}
	childOf := func(parent *chroniclev1alpha1.Workflow) *chroniclev1alpha1.Workflow {
		var list chroniclev1alpha1.WorkflowList
		Expect(k8sClient.List(ctx, &list, client.InNamespace("default"), client.MatchingLabels{LabelParentUID: string(parent.UID)})).To(Succeed())
		Expect(list.Items).To(HaveLen(1))
		return &list.Items[0]
	}

	It("starts a child run and passes its outputs on", func() {
		r := testReconciler()
		key := newWorkflow(ctx, parentSpec(newTemplate()))
		parent := reconcileWith(ctx, r, key)
		child := childOf(parent)
		Expect(metav1.IsControlledBy(child, parent)).To(BeTrue())
		Expect(child.Annotations).To(HaveKeyWithValue(AnnotationDepth, "1"))
		Expect(child.Spec.Arguments.Parameters).To(HaveLen(1))
		Expect(*child.Spec.Arguments.Parameters[0].Value).To(Equal(parent.Name))

		now := metav1.Now()
		v := "5"
		child.Status = chroniclev1alpha1.WorkflowStatus{Phase: chroniclev1alpha1.WorkflowSucceeded, StartedAt: &now, FinishedAt: &now,
			Outputs: &chroniclev1alpha1.Outputs{Parameters: []chroniclev1alpha1.Parameter{{Name: "rows", Value: &v}}}}
		Expect(k8sClient.Status().Update(ctx, child)).To(Succeed())
		Eventually(func() []string {
			var cmds []string
			for _, p := range podsOf(ctx, reconcileWith(ctx, r, key)) {
				cmds = append(cmds, p.Spec.Containers[0].Command...)
			}
			return cmds
		}).Should(ContainElement("5"))
	})

	It("stops the child run when the parent stops", func() {
		r := testReconciler()
		key := newWorkflow(ctx, parentSpec(newTemplate()))
		parent := reconcileWith(ctx, r, key)
		child := childOf(parent)
		now := metav1.Now()
		child.Status = chroniclev1alpha1.WorkflowStatus{Phase: chroniclev1alpha1.WorkflowRunning, StartedAt: &now}
		Expect(k8sClient.Status().Update(ctx, child)).To(Succeed())
		Eventually(func() chroniclev1alpha1.NodePhase {
			return nodeByName(reconcileWith(ctx, r, key).Status, key.Name+".a").Phase
		}).Should(Equal(chroniclev1alpha1.NodeRunning))

		parent = reconcileWith(ctx, r, key)
		parent.Spec.Shutdown = chroniclev1alpha1.ShutdownTerminate
		Expect(k8sClient.Update(ctx, parent)).To(Succeed())
		Eventually(func() chroniclev1alpha1.ShutdownMode {
			reconcileWith(ctx, r, key)
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(child), child)).To(Succeed())
			return child.Spec.Shutdown
		}).Should(Equal(chroniclev1alpha1.ShutdownTerminate))
	})
})

func nodeByName(st chroniclev1alpha1.WorkflowStatus, name string) chroniclev1alpha1.NodeStatus {
	for _, n := range st.Nodes {
		if n.Name == name {
			return n
		}
	}
	return chroniclev1alpha1.NodeStatus{}
}
