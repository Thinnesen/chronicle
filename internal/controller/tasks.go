package controller

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/archive"
	"github.com/Thinnesen/chronicle/internal/engine"
)

// TaskQueue holds the tasks of the task templates. The archive database implements it.
type TaskQueue interface {
	Enqueue(ctx context.Context, t archive.NewTask) error
	Cancel(ctx context.Context, id, reason string) error
	ForWorkflow(ctx context.Context, workflowUID string) ([]archive.Task, error)
}

// observeTasks returns the state of the tasks of a workflow by task ID.
func (r *WorkflowReconciler) observeTasks(ctx context.Context, wf *chroniclev1alpha1.Workflow) (map[string]engine.TaskResult, error) {
	// Only an open task node needs the queue. So an outage of its database stops only the runs with task workers.
	if r.Tasks == nil || !slices.ContainsFunc(slices.Collect(maps.Values(wf.Status.Nodes)), openTask) {
		return nil, nil
	}
	list, err := r.Tasks.ForWorkflow(ctx, string(wf.UID))
	if err != nil {
		return nil, err
	}
	out := make(map[string]engine.TaskResult, len(list))
	for _, t := range list {
		out[t.ID] = taskResult(t)
	}
	return out, nil
}

func openTask(n chroniclev1alpha1.NodeStatus) bool {
	return n.Type == chroniclev1alpha1.NodeTask && n.FinishedAt == nil
}

func taskResult(t archive.Task) engine.TaskResult {
	r := engine.TaskResult{Message: t.Message, Output: t.Output,
		StartedAt: metaTime(t.StartedAt), HeartbeatAt: metaTime(t.HeartbeatAt), FinishedAt: metaTime(t.FinishedAt)}
	switch t.State {
	case archive.TaskPending:
		r.Phase = chroniclev1alpha1.NodePending
	case archive.TaskRunning:
		r.Phase = chroniclev1alpha1.NodeRunning
	case archive.TaskSucceeded:
		r.Phase = chroniclev1alpha1.NodeSucceeded
	case archive.TaskError:
		r.Phase = chroniclev1alpha1.NodeError
	default:
		// failed, and cancelled: the engine cancels a task only after it failed the node.
		r.Phase = chroniclev1alpha1.NodeFailed
	}
	return r
}

// applyTask puts a task into the queue or cancels it.
func (r *WorkflowReconciler) applyTask(ctx context.Context, wf *chroniclev1alpha1.Workflow, a engine.Action) error {
	if r.Tasks == nil {
		return nil
	}
	switch a := a.(type) {
	case engine.EnqueueTask:
		err := r.Tasks.Enqueue(ctx, archive.NewTask{
			ID: a.TaskID, Namespace: wf.Namespace, WorkflowUID: string(wf.UID), Workflow: wf.Name,
			NodeID: a.NodeID, NodeName: a.NodeName, IdempotencyKey: a.IdempotencyKey, Queue: a.Queue, Name: a.Name, Input: a.Input,
			Traceparent: r.traceparent(wf, a.NodeID),
		})
		if err != nil {
			return fmt.Errorf("enqueue the task of node %s: %w", a.NodeName, err)
		}
	case engine.CancelTask:
		if err := r.Tasks.Cancel(ctx, a.TaskID, a.Reason); err != nil {
			return fmt.Errorf("cancel the task %s: %w", a.TaskID, err)
		}
	}
	return nil
}

func metaTime(t *time.Time) *metav1.Time {
	if t == nil {
		return nil
	}
	m := metav1.NewTime(*t)
	return &m
}
