package controller

import (
	"context"
	"errors"
	"testing"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/archive"
)

// downQueue is a task queue whose database does not answer.
type downQueue struct{}

func (downQueue) Enqueue(context.Context, archive.NewTask) error { return errors.New("down") }
func (downQueue) Cancel(context.Context, string, string) error   { return errors.New("down") }
func (downQueue) ForWorkflow(context.Context, string) ([]archive.Task, error) {
	return nil, errors.New("down")
}

// TestObserveTasksOnlyWhenNeeded checks that a run without an open task node does not read the queue, so that an
// outage of the database does not stop the runs that do not use task workers.
func TestObserveTasksOnlyWhenNeeded(t *testing.T) {
	r := &WorkflowReconciler{Tasks: downQueue{}}
	run := func(nodes map[string]chroniclev1alpha1.NodeStatus) *chroniclev1alpha1.Workflow {
		return &chroniclev1alpha1.Workflow{Status: chroniclev1alpha1.WorkflowStatus{Nodes: nodes}}
	}
	end := run(nil).CreationTimestamp
	if _, err := r.observeTasks(context.Background(), run(map[string]chroniclev1alpha1.NodeStatus{
		"p": {Type: chroniclev1alpha1.NodePod},
		"t": {Type: chroniclev1alpha1.NodeTask, FinishedAt: &end},
	})); err != nil {
		t.Errorf("a run without an open task node read the queue: %v", err)
	}
	if _, err := r.observeTasks(context.Background(), run(map[string]chroniclev1alpha1.NodeStatus{
		"t": {Type: chroniclev1alpha1.NodeTask},
	})); err == nil {
		t.Error("a run with an open task node must read the queue")
	}
}
