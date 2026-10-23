package controller

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/archive"
)

// runDeleted is the reason of a task that the sweeper cancels.
const runDeleted = "the run was deleted"

// sweepBatch is the number of open tasks that one sweep reads.
const sweepBatch = 1000

// OpenTaskQueue lists the open tasks and cancels a task. The archive database implements it.
type OpenTaskQueue interface {
	OpenTasks(ctx context.Context, limit int) ([]archive.Task, error)
	Cancel(ctx context.Context, id, reason string) error
}

// TaskSweeper cancels the open tasks of runs that were deleted. Kubernetes deletes the Pods of a deleted run, but its
// tasks are in the database: a pending task would go to a worker, and a running one would keep its worker busy. The
// worker learns of the cancel with its next heartbeat. Only the leader runs the sweeper.
type TaskSweeper struct {
	Reader client.Reader
	Tasks  OpenTaskQueue
	Every  time.Duration
}

// Start implements manager.Runnable. A failed sweep is logged; the next tick tries again.
func (s *TaskSweeper) Start(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("task-sweeper")
	tick := time.NewTicker(s.Every)
	defer tick.Stop()
	for {
		if err := s.sweep(ctx); err != nil && ctx.Err() == nil {
			log.Error(err, "Cannot cancel the tasks of deleted runs")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

// sweep cancels the open tasks whose run does not exist. The UID decides, because a new run can have the same name.
func (s *TaskSweeper) sweep(ctx context.Context) error {
	open, err := s.Tasks.OpenTasks(ctx, sweepBatch)
	if err != nil {
		return err
	}
	for _, t := range open {
		var wf chroniclev1alpha1.Workflow
		err := s.Reader.Get(ctx, client.ObjectKey{Namespace: t.Namespace, Name: t.Workflow}, &wf)
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("read the run of task %s: %w", t.ID, err)
		}
		if err == nil && string(wf.UID) == t.WorkflowUID {
			continue
		}
		if err := s.Tasks.Cancel(ctx, t.ID, runDeleted); err != nil {
			return err
		}
	}
	return nil
}
