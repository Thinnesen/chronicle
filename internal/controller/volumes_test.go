package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

var _ = Describe("volumes", func() {
	ctx := context.Background()
	const spec = `
entrypoint: main
volumeClaimTemplates: [{name: work, storage: 1Gi}]
volumes: [{name: scratch, emptyDir: {}}, {name: creds, secret: {secretName: db}}]
templates:
- name: main
  container:
    image: alpine
    command: [build]
    volumeMounts: [{name: work, mountPath: /work}, {name: scratch, mountPath: /scratch}, {name: creds, mountPath: /creds, readOnly: true}]
`
	claim := func(wf *chroniclev1alpha1.Workflow) (*corev1.PersistentVolumeClaim, error) {
		var pvc corev1.PersistentVolumeClaim
		err := k8sClient.Get(ctx, client.ObjectKey{Namespace: wf.Namespace, Name: claimName(wf, "work")}, &pvc)
		return &pvc, err
	}

	It("creates the claims of a run, mounts the volumes and deletes the claims after success", func() {
		r := testReconciler()
		key := newWorkflow(ctx, spec)
		wf := reconcileWith(ctx, r, key)
		pvc, err := claim(wf)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc.Name).To(MatchRegexp(`^` + wf.Name + `-work-[0-9a-f]{8}$`))
		Expect(pvc.Spec.Resources.Requests[corev1.ResourceStorage]).To(Equal(resource.MustParse("1Gi")))
		Expect(pvc.Spec.AccessModes).To(Equal([]corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}))
		Expect(pvc.OwnerReferences).To(ContainElement(HaveField("UID", wf.UID)))

		pods := podsOf(ctx, wf)
		Expect(pods).To(HaveLen(1))
		Expect(pods[0].Spec.Volumes).To(ContainElements(
			corev1.Volume{Name: "work", PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvc.Name}},
			HaveField("Name", "scratch"), HaveField("Secret.SecretName", "db")))
		Expect(pods[0].Spec.Containers[0].VolumeMounts).To(ContainElements(
			corev1.VolumeMount{Name: "work", MountPath: "/work"}, corev1.VolumeMount{Name: "creds", MountPath: "/creds", ReadOnly: true}))

		finishPod(ctx, &pods[0])
		wf = reconcileWith(ctx, r, key)
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowSucceeded))
		reconcileWith(ctx, r, key)
		pvc, err = claim(wf)
		// envtest has no garbage collector, so the claim keeps the protection finalizer and stays with a deletion time.
		Expect(err == nil && pvc.DeletionTimestamp != nil || client.IgnoreNotFound(err) == nil && err != nil).To(BeTrue())
	})

	It("does not use a claim that the run does not own", func() {
		key := newWorkflow(ctx, spec)
		var created chroniclev1alpha1.Workflow
		Expect(k8sClient.Get(ctx, key, &created)).To(Succeed())
		taken := &corev1.PersistentVolumeClaim{Name: claimName(&created, "work"), Namespace: key.Namespace, Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources:   corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}},
		}}
		Expect(k8sClient.Create(ctx, taken)).To(Succeed())
		DeferCleanup(func() error { return k8sClient.Delete(ctx, taken) })
		wf := reconcileWith(ctx, testReconciler(), key)
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowError))
		Expect(wf.Status.Message).To(ContainSubstring(`the claim "` + taken.Name + `" exists, and the run does not own it`))
		Expect(podsOf(ctx, wf)).To(BeEmpty())
	})

	It("creates no claim for a spec that is not valid", func() {
		wf := reconcileWith(ctx, testReconciler(), newWorkflow(ctx, `
entrypoint: main
volumeClaimTemplates: [{name: work, storage: 1Gi}]
templates: [{name: main, container: {image: alpine, command: [run], volumeMounts: [{name: nope, mountPath: /n}]}}]
`))
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowError))
		_, err := claim(wf)
		Expect(err).To(Satisfy(apierrors.IsNotFound))
	})
})
