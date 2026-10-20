package v1alpha1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/util/rand"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

var _ = Describe("ClusterWorkflowTemplate validation", func() {
	It("accepts a valid template and rejects a cycle", func() {
		t := &chroniclev1alpha1.ClusterWorkflowTemplate{Name: "ct-" + rand.String(6), Spec: templateSpec("entrypoint: c\ntemplates: [" + container + "]")}
		Expect(k8sClient.Create(ctx, t)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, t)

		bad := &chroniclev1alpha1.ClusterWorkflowTemplate{Name: "ct-" + rand.String(6),
			Spec: templateSpec("entrypoint: m\ntemplates: [{name: m, dag: {tasks: [{name: a, template: c, dependencies: [a]}]}}, " + container + "]")}
		expectRejected(bad, "the DAG has a cycle")

		// An update gets the same checks as a create.
		t.Spec.Entrypoint = "nope"
		err := k8sClient.Update(ctx, t)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.entrypoint"))
	})
})
