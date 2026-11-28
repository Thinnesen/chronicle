package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/engine"
)

// runTooLarge is the reason of the tasks that the controller cancels when a run is too large.
const runTooLarge = "the run is too large"

// defaultMaxObjectBytes is the largest Workflow that the controller writes. etcd refuses objects above about 1.5 MiB.
// One step of the engine can add many nodes, so the limit keeps room.
const defaultMaxObjectBytes = 1 << 20

// tooLarge returns the size of the Workflow with the new status when it is above the limit, or 0.
func (r *WorkflowReconciler) tooLarge(wf *chroniclev1alpha1.Workflow, status chroniclev1alpha1.WorkflowStatus) int {
	limit := r.MaxObjectBytes
	if limit <= 0 {
		limit = defaultMaxObjectBytes
	}
	next := *wf
	next.Status = status
	data, err := json.Marshal(&next)
	if err != nil || len(data) <= limit {
		return 0
	}
	return len(data)
}

// failTooLarge ends a run whose status would be too large. It keeps the last saved status, which fits. It ends the open
// nodes with Error, deletes the Pods that still run, cancels the open tasks and terminates the child runs. The engine
// does not create the new Pods.
func (r *WorkflowReconciler) failTooLarge(ctx context.Context, wf *chroniclev1alpha1.Workflow, size int) (ctrl.Result, error) {
	// The cleanup comes before the Error phase, because nothing cleans up a finished run. A failed cleanup returns the
	// error, so the reconcile tries again.
	if err := r.stopTooLarge(ctx, wf); err != nil {
		return ctrl.Result{}, err
	}
	msg := fmt.Sprintf("the run is too large: its status would have %d KiB. Use fewer items in a fan-out, "+
		"or split the work into more runs", size>>10)
	now := metav1.Now()
	for id, n := range wf.Status.Nodes {
		if !engine.IsTerminal(n.Phase) {
			n.Phase, n.Message, n.FinishedAt = chroniclev1alpha1.NodeError, "stopped: the run is too large", &now
			wf.Status.Nodes[id] = n
		}
	}
	ctrl.LoggerFrom(ctx).Info("The run is too large", "bytes", size)
	return r.setError(ctx, wf, msg)
}

// stopTooLarge deletes the Pods that still run, cancels the open tasks and terminates the child runs of a run.
func (r *WorkflowReconciler) stopTooLarge(ctx context.Context, wf *chroniclev1alpha1.Workflow) error {
	var pods corev1.PodList
	if err := r.List(ctx, &pods, client.InNamespace(wf.Namespace), client.MatchingLabels{LabelWorkflowUID: string(wf.UID)}); err != nil {
		return fmt.Errorf("list the workflow Pods: %w", err)
	}
	for i := range pods.Items {
		if p := pods.Items[i].Status.Phase; p != corev1.PodSucceeded && p != corev1.PodFailed {
			if err := r.Delete(ctx, &pods.Items[i]); client.IgnoreNotFound(err) != nil {
				return fmt.Errorf("delete the Pod %s: %w", pods.Items[i].Name, err)
			}
		}
	}
	// Only a run with an open task needs the queue, as in observeTasks.
	if r.Tasks != nil && slices.ContainsFunc(slices.Collect(maps.Values(wf.Status.Nodes)), openTask) {
		if err := r.Tasks.CancelRun(ctx, string(wf.UID), runTooLarge); err != nil {
			return err
		}
	}
	for _, n := range wf.Status.Nodes {
		if n.ChildWorkflow != "" && !engine.IsTerminal(n.Phase) {
			a := engine.ShutdownWorkflow{Name: n.ChildWorkflow, Mode: chroniclev1alpha1.ShutdownTerminate}
			if err := r.shutdownChild(ctx, wf, a); err != nil {
				return err
			}
		}
	}
	return nil
}
