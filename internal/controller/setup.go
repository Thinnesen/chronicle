package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/source"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

// SetupWithManager sets up the controller with the Manager. It watches the Pods that the controller owns, and the
// runs that a lock or a task result wakes.
func (r *WorkflowReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.wake = make(chan event.GenericEvent, wakeBuffer)
	if r.TaskEvents != nil {
		// A runnable of the manager runs only on the leader, like the reconciler.
		listen := manager.RunnableFunc(func(ctx context.Context) error {
			return r.TaskEvents.ListenTaskDone(ctrl.LoggerInto(ctx, mgr.GetLogger().WithName("tasks")), r.wakeRun)
		})
		if err := mgr.Add(listen); err != nil {
			return fmt.Errorf("add the listener of the task results: %w", err)
		}
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&chroniclev1alpha1.Workflow{}).
		Owns(&corev1.Pod{}).
		// A child run wakes its parent run when its status changes.
		Owns(&chroniclev1alpha1.Workflow{}).
		WatchesRawSource(source.Channel(r.wake, &handler.EnqueueRequestForObject{})).
		Named("workflow").
		WithOptions(controller.Options{MaxConcurrentReconciles: max(r.Workers, 1)}).
		Complete(r)
}
