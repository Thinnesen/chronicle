package controller

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

var _ = Describe("Long run names", func() {
	ctx := context.Background()

	// A label value has at most 63 characters, so the Pod of a long root node has no node label.
	It("starts the Pod of a root node whose name is longer than 63 characters", func() {
		wf := &chroniclev1alpha1.Workflow{Name: "long-" + strings.Repeat("x", 65), Namespace: "default"}
		Expect(yaml.UnmarshalStrict([]byte(`{entrypoint: c, templates: [{name: c, container: {image: alpine, command: [run]}}]}`), &wf.Spec)).To(Succeed())
		Expect(k8sClient.Create(ctx, wf)).To(Succeed())
		DeferCleanup(func() error { return client.IgnoreNotFound(k8sClient.Delete(ctx, wf)) })
		got := reconcileWith(ctx, testReconciler(), types.NamespacedName{Name: wf.Name, Namespace: wf.Namespace})
		pods := podsOf(ctx, got)
		Expect(pods).To(HaveLen(1))
		Expect(pods[0].Labels).NotTo(HaveKey(LabelNodeID))
	})
})
