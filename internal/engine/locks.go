package engine

import (
	"maps"
	"slices"
	"time"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// lockPoll is the wait of a node for the lock of its template between two tries.
const lockPoll = 5 * time.Second

// waitingForLock starts the message of a node that waits for the lock of its template.
const waitingForLock = "waiting for the lock"

// AcquireLock asks the controller to take the lock of a template for a node. Limit is the number of holders, or
// LimitFrom names the ConfigMap key that gives it.
type AcquireLock struct {
	Lock      string
	Limit     int
	LimitFrom *v1alpha1.ConfigMapKeyRef
	NodeID    string
}

// ReleaseLock asks the controller to give the lock of a node back.
type ReleaseLock struct {
	Lock   string
	NodeID string
}

func (AcquireLock) isAction() {}
func (ReleaseLock) isAction() {}

// waitForLock tells if a node must wait for the lock of its template. Then it asks the controller for the lock.
func (e *engine) waitForLock(t *v1alpha1.Template, n *v1alpha1.NodeStatus) bool {
	name, limit, ok := t.Synchronization.Lock()
	if !ok || e.in.HeldLocks[n.ID] != "" {
		return false
	}
	n.Message = waitingForLock + " " + name
	e.actions = append(e.actions, AcquireLock{Lock: name, Limit: limit, LimitFrom: t.Synchronization.LimitFrom(), NodeID: n.ID})
	e.reached(e.in.Now.Add(lockPoll))
	return true
}

// releaseLocks gives back the lock of each node that has finished or that is no longer in the run.
func (e *engine) releaseLocks() {
	for _, id := range slices.Sorted(maps.Keys(e.in.HeldLocks)) {
		if n, ok := e.nodes[id]; !ok || IsTerminal(n.Phase) {
			e.actions = append(e.actions, ReleaseLock{Lock: e.in.HeldLocks[id], NodeID: id})
		}
	}
}
