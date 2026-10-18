package v1alpha1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/yaml"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

func templateSpec(spec string) chroniclev1alpha1.WorkflowTemplateSpec {
	var s chroniclev1alpha1.WorkflowTemplateSpec
	Expect(yaml.UnmarshalStrict([]byte(spec), &s)).To(Succeed())
	return s
}

var _ = Describe("WorkflowTemplate validation", func() {
	newTemplate := func(spec string) *chroniclev1alpha1.WorkflowTemplate {
		return &chroniclev1alpha1.WorkflowTemplate{
			Name: "t-" + rand.String(6), Namespace: "default",
			Spec: templateSpec(spec),
		}
	}

	It("accepts a parameter without a value", func() {
		t := newTemplate("entrypoint: c\narguments: {parameters: [{name: p}]}\ntemplates: [" + container + "]")
		Expect(k8sClient.Create(ctx, t)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, t)

		By("rejecting an invalid spec change")
		t.Spec.Entrypoint = unknownName
		Expect(k8sClient.Update(ctx, t)).NotTo(Succeed())
	})

	It("rejects a template without an entrypoint", func() {
		expectRejected(newTemplate("templates: ["+container+"]"), "spec.entrypoint and spec.templates are required")
	})

})
