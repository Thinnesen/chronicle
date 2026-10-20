package engine

import (
	"time"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// reached tells if the time t has come. If not, it sets a timer for t.
func (e *engine) reached(t time.Time) bool {
	if !e.in.Now.Time.Before(t) {
		return true
	}
	if e.wakeAt == nil || t.Before(*e.wakeAt) {
		e.wakeAt = &t
	}
	return false
}

func (e *engine) requeueAfter() time.Duration {
	if e.wakeAt == nil {
		return 0
	}
	return max(e.wakeAt.Sub(e.in.Now.Time), time.Second)
}

// stop fails a node and all its running descendants, deletes their Pods and cancels their tasks.
func (e *engine) stop(id, msg string) {
	n, ok := e.nodes[id]
	if !ok || IsTerminal(n.Phase) {
		return
	}
	for _, c := range n.Children {
		e.stop(c, msg)
	}
	n = e.nodes[id]
	if n.PodName != "" {
		e.actions = append(e.actions, DeletePod{PodName: n.PodName})
	}
	if n.TaskID != "" {
		e.actions = append(e.actions, CancelTask{TaskID: n.TaskID, Reason: msg})
	}
	e.setPhase(&n, v1alpha1.NodeFailed, msg)
	e.save(n)
}
