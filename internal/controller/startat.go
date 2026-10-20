package controller

import (
	"context"
	"strings"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

// waitingToStart starts the message of a run that waits for its start time.
const waitingToStart = "the run starts at "

// holdBack keeps a run from starting until its start time, and then until it holds its lock.
func (r *WorkflowReconciler) holdBack(ctx context.Context, wf *chroniclev1alpha1.Workflow, spec *chroniclev1alpha1.WorkflowTemplateSpec,
	observed *chroniclev1alpha1.WorkflowStatus) (ctrl.Result, bool, error) {
	if res, wait, err := r.waitForStart(ctx, wf, observed); wait || err != nil {
		return res, wait, err
	}
	return r.gate(ctx, wf, spec, observed)
}

// waitForStart keeps a run Pending until its start time. A stop goes on to the engine, which ends the run at once.
func (r *WorkflowReconciler) waitForStart(ctx context.Context, wf *chroniclev1alpha1.Workflow, observed *chroniclev1alpha1.WorkflowStatus) (ctrl.Result, bool, error) {
	at := wf.Spec.StartAt
	if at == nil || wf.Spec.Shutdown != "" || wf.Status.StartedAt != nil || finished(wf.Status.Phase) {
		return ctrl.Result{}, false, nil
	}
	wait := time.Until(at.Time)
	if wait <= 0 {
		if strings.HasPrefix(wf.Status.Message, waitingToStart) {
			wf.Status.Message = ""
		}
		return ctrl.Result{}, false, nil
	}
	wf.Status.Phase = chroniclev1alpha1.WorkflowPending
	wf.Status.Message = waitingToStart + at.UTC().Format(time.RFC3339)
	return r.writeWaiting(ctx, wf, observed, ctrl.Result{RequeueAfter: wait})
}
