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

// OpenTaskQueue lists the runs with open tasks and cancels the open tasks of a run. The archive database implements it.
type OpenTaskQueue interface {
	OpenRuns(ctx context.Context) ([]archive.OpenRun, error)
	CancelRun(ctx context.Context, workflowUID, reason string) error
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
	runs, err := s.Tasks.OpenRuns(ctx)
	if err != nil {
		return err
	}
	for _, r := range runs {
		var wf chroniclev1alpha1.Workflow
		err := s.Reader.Get(ctx, client.ObjectKey{Namespace: r.Namespace, Name: r.Workflow}, &wf)
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("read the run %s/%s: %w", r.Namespace, r.Workflow, err)
		}
		if err == nil && string(wf.UID) == r.WorkflowUID {
			continue
		}
		if err := s.Tasks.CancelRun(ctx, r.WorkflowUID, runDeleted); err != nil {
			return err
		}
	}
	return nil
}
