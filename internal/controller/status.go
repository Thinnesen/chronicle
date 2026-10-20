package controller

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

func finished(p chroniclev1alpha1.WorkflowPhase) bool {
	return p == chroniclev1alpha1.WorkflowSucceeded || p == chroniclev1alpha1.WorkflowFailed || p == chroniclev1alpha1.WorkflowError
}

// setError ends a run with phase Error and a message.
func (r *WorkflowReconciler) setError(ctx context.Context, wf *chroniclev1alpha1.Workflow, msg string) (ctrl.Result, error) {
	if wf.Status.Phase == chroniclev1alpha1.WorkflowError {
		return ctrl.Result{}, nil
	}
	now := metav1.Now()
	wf.Status.Phase, wf.Status.Message, wf.Status.FinishedAt = chroniclev1alpha1.WorkflowError, msg, &now
	if err := r.Status().Update(ctx, wf); err != nil {
		return ctrl.Result{}, fmt.Errorf("update the workflow status: %w", err)
	}
	return ctrl.Result{}, nil
}
