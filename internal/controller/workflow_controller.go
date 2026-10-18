package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/engine"
)

// WorkflowReconciler runs Workflows: it runs the engine and applies its actions.
type WorkflowReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// ExecutorImage is the image with the chronicle-exec binary.
	ExecutorImage string
}

// +kubebuilder:rbac:groups=chronicle.thinnesen.com,resources=workflows,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=chronicle.thinnesen.com,resources=workflows/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=chronicle.thinnesen.com,resources=workflows/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;delete

// Reconcile runs one step of the engine for a Workflow.
func (r *WorkflowReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	var wf chroniclev1alpha1.Workflow
	if err := r.Get(ctx, req.NamespacedName, &wf); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if wf.Spec.WorkflowTemplateRef != nil {
		return r.setError(ctx, &wf, "workflowTemplateRef is not supported yet")
	}
	pods, err := r.observePods(ctx, &wf)
	if err != nil {
		return ctrl.Result{}, err
	}
	spec := &wf.Spec.WorkflowTemplateSpec
	res := engine.Run(engine.Input{
		Name: wf.Name, Namespace: wf.Namespace, UID: string(wf.UID),
		Spec: spec, Status: wf.Status, Pods: pods, Now: metav1.Now(),
		Suspend: wf.Spec.Suspend, Shutdown: wf.Spec.Shutdown, Approvals: wf.Spec.Approvals,
	})
	for _, a := range res.Actions {
		if err := r.apply(ctx, &wf, spec, a); err != nil {
			return ctrl.Result{}, err
		}
	}
	result := ctrl.Result{RequeueAfter: res.RequeueAfter}
	if equality.Semantic.DeepEqual(wf.Status, res.Status) {
		return r.applyTTL(ctx, &wf, result)
	}
	if res.Status.Phase != wf.Status.Phase {
		log.Info("Workflow phase changed", "phase", res.Status.Phase)
	}
	wf.Status = res.Status
	if err := r.Status().Update(ctx, &wf); err != nil {
		if apierrors.IsConflict(err) {
			// The cache had an old Workflow. Run again with the new one.
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
		return ctrl.Result{}, fmt.Errorf("update the workflow status: %w", err)
	}
	return r.applyTTL(ctx, &wf, result)
}

// applyTTL deletes a finished workflow when its TTL is over, or runs again when it will be over.
func (r *WorkflowReconciler) applyTTL(ctx context.Context, wf *chroniclev1alpha1.Workflow, result ctrl.Result) (ctrl.Result, error) {
	when, ok := engine.TTL(&wf.Spec.WorkflowTemplateSpec, wf.Status)
	if !ok {
		return result, nil
	}
	if wait := time.Until(when); wait > 0 {
		if result.RequeueAfter == 0 || wait < result.RequeueAfter {
			result.RequeueAfter = wait
		}
		return result, nil
	}
	logf.FromContext(ctx).Info("Deleting the workflow after its TTL")
	if err := r.Delete(ctx, wf, client.PropagationPolicy(metav1.DeletePropagationBackground)); client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, fmt.Errorf("delete the workflow after its TTL: %w", err)
	}
	return ctrl.Result{}, nil
}

// observePods returns the state of the Pods of a workflow by node ID.
func (r *WorkflowReconciler) observePods(ctx context.Context, wf *chroniclev1alpha1.Workflow) (map[string]engine.PodResult, error) {
	var list corev1.PodList
	if err := r.List(ctx, &list, client.InNamespace(wf.Namespace), client.MatchingLabels{LabelWorkflowUID: string(wf.UID)}); err != nil {
		return nil, fmt.Errorf("list the workflow Pods: %w", err)
	}
	pods := make(map[string]engine.PodResult, len(list.Items))
	for i := range list.Items {
		pods[list.Items[i].Labels[LabelNodeID]] = podResult(&list.Items[i])
	}
	return pods, nil
}

func (r *WorkflowReconciler) apply(ctx context.Context, wf *chroniclev1alpha1.Workflow, spec *chroniclev1alpha1.WorkflowTemplateSpec, a engine.Action) error {
	switch a := a.(type) {
	case engine.CreatePod:
		pod := buildPod(wf, spec, a, r.ExecutorImage)
		if err := r.Create(ctx, pod); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create the Pod of node %s: %w", a.NodeName, err)
		}
	case engine.DeletePod:
		pod := &corev1.Pod{Name: a.PodName, Namespace: wf.Namespace}
		if err := r.Delete(ctx, pod); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("delete the Pod %s: %w", a.PodName, err)
		}
	}
	return nil
}

func (r *WorkflowReconciler) setError(ctx context.Context, wf *chroniclev1alpha1.Workflow, msg string) (ctrl.Result, error) {
	if wf.Status.Phase == chroniclev1alpha1.WorkflowError {
		return ctrl.Result{}, nil
	}
	now := metav1.Now()
	wf.Status.Phase, wf.Status.Message, wf.Status.FinishedAt = chroniclev1alpha1.WorkflowError, msg, &now
	if err := r.Status().Update(ctx, wf); err != nil {
		return ctrl.Result{}, fmt.Errorf("update the workflow status: %w", err)
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager. It watches the Pods that the controller owns.
func (r *WorkflowReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&chroniclev1alpha1.Workflow{}).
		Owns(&corev1.Pod{}).
		Named("workflow").
		Complete(r)
}
