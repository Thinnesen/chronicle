package controller

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/engine"
)

// The labels and annotations of a child run.
const (
	LabelParentUID       = chroniclev1alpha1.LabelParentUID
	AnnotationParent     = chroniclev1alpha1.AnnotationParent
	AnnotationParentNode = chroniclev1alpha1.AnnotationParentNode
	AnnotationDepth      = chroniclev1alpha1.AnnotationDepth
)

// observe reads the Pods, the tasks, the child runs and the held locks of a run, for the engine.
func (r *WorkflowReconciler) observe(ctx context.Context, wf *chroniclev1alpha1.Workflow, spec *chroniclev1alpha1.WorkflowTemplateSpec) (engine.Input, error) {
	var in engine.Input
	var err error
	if in.Pods, err = r.observePods(ctx, wf); err != nil {
		return in, err
	}
	if in.Tasks, err = r.observeTasks(ctx, wf); err != nil {
		return in, err
	}
	if in.Children, err = r.observeChildren(ctx, wf); err != nil {
		return in, err
	}
	in.HeldLocks, err = r.heldLocks(ctx, wf, spec)
	return in, err
}

// observeChildren returns the child runs of a run that has open child nodes. Only runs that the parent controls count.
func (r *WorkflowReconciler) observeChildren(ctx context.Context, wf *chroniclev1alpha1.Workflow) (map[string]engine.ChildResult, error) {
	if !slices.ContainsFunc(slices.Collect(maps.Values(wf.Status.Nodes)), openChild) {
		return nil, nil
	}
	var list chroniclev1alpha1.WorkflowList
	if err := r.List(ctx, &list, client.InNamespace(wf.Namespace), client.MatchingLabels{LabelParentUID: string(wf.UID)}); err != nil {
		return nil, fmt.Errorf("list the child runs: %w", err)
	}
	out := make(map[string]engine.ChildResult, len(list.Items))
	for i := range list.Items {
		c := &list.Items[i]
		if metav1.IsControlledBy(c, wf) {
			out[c.Name] = childResult(c)
		}
	}
	return out, nil
}

func childResult(c *chroniclev1alpha1.Workflow) engine.ChildResult {
	res := engine.ChildResult{Phase: c.Status.Phase, Message: c.Status.Message, Outputs: c.Status.Outputs,
		FinishedAt: c.Status.FinishedAt, Restarts: c.Status.Restarts}
	if c.Spec.Restart != nil {
		res.RestartGeneration = c.Spec.Restart.Generation
	}
	if cond := meta.FindStatusCondition(c.Status.Conditions, "Restarted"); cond != nil && cond.Status == metav1.ConditionFalse {
		res.RestartRefused = cond.Message
	}
	return res
}

func openChild(n chroniclev1alpha1.NodeStatus) bool {
	return n.Type == chroniclev1alpha1.NodeChildWorkflow && !engine.IsTerminal(n.Phase)
}

// depth returns the number of parent runs above a run.
func depth(wf *chroniclev1alpha1.Workflow) int {
	d, _ := strconv.Atoi(wf.Annotations[AnnotationDepth])
	return d
}

// createChild creates the child run of a node. A child run with the name that the run does not control is an error.
func (r *WorkflowReconciler) createChild(ctx context.Context, wf *chroniclev1alpha1.Workflow, a engine.CreateWorkflow) error {
	child := &chroniclev1alpha1.Workflow{
		Name:      a.Name,
		Namespace: wf.Namespace,
		Labels:    map[string]string{LabelParentUID: string(wf.UID)},
		Annotations: map[string]string{
			AnnotationParent: wf.Name, AnnotationParentNode: a.NodeName, AnnotationDepth: strconv.Itoa(depth(wf) + 1),
		},
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(wf, chroniclev1alpha1.GroupVersion.WithKind("Workflow"))},
	}
	ref := a.TemplateRef
	child.Spec.WorkflowTemplateRef = &ref
	child.Spec.Arguments.Parameters = a.Parameters
	err := r.Create(ctx, child)
	if apierrors.IsAlreadyExists(err) {
		var old chroniclev1alpha1.Workflow
		if err := r.Get(ctx, client.ObjectKeyFromObject(child), &old); err != nil {
			return fmt.Errorf("read the child run %s: %w", a.Name, err)
		}
		if !metav1.IsControlledBy(&old, wf) {
			return fmt.Errorf("the workflow %s exists and is not a child run of this run", a.Name)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("create the child run of node %s: %w", a.NodeName, err)
	}
	return nil
}

// shutdownChild stops or terminates a child run that has not finished.
func (r *WorkflowReconciler) shutdownChild(ctx context.Context, wf *chroniclev1alpha1.Workflow, a engine.ShutdownWorkflow) error {
	var child chroniclev1alpha1.Workflow
	err := r.Get(ctx, client.ObjectKey{Namespace: wf.Namespace, Name: a.Name}, &child)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read the child run %s: %w", a.Name, err)
	}
	if !metav1.IsControlledBy(&child, wf) || finished(child.Status.Phase) || child.Spec.Shutdown == a.Mode {
		return nil
	}
	patch := fmt.Appendf(nil, `{"spec":{"shutdown":%q}}`, a.Mode)
	if err := r.Patch(ctx, &child, client.RawPatch("application/merge-patch+json", patch)); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("stop the child run %s: %w", a.Name, err)
	}
	return nil
}

// retryChild asks a finished child run to retry its failed nodes. A stop of the parent can have stopped the child, so
// the retry also removes the shutdown, as the API does.
func (r *WorkflowReconciler) retryChild(ctx context.Context, wf *chroniclev1alpha1.Workflow, a engine.RetryWorkflow) error {
	var child chroniclev1alpha1.Workflow
	err := r.Get(ctx, client.ObjectKey{Namespace: wf.Namespace, Name: a.Name}, &child)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read the child run %s: %w", a.Name, err)
	}
	if !metav1.IsControlledBy(&child, wf) || !finished(child.Status.Phase) || child.Spec.Restart != nil && child.Spec.Restart.Generation >= a.Generation {
		return nil
	}
	patch := fmt.Appendf(nil, `{"spec":{"restart":{"generation":%d},"shutdown":null}}`, a.Generation)
	if err := r.Patch(ctx, &child, client.RawPatch("application/merge-patch+json", patch)); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("retry the child run %s: %w", a.Name, err)
	}
	return nil
}
