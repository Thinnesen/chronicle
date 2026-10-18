package v1alpha1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/util/rand"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

var _ = Describe("ClusterWorkflowTemplate validation", func() {
	newTemplate := func(spec string) *chroniclev1alpha1.ClusterWorkflowTemplate {
		return &chroniclev1alpha1.ClusterWorkflowTemplate{
			Name: "ct-" + rand.String(6),
			Spec: templateSpec(spec),
		}
	}

	It("accepts a valid template", func() {
		t := newTemplate("entrypoint: c\ntemplates: [" + container + "]")
		Expect(k8sClient.Create(ctx, t)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, t)

		By("rejecting an invalid spec change")
		t.Spec.Entrypoint = unknownName
		Expect(k8sClient.Update(ctx, t)).NotTo(Succeed())
	})

	It("rejects a cycle", func() {
		expectRejected(newTemplate("entrypoint: m\ntemplates: [{name: m, dag: {tasks: [{name: a, template: c, dependencies: [a]}]}}, "+container+"]"),
			"the DAG has a cycle")
	})

	It("skips the checks when the spec does not change", func() {
		t := newTemplate("entrypoint: nope\ntemplates: [" + container + "]")
		Expect((&ClusterWorkflowTemplateValidator{}).ValidateUpdate(ctx, t, t.DeepCopy())).Error().NotTo(HaveOccurred())
		Expect((&ClusterWorkflowTemplateValidator{}).ValidateDelete(ctx, t)).Error().NotTo(HaveOccurred())
	})
})
