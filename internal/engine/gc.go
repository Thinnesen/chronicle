package engine

import (
	"maps"
	"slices"
	"time"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// gc returns DeletePod actions for the observed Pods that the Pod GC strategy permits to delete.
func (e *engine) gc(status *v1alpha1.WorkflowStatus) []Action {
	p := e.in.Spec.PodGC
	if p == nil {
		return nil
	}
	var delay time.Duration
	if p.DeleteDelayDuration != nil {
		delay = p.DeleteDelayDuration.Duration
	}
	var actions []Action
	for _, id := range slices.Sorted(maps.Keys(e.in.Pods)) {
		n, ok := status.Nodes[id]
		if !ok || !isTerminal(n.Phase) || n.FinishedAt == nil {
			continue
		}
		var since time.Time
		switch p.Strategy {
		case v1alpha1.PodGCOnPodCompletion:
			since = n.FinishedAt.Time
		case v1alpha1.PodGCOnPodSuccess:
			if n.Phase != v1alpha1.NodeSucceeded {
				continue
			}
			since = n.FinishedAt.Time
		case v1alpha1.PodGCOnWorkflowCompletion, v1alpha1.PodGCOnWorkflowSuccess:
			if status.FinishedAt == nil || p.Strategy == v1alpha1.PodGCOnWorkflowSuccess && status.Phase != v1alpha1.WorkflowSucceeded {
				continue
			}
			since = status.FinishedAt.Time
		}
		if e.reached(since.Add(delay)) {
			actions = append(actions, DeletePod{PodName: n.PodName})
		}
	}
	return actions
}

// TTL returns the time when a finished workflow must be deleted, or false when it must stay.
func TTL(spec *v1alpha1.WorkflowTemplateSpec, status v1alpha1.WorkflowStatus) (time.Time, bool) {
	t := spec.TTLStrategy
	if t == nil || status.FinishedAt == nil || !isFinished(status.Phase) {
		return time.Time{}, false
	}
	seconds := t.SecondsAfterCompletion
	if status.Phase == v1alpha1.WorkflowSucceeded && t.SecondsAfterSuccess != nil {
		seconds = t.SecondsAfterSuccess
	}
	if status.Phase != v1alpha1.WorkflowSucceeded && t.SecondsAfterFailure != nil {
		seconds = t.SecondsAfterFailure
	}
	if seconds == nil {
		return time.Time{}, false
	}
	return status.FinishedAt.Add(time.Duration(*seconds) * time.Second), true
}
