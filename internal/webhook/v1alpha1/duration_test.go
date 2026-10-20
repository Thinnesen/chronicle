package v1alpha1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/yaml"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

var _ = Describe("Durations", func() {
	// The object is unstructured, because the Go types cannot hold a bad duration.
	unstructuredWorkflow := func(spec string) *unstructured.Unstructured {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(chroniclev1alpha1.GroupVersion.WithKind("Workflow"))
		obj.SetName("wf-" + rand.String(6))
		obj.SetNamespace("default")
		var s map[string]any
		Expect(yaml.Unmarshal([]byte(spec), &s)).To(Succeed())
		obj.Object["spec"] = s
		return obj
	}

	It("names the field of a bad duration", func() {
		expectRejected(unstructuredWorkflow("entrypoint: c\ntemplates: [{name: c, timeout: 5x, container: {image: alpine}}]"),
			"spec.templates[0].timeout", `"5x"`)
	})

	It("accepts Go durations", func() {
		wf := unstructuredWorkflow("entrypoint: c\ntemplates: [{name: c, timeout: 1h30m, container: {image: alpine}}, {name: w, suspend: {duration: 1.5s}}]")
		Expect(k8sClient.Create(ctx, wf)).To(Succeed())
		Expect(k8sClient.Delete(ctx, wf)).To(Succeed())
	})
})
