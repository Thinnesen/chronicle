package controller

import (
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

// refusedRetry is the wait before the controller creates a Pod again that Kubernetes refused for now.
const refusedRetry = 10 * time.Second

// refusedPodError is a Pod create that Kubernetes refused. An invalid Pod can never start, so the node ends. A forbidden
// Pod, for example by the Pod security policy or a ResourceQuota of the namespace, can start later, so the node waits.
type refusedPodError struct {
	nodeID    string
	permanent bool
	err       error
}

func (e refusedPodError) Error() string { return e.err.Error() }

// refusal returns a refusedPodError for a refused Pod create, or nil for other errors.
func refusal(nodeID string, err error) error {
	switch {
	case apierrors.IsInvalid(err):
		return refusedPodError{nodeID: nodeID, permanent: true, err: fmt.Errorf("the API server refused the Pod: %w", err)}
	case apierrors.IsForbidden(err):
		return refusedPodError{nodeID: nodeID, err: fmt.Errorf("waiting: the API server refused the Pod: %w", err)}
	}
	return nil
}

// noteApplied writes what the controller learned while it applied the actions into the pending nodes: the message of
// each node that waits for a lock, and the refused Pods. A forbidden Pod makes the run try again soon.
func noteApplied(status *chroniclev1alpha1.WorkflowStatus, messages map[string]string, refused []refusedPodError, result *ctrl.Result) {
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
func markRefused(status *chroniclev1alpha1.WorkflowStatus, refused []refusedPodError) bool {
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

// asRefusal tells if an apply error is a refused Pod.
func asRefusal(err error) (refusedPodError, bool) {
	var r refusedPodError
	return r, errors.As(err, &r)
}
