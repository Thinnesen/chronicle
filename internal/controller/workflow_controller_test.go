package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

var _ = Describe("Workflow Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName      = "test-resource"
			resourceNamespace = "default"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: resourceNamespace,
		}
		workflow := &chroniclev1alpha1.Workflow{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Workflow")
			err := k8sClient.Get(ctx, typeNamespacedName, workflow)
			if err != nil && errors.IsNotFound(err) {
				resource := &chroniclev1alpha1.Workflow{
					Name:      resourceName,
					Namespace: resourceNamespace,
					Spec: chroniclev1alpha1.WorkflowSpec{
						WorkflowTemplateSpec: chroniclev1alpha1.WorkflowTemplateSpec{
							Entrypoint: "main",
							Templates: []chroniclev1alpha1.Template{{
								Name:      "main",
								Container: &chroniclev1alpha1.ContainerTemplate{Image: "alpine"},
							}},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &chroniclev1alpha1.Workflow{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Workflow")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &WorkflowReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})
