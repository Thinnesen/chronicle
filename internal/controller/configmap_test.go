package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

var _ = Describe("parameters from a ConfigMap", func() {
	ctx := context.Background()

	configMap := func(labeled bool) string {
		cm := &corev1.ConfigMap{Name: "params-" + rand.String(6), Namespace: "default", Data: map[string]string{"region": "eu"}}
		if labeled {
			cm.Labels = map[string]string{LabelParameters: "true"}
		}
		Expect(k8sClient.Create(ctx, cm)).To(Succeed())
		DeferCleanup(func() error { return k8sClient.Delete(ctx, cm) })
		return cm.Name
	}
	spec := func(cm string) string {
		return `
entrypoint: main
arguments: {parameters: [{name: region, valueFrom: {configMapKeyRef: {name: ` + cm + `, key: region}}}]}
templates:
- {name: main, container: {image: alpine, args: ["{{workflow.parameters.region}}"]}}
`
	}

	It("reads a labeled ConfigMap once and keeps the value in the stored spec", func() {
		wf := reconcileWith(ctx, testReconciler(), newWorkflow(ctx, spec(configMap(true))))
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowRunning))
		Expect(*wf.Status.StoredSpec.Arguments.Parameters[0].Value).To(Equal("eu"))
		Expect(podsOf(ctx, wf)[0].Spec.Containers[0].Args).To(Equal([]string{"eu"}))
	})

	It("refuses a ConfigMap without the label", func() {
		wf := reconcileWith(ctx, testReconciler(), newWorkflow(ctx, spec(configMap(false))))
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowError))
		Expect(wf.Status.Message).To(ContainSubstring(`has no label chronicle.thinnesen.com/parameters: "true"`))
		Expect(podsOf(ctx, wf)).To(BeEmpty())
	})
})
