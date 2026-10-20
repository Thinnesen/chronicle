package controller

import (
	"context"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

// The other tests run as an admin. This one runs the controller with only the generated role (config/rbac/role.yaml),
// so a call without its verb fails here and not first on a cluster.
var _ = Describe("The RBAC role of the controller", func() {
	ctx := context.Background()

	It("is enough for a run with a lock and a volume", func() {
		c := limitedClient(ctx)
		r := &WorkflowReconciler{Client: c, APIReader: c, Scheme: c.Scheme(), ExecutorImage: testExecutorImage}

		lock := "rbac-" + rand.String(6)
		wf := &chroniclev1alpha1.Workflow{Name: "rbac-" + rand.String(6), Namespace: "default"}
		Expect(yaml.UnmarshalStrict([]byte(`
entrypoint: c
synchronization: {mutex: {name: `+lock+`}}
volumeClaimTemplates: [{name: work, storage: 1Gi}]
templates: [{name: c, container: {image: alpine, command: [run], volumeMounts: [{name: work, mountPath: /work}]}}]
`), &wf.Spec)).To(Succeed())
		Expect(k8sClient.Create(ctx, wf)).To(Succeed())
		DeferCleanup(func() error { return client.IgnoreNotFound(k8sClient.Delete(ctx, wf)) })
		key := types.NamespacedName{Name: wf.Name, Namespace: wf.Namespace}

		got := reconcileWith(ctx, r, key)
		Expect(got.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowRunning))
		pods := podsOf(ctx, got)
		Expect(pods).To(HaveLen(1))
		finishPod(ctx, &pods[0])
		Eventually(func() chroniclev1alpha1.WorkflowPhase { return reconcileWith(ctx, r, key).Status.Phase }).
			Should(Equal(chroniclev1alpha1.WorkflowSucceeded))
		// The release deleted the Lease, and the claim of the run is gone after it succeeded.
		err := k8sClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: lockPrefix + lock}, &coordinationv1.Lease{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "lease: %v", err)
		var claims corev1.PersistentVolumeClaimList
		Expect(k8sClient.List(ctx, &claims, client.InNamespace("default"))).To(Succeed())
		for _, pvc := range claims.Items {
			if pvc.DeletionTimestamp == nil {
				Expect(pvc.Name).NotTo(HavePrefix(wf.Name))
			}
		}
	})

	It("is enough for child runs and schedules", func() {
		c := limitedClient(ctx)
		r := &WorkflowReconciler{Client: c, APIReader: c, Scheme: c.Scheme(), ExecutorImage: testExecutorImage}
		tmpl := &chroniclev1alpha1.WorkflowTemplate{Name: "rbac-" + rand.String(6), Namespace: "default"}
		Expect(yaml.UnmarshalStrict([]byte(`{entrypoint: c, templates: [{name: c, container: {image: alpine}}]}`), &tmpl.Spec)).To(Succeed())
		Expect(k8sClient.Create(ctx, tmpl)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, tmpl)

		key := newWorkflow(ctx, `{entrypoint: c, templates: [{name: c, childWorkflow: {workflowTemplateRef: {name: `+tmpl.Name+`}}}]}`)
		parent := reconcileWith(ctx, r, key)
		var children chroniclev1alpha1.WorkflowList
		Expect(k8sClient.List(ctx, &children, client.InNamespace("default"), client.MatchingLabels{LabelParentUID: string(parent.UID)})).To(Succeed())
		Expect(children.Items).To(HaveLen(1))
		parent.Spec.Shutdown = chroniclev1alpha1.ShutdownStop
		Expect(k8sClient.Update(ctx, parent)).To(Succeed())
		Eventually(func() chroniclev1alpha1.WorkflowPhase { return reconcileWith(ctx, r, key).Status.Phase }).
			Should(Equal(chroniclev1alpha1.WorkflowFailed))

		cw := &chroniclev1alpha1.CronWorkflow{Name: "rbac-" + rand.String(6), Namespace: "default"}
		Expect(yaml.UnmarshalStrict([]byte(`{schedules: ["* * * * *"], workflowSpec: {workflowTemplateRef: {name: `+tmpl.Name+`}}}`), &cw.Spec)).To(Succeed())
		Expect(k8sClient.Create(ctx, cw)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, cw)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cw), cw)).To(Succeed())
		now := cw.CreationTimestamp.Add(2 * time.Minute)
		cr := &CronWorkflowReconciler{Client: c, Scheme: c.Scheme(), Now: func() time.Time { return now }}
		_, err := cr.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(cw)})
		Expect(err).NotTo(HaveOccurred())
		var runs chroniclev1alpha1.WorkflowList
		Expect(k8sClient.List(ctx, &runs, client.InNamespace("default"), client.MatchingLabels{LabelCronWorkflow: cw.Name})).To(Succeed())
		Expect(runs.Items).To(HaveLen(1))
		DeferCleanup(k8sClient.Delete, ctx, &runs.Items[0])
	})
})

// limitedClient returns a client of a new user that has only the generated role of the controller.
func limitedClient(ctx context.Context) client.Client {
	data, err := os.ReadFile("../../config/rbac/role.yaml")
	Expect(err).NotTo(HaveOccurred())
	role := &rbacv1.ClusterRole{}
	Expect(yaml.Unmarshal(data, role)).To(Succeed())
	user := "rbac-test-controller-" + rand.String(6)
	role.Name = user
	binding := &rbacv1.ClusterRoleBinding{Name: user,
		RoleRef:  rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: user},
		Subjects: []rbacv1.Subject{{APIGroup: "rbac.authorization.k8s.io", Kind: "User", Name: user}}}
	for _, o := range []client.Object{role, binding} {
		Expect(k8sClient.Create(ctx, o)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, o)
	}
	limited := rest.CopyConfig(cfg)
	limited.Impersonate = rest.ImpersonationConfig{UserName: user}
	c, err := client.New(limited, client.Options{Scheme: k8sClient.Scheme(), Mapper: k8sClient.RESTMapper()})
	Expect(err).NotTo(HaveOccurred())
	return c
}
