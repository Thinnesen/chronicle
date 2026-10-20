package controller

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/engine"
)

// defaultMaxObjectBytes is the largest Workflow that the controller writes. etcd refuses objects above about 1.5 MiB,
// and one step of the engine can add many nodes, so the limit keeps room.
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

// failTooLarge ends a run whose status would be too large. It keeps the last saved status, which fits, ends its
// open nodes with Error and deletes the Pods that still run. The engine does not create the new Pods.
func (r *WorkflowReconciler) failTooLarge(ctx context.Context, wf *chroniclev1alpha1.Workflow, size int) (ctrl.Result, error) {
	var pods corev1.PodList
	if err := r.List(ctx, &pods, client.InNamespace(wf.Namespace), client.MatchingLabels{LabelWorkflowUID: string(wf.UID)}); err != nil {
		return ctrl.Result{}, fmt.Errorf("list the workflow Pods: %w", err)
	}
	for i := range pods.Items {
		if p := pods.Items[i].Status.Phase; p != corev1.PodSucceeded && p != corev1.PodFailed {
			if err := r.Delete(ctx, &pods.Items[i]); client.IgnoreNotFound(err) != nil {
				return ctrl.Result{}, fmt.Errorf("delete the Pod %s: %w", pods.Items[i].Name, err)
			}
		}
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
