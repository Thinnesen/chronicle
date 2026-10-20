package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

var _ = Describe("CronWorkflow Controller", func() {
	ctx := context.Background()

	It("starts each due run once and replaces an active run", func() {
		cw := &chroniclev1alpha1.CronWorkflow{Name: "cw-" + rand.String(6), Namespace: "default"}
		Expect(yaml.UnmarshalStrict([]byte(`
schedules: ["*/10 * * * *"]
concurrencyPolicy: Replace
workflowMetadata: {labels: {team: a}}
workflowSpec: {entrypoint: c, templates: [{name: c, container: {image: alpine}}]}
`), &cw.Spec)).To(Succeed())
		Expect(k8sClient.Create(ctx, cw)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, cw)
		key := types.NamespacedName{Name: cw.Name, Namespace: cw.Namespace}
		Expect(k8sClient.Get(ctx, key, cw)).To(Succeed())

		// The first schedule time after the creation.
		first := cw.CreationTimestamp.Truncate(10 * time.Minute).Add(10 * time.Minute)
		now := first.Add(time.Minute)
		r := &CronWorkflowReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Now: func() time.Time { return now }}
		runs := func() []chroniclev1alpha1.Workflow {
			var list chroniclev1alpha1.WorkflowList
			Expect(k8sClient.List(ctx, &list, client.InNamespace("default"), client.MatchingLabels{LabelCronWorkflow: cw.Name})).To(Succeed())
			return list.Items
		}

		for range 2 {
			res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(Equal(9 * time.Minute))
		}
		Expect(runs()).To(HaveLen(1))
		run := runs()[0]
		Expect(run.Labels).To(HaveKeyWithValue("team", "a"))
		Expect(run.Annotations).To(HaveKeyWithValue(AnnotationScheduledTime, first.UTC().Format(time.RFC3339)))

		now = now.Add(10 * time.Minute)
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(runs()).To(HaveLen(2))
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&run), &run)).To(Succeed())
		Expect(run.Spec.Shutdown).To(Equal(chroniclev1alpha1.ShutdownTerminate))
		Expect(k8sClient.Get(ctx, key, cw)).To(Succeed())
		Expect(cw.Status.Active).To(HaveLen(1))
	})
	It("keeps the time of the last started run when it skips a run", func() {
		cw := &chroniclev1alpha1.CronWorkflow{Name: "cw-" + rand.String(6), Namespace: "default"}
		Expect(yaml.UnmarshalStrict([]byte(`
schedules: ["*/10 * * * *"]
concurrencyPolicy: Forbid
workflowSpec: {entrypoint: c, templates: [{name: c, container: {image: alpine}}]}
`), &cw.Spec)).To(Succeed())
		Expect(k8sClient.Create(ctx, cw)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, cw)
		key := types.NamespacedName{Name: cw.Name, Namespace: cw.Namespace}
		Expect(k8sClient.Get(ctx, key, cw)).To(Succeed())
		first := cw.CreationTimestamp.Truncate(10 * time.Minute).Add(10 * time.Minute)
		now := first.Add(time.Minute)
		r := &CronWorkflowReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Now: func() time.Time { return now }}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		// The first run is still active, so Forbid skips the second schedule time.
		now = now.Add(10 * time.Minute)
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, key, cw)).To(Succeed())
		Expect(cw.Status.LastScheduledTime.Time).To(BeTemporally("==", first.Add(10*time.Minute)))
		Expect(cw.Status.LastStartedTime).NotTo(BeNil())
		Expect(cw.Status.LastStartedTime.Time).To(BeTemporally("==", first))
	})
})
