package controller

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/engine"
)

func finished(p chroniclev1alpha1.WorkflowPhase) bool {
	return p == chroniclev1alpha1.WorkflowSucceeded || p == chroniclev1alpha1.WorkflowFailed || p == chroniclev1alpha1.WorkflowError
}

// setError ends a run with phase Error and a message, before the engine runs. Such a run is archived, counted and
// deleted after its TTL like any other finished run.
func (r *WorkflowReconciler) setError(ctx context.Context, wf *chroniclev1alpha1.Workflow, msg string) (ctrl.Result, error) {
	observed := wf.Status.DeepCopy()
	first := wf.Status.Phase != chroniclev1alpha1.WorkflowError
	if first {
		now := metav1.Now()
		wf.Status.Phase, wf.Status.Message, wf.Status.FinishedAt = chroniclev1alpha1.WorkflowError, msg, &now
	}
	archiveErr := r.archive(ctx, wf)
	if !equality.Semantic.DeepEqual(*observed, wf.Status) {
		if err := r.Status().Update(ctx, wf); err != nil {
			return ctrl.Result{}, fmt.Errorf("update the workflow status: %w", err)
		}
	}
	if first {
		r.finishedRun(wf)
		r.traceRun(ctx, wf)
	}
	if archiveErr != nil {
		return ctrl.Result{}, archiveErr
	}
	return r.applyTTL(ctx, wf, ctrl.Result{})
}

// applyTTL deletes a finished workflow when its TTL is over, or runs again when it will be over.
func (r *WorkflowReconciler) applyTTL(ctx context.Context, wf *chroniclev1alpha1.Workflow, result ctrl.Result) (ctrl.Result, error) {
	spec := &wf.Spec.WorkflowTemplateSpec
	if wf.Status.StoredSpec != nil {
		spec = wf.Status.StoredSpec
	}
	when, ok := engine.TTL(spec, wf.Status)
	// With an archive, the TTL deletes a run only after the archive has it.
	if !ok || r.Archive != nil && !meta.IsStatusConditionTrue(wf.Status.Conditions, conditionArchived) {
		return result, nil
	}
	if wait := time.Until(when); wait > 0 {
		if result.RequeueAfter == 0 || wait < result.RequeueAfter {
			result.RequeueAfter = wait
		}
		return result, nil
	}
	logf.FromContext(ctx).Info("Deleting the workflow after its TTL")
	if err := r.Delete(ctx, wf, client.PropagationPolicy(metav1.DeletePropagationBackground)); client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, fmt.Errorf("delete the workflow after its TTL: %w", err)
	}
	return ctrl.Result{}, nil
}
