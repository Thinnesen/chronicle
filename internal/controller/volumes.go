package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/resolve"
)

// conditionVolumes tells that the claims of the volume claim templates of a run exist.
const conditionVolumes = "Volumes"

// claimName is the name of the claim of a volume claim template in a run.
func claimName(wf *chroniclev1alpha1.Workflow, template string) string {
	return wf.Name + "-" + template
}

// claims creates the claims of an unfinished run, and deletes them when the run has succeeded. A failed run keeps
// them, so that a retry finds the files of the steps that succeeded. The owner reference deletes them with the run.
func (r *WorkflowReconciler) claims(ctx context.Context, wf *chroniclev1alpha1.Workflow, spec *chroniclev1alpha1.WorkflowTemplateSpec) error {
	if len(spec.VolumeClaimTemplates) == 0 {
		return nil
	}
	created := meta.IsStatusConditionTrue(wf.Status.Conditions, conditionVolumes)
	switch {
	case !finished(wf.Status.Phase) && !created:
		for _, t := range spec.VolumeClaimTemplates {
			if err := r.createClaim(ctx, wf, t); err != nil {
				return err
			}
		}
		setVolumes(wf, true, "Created")
	case wf.Status.Phase == chroniclev1alpha1.WorkflowSucceeded && created:
		for _, t := range spec.VolumeClaimTemplates {
			pvc := &corev1.PersistentVolumeClaim{Name: claimName(wf, t.Name), Namespace: wf.Namespace}
			if err := r.Delete(ctx, pvc); client.IgnoreNotFound(err) != nil {
				return fmt.Errorf("delete the claim %s: %w", pvc.Name, err)
			}
		}
		setVolumes(wf, false, "Deleted")
	}
	return nil
}

// createClaim creates the claim of a volume claim template. A claim with the name that the run does not own is an
// error: the run must never get the files of another owner.
func (r *WorkflowReconciler) createClaim(ctx context.Context, wf *chroniclev1alpha1.Workflow, t chroniclev1alpha1.VolumeClaimTemplate) error {
	modes := t.AccessModes
	if len(modes) == 0 {
		modes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	}
	pvc := &corev1.PersistentVolumeClaim{
		Name:            claimName(wf, t.Name),
		Namespace:       wf.Namespace,
		Labels:          map[string]string{LabelWorkflowUID: string(wf.UID)},
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(wf, chroniclev1alpha1.GroupVersion.WithKind("Workflow"))},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      modes,
			StorageClassName: t.StorageClassName,
			Resources:        corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: t.Storage}},
		},
	}
	err := r.Create(ctx, pvc)
	if apierrors.IsAlreadyExists(err) {
		var old corev1.PersistentVolumeClaim
		if err := r.reader().Get(ctx, client.ObjectKeyFromObject(pvc), &old); err != nil {
			return fmt.Errorf("read the claim %s: %w", pvc.Name, err)
		}
		if !metav1.IsControlledBy(&old, wf) {
			return resolve.Invalidf("the claim %q exists, and the run does not own it", pvc.Name)
		}
		return nil
	}
	if apierrors.IsInvalid(err) {
		return resolve.Invalidf("create the claim %s: %v", pvc.Name, err)
	}
	if err != nil {
		return fmt.Errorf("create the claim %s: %w", pvc.Name, err)
	}
	return nil
}

func setVolumes(wf *chroniclev1alpha1.Workflow, created bool, reason string) {
	status := metav1.ConditionFalse
	if created {
		status = metav1.ConditionTrue
	}
	meta.SetStatusCondition(&wf.Status.Conditions, metav1.Condition{Type: conditionVolumes, Status: status, Reason: reason, ObservedGeneration: wf.Generation})
}

// mountVolumes adds the volume mounts of a template to its main container, and the volumes to the Pod.
func mountVolumes(wf *chroniclev1alpha1.Workflow, spec *chroniclev1alpha1.WorkflowTemplateSpec, mounts []chroniclev1alpha1.VolumeMount,
	main *corev1.Container, volumes []corev1.Volume) []corev1.Volume {
	added := map[string]bool{}
	for _, m := range mounts {
		main.VolumeMounts = append(main.VolumeMounts, corev1.VolumeMount{Name: m.Name, MountPath: m.MountPath, SubPath: m.SubPath, ReadOnly: m.ReadOnly})
		if added[m.Name] {
			continue
		}
		added[m.Name] = true
		if v, ok := podVolume(wf, spec, m.Name); ok {
			volumes = append(volumes, v)
		}
	}
	return volumes
}

// podVolume returns the Pod volume for a volume or a volume claim template of the workflow.
func podVolume(wf *chroniclev1alpha1.Workflow, spec *chroniclev1alpha1.WorkflowTemplateSpec, name string) (corev1.Volume, bool) {
	for _, t := range spec.VolumeClaimTemplates {
		if t.Name == name {
			return corev1.Volume{Name: name, PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claimName(wf, name)}}, true
		}
	}
	for _, v := range spec.Volumes {
		if v.Name == name {
			return corev1.Volume{Name: name, EmptyDir: v.EmptyDir, Secret: v.Secret, ConfigMap: v.ConfigMap}, true
		}
	}
	return corev1.Volume{}, false
}
