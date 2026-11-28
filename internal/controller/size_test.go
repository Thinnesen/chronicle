package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/archive"
)

var _ = Describe("the size limit of a run", func() {
	ctx := context.Background()

	It("fails a run whose status would be too large, before it creates more Pods", func() {
		key := newWorkflow(ctx, `
entrypoint: main
templates:
- name: main
  steps:
  - - {name: first, template: c}
  - - {name: many, template: c, withSequence: {count: 50}}
- name: c
  container: {image: alpine, command: ["true"]}
`)
		r := testReconciler()
		r.MaxObjectBytes = 8 << 10
		wf := reconcileWith(ctx, r, key)
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowRunning))
		first := podsOf(ctx, wf)
		Expect(first).To(HaveLen(1))
		finishPod(ctx, &first[0])

		wf = reconcileWith(ctx, r, key)
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowError))
		Expect(wf.Status.Message).To(ContainSubstring("the run is too large"))
		Expect(wf.Status.FinishedAt).NotTo(BeNil())
		Expect(podsOf(ctx, wf)).To(HaveLen(1), "no Pod of the 50 items")
	})

	It("cancels the open tasks and stops the child runs of a run that is too large", func() {
		tmpl := &chroniclev1alpha1.WorkflowTemplate{Name: "etl-" + rand.String(6), Namespace: "default"}
		Expect(yaml.UnmarshalStrict([]byte(`{entrypoint: c, templates: [{name: c, container: {image: alpine}}]}`), &tmpl.Spec)).To(Succeed())
		Expect(k8sClient.Create(ctx, tmpl)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, tmpl)
		key := newWorkflow(ctx, `
entrypoint: main
templates:
- name: main
  dag: {tasks: [{name: t, template: t}, {name: ch, template: ch}, {name: first, template: c},
    {name: many, template: c, depends: first, withSequence: {count: 50}}]}
- {name: t, task: {name: greet}}
- {name: ch, childWorkflow: {workflowTemplateRef: {name: `+tmpl.Name+`}}}
- {name: c, container: {image: alpine, command: ["true"]}}
`)
		queue := &fakeQueue{}
		r := testReconciler()
		r.Tasks, r.MaxObjectBytes = queue, 8<<10
		wf := reconcileWith(ctx, r, key)
		Expect(queue.added).To(HaveLen(1))
		queue.tasks = []archive.Task{{NewTask: queue.added[0], State: archive.TaskRunning}}
		var children chroniclev1alpha1.WorkflowList
		Expect(k8sClient.List(ctx, &children, client.InNamespace("default"), client.MatchingLabels{LabelParentUID: string(wf.UID)})).To(Succeed())
		Expect(children.Items).To(HaveLen(1))
		first := podsOf(ctx, wf)
		Expect(first).To(HaveLen(1))
		finishPod(ctx, &first[0])

		wf = reconcileWith(ctx, r, key)
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowError))
		Expect(queue.cancelled).To(Equal([]string{string(wf.UID) + ": " + runTooLarge}))
		child := &children.Items[0]
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(child), child)).To(Succeed())
		Expect(child.Spec.Shutdown).To(Equal(chroniclev1alpha1.ShutdownTerminate))
	})
})
