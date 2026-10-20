package v1alpha1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	authenticationv1 "k8s.io/api/authentication/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

// newWorkflow returns a Workflow in the default namespace with the spec from YAML.
func newWorkflow(spec string) *chroniclev1alpha1.Workflow {
	wf := &chroniclev1alpha1.Workflow{Name: "wf-" + rand.String(6), Namespace: "default"}
	Expect(yaml.UnmarshalStrict([]byte(spec), &wf.Spec)).To(Succeed())
	return wf
}

// expectRejected creates obj and expects an Invalid error that contains each text.
func expectRejected(obj client.Object, texts ...string) {
	err := k8sClient.Create(ctx, obj)
	Expect(err).To(HaveOccurred())
	Expect(apierrors.IsInvalid(err)).To(BeTrue(), "error: %v", err)
	for _, t := range texts {
		Expect(err.Error()).To(ContainSubstring(t))
	}
}

const (
	container   = "{name: c, container: {image: alpine}}"
	unknownName = "nope"
)

var _ = Describe("Workflow validation", func() {
	DescribeTable("the CRD schema rejects",
		func(spec string, texts ...string) {
			expectRejected(newWorkflow(spec), texts...)
		},
		Entry("a template with two types",
			"entrypoint: c\ntemplates: [{name: c, container: {image: a}, script: {image: a, source: x}}]",
			"exactly one of container, script, dag, steps, suspend, resource, task or http"),
		Entry("a template with no type",
			"entrypoint: c\ntemplates: [{name: c}]",
			"exactly one of container, script, dag, steps, suspend, resource, task or http"),
		Entry("duplicate template names",
			"entrypoint: c\ntemplates: ["+container+", "+container+"]",
			"Duplicate value"),
		Entry("dependencies and depends in one task",
			"entrypoint: m\ntemplates: [{name: m, dag: {tasks: [{name: a, template: c}, {name: b, template: c, dependencies: [a], depends: a}]}}, "+container+"]",
			"use dependencies or depends, not both"),
		Entry("no templates and no reference",
			"entrypoint: c",
			"give spec.entrypoint and spec.templates"),
		Entry("a reference and templates",
			"workflowTemplateRef: {name: t}\ntemplates: ["+container+"]",
			"give spec.entrypoint and spec.templates"),
		Entry("a bad template name",
			"entrypoint: 'a b'\ntemplates: [{name: 'a b', container: {image: a}}]",
			"spec.templates[0].name"),
		Entry("an empty DAG",
			"entrypoint: m\ntemplates: [{name: m, dag: {tasks: []}}]",
			"spec.templates[0].dag.tasks"),
	)

	DescribeTable("the webhook rejects",
		func(spec string, texts ...string) {
			expectRejected(newWorkflow(spec), texts...)
		},
		Entry("a cycle",
			"entrypoint: m\ntemplates: [{name: m, dag: {tasks: [{name: a, template: c, depends: b}, {name: b, template: c, depends: a}]}}, "+container+"]",
			"the DAG has a cycle", "a -> b -> a"),
		Entry("an unknown template",
			"entrypoint: m\ntemplates: [{name: m, dag: {tasks: [{name: a, template: nope}]}}]",
			`spec.templates[0].dag.tasks[0].template: Not found: "nope"`),
		Entry("an unknown argument",
			"entrypoint: m\ntemplates: [{name: m, steps: [[{name: a, template: c, arguments: {parameters: [{name: x, value: z}]}}]]}, "+container+"]",
			`spec.templates[0].steps[0][0].arguments.parameters[0].name: Not found: "x"`),
		Entry("an unknown placeholder",
			"entrypoint: c\ntemplates: [{name: c, container: {image: alpine, args: ['{{inputs.parameters.x}}']}}]",
			`unknown placeholder "inputs.parameters.x"`),
		Entry("a workflow parameter without a value",
			"entrypoint: c\narguments: {parameters: [{name: p}]}\ntemplates: ["+container+"]",
			"spec.arguments.parameters[0].value: Required value"),
	)

	It("accepts a valid workflow", func() {
		wf := newWorkflow(`
entrypoint: main
arguments: {parameters: [{name: who, value: world}]}
templates:
- name: main
  dag:
    tasks:
    - {name: a, template: hello, arguments: {parameters: [{name: who, value: "{{workflow.parameters.who}}"}]}}
    - {name: b, template: hello, depends: a.Succeeded, arguments: {parameters: [{name: who, value: "{{tasks.a.outputs.result}}"}]}}
- name: hello
  inputs: {parameters: [{name: who}]}
  container: {image: alpine, command: [echo, "hello {{inputs.parameters.who}}"]}
`)
		Expect(k8sClient.Create(ctx, wf)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, wf)

		By("changing a label without a spec change")
		wf.Labels = map[string]string{"team": "a"}
		Expect(k8sClient.Update(ctx, wf)).To(Succeed())
	})

	It("accepts a workflow with a template reference", func() {
		wf := newWorkflow("workflowTemplateRef: {name: some-template}\narguments: {parameters: [{name: p, value: v}]}")
		Expect(k8sClient.Create(ctx, wf)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, wf)
	})

	It("rejects an invalid spec change", func() {
		wf := newWorkflow("entrypoint: c\ntemplates: [" + container + "]")
		Expect(k8sClient.Create(ctx, wf)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, wf)
		wf.Spec.Entrypoint = unknownName
		err := k8sClient.Update(ctx, wf)
		Expect(apierrors.IsInvalid(err)).To(BeTrue(), "error: %v", err)
	})

	It("permits only Stop to Terminate for a shutdown", func() {
		wf := newWorkflow("entrypoint: c\ntemplates: [" + container + "]")
		Expect(k8sClient.Create(ctx, wf)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, wf)
		wf.Spec.Shutdown = chroniclev1alpha1.ShutdownStop
		Expect(k8sClient.Update(ctx, wf)).To(Succeed())
		wf.Spec.Shutdown = ""
		Expect(k8sClient.Update(ctx, wf)).To(MatchError(ContainSubstring("a shutdown cannot be removed")))
		wf.Spec.Shutdown = chroniclev1alpha1.ShutdownTerminate
		Expect(k8sClient.Update(ctx, wf)).To(Succeed())

		// A retry of the finished run removes the shutdown, so that the new try runs.
		wf.Status.Phase = chroniclev1alpha1.WorkflowFailed
		Expect(k8sClient.Status().Update(ctx, wf)).To(Succeed())
		wf.Spec.Shutdown, wf.Spec.Restart = "", &chroniclev1alpha1.Restart{Generation: 1}
		Expect(k8sClient.Update(ctx, wf)).To(Succeed())
	})

	It("skips the checks when the spec does not change", func() {
		wf := newWorkflow("entrypoint: nope\ntemplates: [" + container + "]")
		Expect((&WorkflowValidator{}).ValidateUpdate(ctx, wf, wf.DeepCopy())).Error().NotTo(HaveOccurred())
		Expect((&WorkflowValidator{}).ValidateDelete(ctx, wf)).Error().NotTo(HaveOccurred())
	})
})

var _ = Describe("Workflow approvals", func() {
	It("accepts an approval user only when it is the user of the request", func() {
		review := &authenticationv1.SelfSubjectReview{}
		Expect(k8sClient.Create(ctx, review)).To(Succeed())
		me := review.Status.UserInfo.Username
		Expect(me).NotTo(BeEmpty())

		wf := newWorkflow("entrypoint: c\ntemplates: [" + container + "]")
		Expect(k8sClient.Create(ctx, wf)).To(Succeed())
		wf.Spec.Approvals = []chroniclev1alpha1.Approval{{Node: wf.Name, Decision: chroniclev1alpha1.DecisionApprove, User: "mallory"}}
		err := k8sClient.Update(ctx, wf)
		Expect(apierrors.IsInvalid(err)).To(BeTrue(), "error: %v", err)
		Expect(err.Error()).To(ContainSubstring("the user of an approval must be the user of the request"))

		wf.Spec.Approvals[0].User = me
		Expect(k8sClient.Update(ctx, wf)).To(Succeed())
		// Without a user, as with kubectl patch, the approval is allowed.
		wf.Spec.Approvals = append(wf.Spec.Approvals, chroniclev1alpha1.Approval{Node: "other", Decision: chroniclev1alpha1.DecisionReject})
		Expect(k8sClient.Update(ctx, wf)).To(Succeed())
	})
})
