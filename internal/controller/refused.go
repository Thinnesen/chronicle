package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

// refusedRetry is the wait before the controller tries again an action of a node that failed for now, without the
// growing backoff of an error. So the run goes on soon after the cause is gone.
const refusedRetry = 10 * time.Second

// nodeProblem is an action of a node that failed in a way that the user must see in the node. A permanent problem, for
// example an invalid Pod, ends the node with Error. Other problems, for example a ResourceQuota that is full or a task
// queue that does not answer, put the reason on the pending node, and the controller tries again soon.
type nodeProblem struct {
	nodeID    string
	permanent bool
	err       error
}

func (e nodeProblem) Error() string { return e.err.Error() }

// refusal returns a nodeProblem for a refused Pod create, or nil for other errors.
func refusal(nodeID string, err error) error {
	switch {
	case apierrors.IsInvalid(err):
		return nodeProblem{nodeID: nodeID, permanent: true, err: fmt.Errorf("the API server refused the Pod: %w", err)}
	case apierrors.IsForbidden(err):
		return nodeProblem{nodeID: nodeID, err: fmt.Errorf("waiting: the API server refused the Pod: %w", err)}
	}
	return nil
}

// noteApplied writes what the controller learned while it applied the actions into the pending nodes: the message of
// each node that waits for a lock, and the refused Pods. A forbidden Pod makes the run try again soon.
func noteApplied(status *chroniclev1alpha1.WorkflowStatus, messages map[string]string, refused []nodeProblem, result *ctrl.Result) {
	for id, msg := range messages {
		// A node that ended in this step, for example by its timeout, keeps the message of its end.
		if n, ok := status.Nodes[id]; ok && n.Phase == chroniclev1alpha1.NodePending {
			n.Message = msg
			status.Nodes[id] = n
		}
	}
	if markRefused(status, refused) && (result.RequeueAfter == 0 || result.RequeueAfter > refusedRetry) {
		result.RequeueAfter = refusedRetry
	}
}

// markRefused writes the refusals into the nodes: an invalid Pod ends its node with Error, a forbidden Pod puts the
// reason on its pending node. It returns true when a node waits for a forbidden Pod.
func markRefused(status *chroniclev1alpha1.WorkflowStatus, refused []nodeProblem) bool {
	waits := false
	now := metav1.Now()
	for _, r := range refused {
		n, ok := status.Nodes[r.nodeID]
		if !ok || n.Phase != chroniclev1alpha1.NodePending {
			continue
		}
		n.Message = r.Error()
		if r.permanent {
			n.Phase, n.FinishedAt = chroniclev1alpha1.NodeError, &now
		} else {
			waits = true
		}
		status.Nodes[r.nodeID] = n
	}
	return waits
}

// errQueueDown is the node message while the task queue does not answer. The controller log has the details, which
// name the database.
var errQueueDown = errors.New("waiting: the task queue does not answer")

// queueDownError is a failed read of the task queue.
type queueDownError struct{ err error }

func (e queueDownError) Error() string { return e.err.Error() }
func (e queueDownError) Unwrap() error { return e.err }

// asRefusal tells if an apply error is a problem of its node.
func asRefusal(err error) (nodeProblem, bool) {
	return errors.AsType[nodeProblem](err)
}

// observeFailed returns the result of a reconcile whose observation failed. While the task queue does not answer, the
// run tries again soon without the growing backoff of an error.
func observeFailed(ctx context.Context, err error) (ctrl.Result, error) {
	if _, down := errors.AsType[queueDownError](err); down {
		logf.FromContext(ctx).Error(err, "The task queue does not answer")
		return ctrl.Result{RequeueAfter: refusedRetry}, nil
	}
	return ctrl.Result{}, err
}
