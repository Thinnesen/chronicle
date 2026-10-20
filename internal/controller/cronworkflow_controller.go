package controller

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/schedule"
)

// The label and annotation on Workflows that a CronWorkflow created.
const (
	LabelCronWorkflow       = "chronicle.thinnesen.com/cron-workflow"
	AnnotationScheduledTime = "chronicle.thinnesen.com/scheduled-time"
)

// CronWorkflowReconciler creates Workflows on the schedules of CronWorkflows.
type CronWorkflowReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// Now returns the current time. Tests replace it.
	Now func() time.Time
	// Archive is true when the controller archives runs. Then the history limits delete a run only after the archive
	// has it, as the TTL does.
	Archive bool
}

// +kubebuilder:rbac:groups=chronicle.thinnesen.com,resources=cronworkflows,verbs=get;list;watch
// +kubebuilder:rbac:groups=chronicle.thinnesen.com,resources=cronworkflows/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=chronicle.thinnesen.com,resources=workflows,verbs=create

// Reconcile starts the due run of a CronWorkflow, applies the concurrency policy and the history limits.
func (r *CronWorkflowReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cw chroniclev1alpha1.CronWorkflow
	if err := r.Get(ctx, req.NamespacedName, &cw); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	var list chroniclev1alpha1.WorkflowList
	if err := r.List(ctx, &list, client.InNamespace(cw.Namespace), client.MatchingLabels{LabelCronWorkflow: cw.Name}); err != nil {
		return ctrl.Result{}, fmt.Errorf("list the runs: %w", err)
	}
	runs := ownedBy(list.Items, &cw)
	now := r.Now()
	plan, err := schedule.Decide(&cw, runs, now)
	if err != nil {
		// A bad schedule does not heal itself, so the controller waits for a spec change.
		return ctrl.Result{}, r.setReady(ctx, &cw, metav1.ConditionFalse, "InvalidSchedule", err.Error())
	}
	plan.Delete = archivedOnly(runs, plan.Delete, r.Archive)
	if err := r.apply(ctx, &cw, plan); err != nil {
		return ctrl.Result{}, err
	}
	old := cw.Status.DeepCopy()
	cw.Status.NextScheduledTime = &metav1.Time{Time: plan.Next}
	if plan.Due != nil {
		cw.Status.LastScheduledTime = &metav1.Time{Time: *plan.Due}
	}
	cw.Status.Active = activeRefs(plan)
	msg := "the schedules are valid"
	if plan.Skipped != "" {
		msg = "the last run was skipped: " + plan.Skipped
	}
	meta.SetStatusCondition(&cw.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Scheduled", Message: msg, ObservedGeneration: cw.Generation})
	if !equality.Semantic.DeepEqual(old, &cw.Status) {
		if err := r.Status().Update(ctx, &cw); apierrors.IsConflict(err) {
			// The cache had an old CronWorkflow. Run again with the new one; the run names are the same.
			return ctrl.Result{RequeueAfter: time.Second}, nil
		} else if err != nil {
			return ctrl.Result{}, fmt.Errorf("update the CronWorkflow status: %w", err)
		}
	}
	return ctrl.Result{RequeueAfter: max(plan.Next.Sub(now), time.Second)}, nil
}

// archivedOnly returns the runs of names that can be deleted. Without an archive, these are all of them. With an
// archive, these are the runs that the archive has. A later reconcile deletes a run that is not archived yet, after its
// status changes.
func archivedOnly(runs []chroniclev1alpha1.Workflow, names []string, archive bool) []string {
	if !archive {
		return names
	}
	saved := map[string]bool{}
	for _, wf := range runs {
		saved[wf.Name] = meta.IsStatusConditionTrue(wf.Status.Conditions, conditionArchived)
	}
	var out []string
	for _, name := range names {
		if saved[name] {
			out = append(out, name)
		}
	}
	return out
}

// apply deletes old runs, terminates replaced runs and creates the due run.
func (r *CronWorkflowReconciler) apply(ctx context.Context, cw *chroniclev1alpha1.CronWorkflow, plan schedule.Plan) error {
	log := logf.FromContext(ctx)
	for _, name := range plan.Delete {
		wf := &chroniclev1alpha1.Workflow{Name: name, Namespace: cw.Namespace}
		if err := r.Delete(ctx, wf, client.PropagationPolicy(metav1.DeletePropagationBackground)); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("delete the old run %s: %w", name, err)
		}
	}
	for _, name := range plan.Terminate {
		patch := []byte(`{"spec":{"shutdown":"Terminate"}}`)
		wf := &chroniclev1alpha1.Workflow{Name: name, Namespace: cw.Namespace}
		if err := r.Patch(ctx, wf, client.RawPatch("application/merge-patch+json", patch)); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("terminate the run %s: %w", name, err)
		}
	}
	if plan.Run == nil {
		if plan.Skipped != "" {
			log.Info("Skipped a run", "reason", plan.Skipped)
		}
		return nil
	}
	wf := newRun(cw, *plan.Run)
	if err := r.Create(ctx, wf); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create the run %s: %w", wf.Name, err)
	}
	log.Info("Started a run", "workflow", wf.Name)
	return nil
}

// newRun returns the Workflow for a schedule time. Its name comes from the time, so a second create fails.
func newRun(cw *chroniclev1alpha1.CronWorkflow, at time.Time) *chroniclev1alpha1.Workflow {
	labels := maps.Clone(cw.Spec.WorkflowMetadata.Labels)
	if labels == nil {
		labels = map[string]string{}
	}
	labels[LabelCronWorkflow] = cw.Name
	annotations := maps.Clone(cw.Spec.WorkflowMetadata.Annotations)
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[AnnotationScheduledTime] = at.UTC().Format(time.RFC3339)
	return &chroniclev1alpha1.Workflow{
		Name:            cw.Name + "-" + strconv.FormatInt(at.Unix(), 10),
		Namespace:       cw.Namespace,
		Labels:          labels,
		Annotations:     annotations,
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(cw, chroniclev1alpha1.GroupVersion.WithKind("CronWorkflow"))},
		Spec:            *cw.Spec.WorkflowSpec.DeepCopy(),
	}
}

func ownedBy(list []chroniclev1alpha1.Workflow, cw *chroniclev1alpha1.CronWorkflow) []chroniclev1alpha1.Workflow {
	var out []chroniclev1alpha1.Workflow
	for _, wf := range list {
		if ref := metav1.GetControllerOf(&wf); ref != nil && ref.UID == cw.UID {
			out = append(out, wf)
		}
	}
	return out
}

func activeRefs(plan schedule.Plan) []corev1.ObjectReference {
	refs := make([]corev1.ObjectReference, 0, len(plan.Active))
	for _, wf := range plan.Active {
		refs = append(refs, corev1.ObjectReference{Kind: "Workflow", Namespace: wf.Namespace, Name: wf.Name, UID: wf.UID})
	}
	return refs
}

func (r *CronWorkflowReconciler) setReady(ctx context.Context, cw *chroniclev1alpha1.CronWorkflow, status metav1.ConditionStatus, reason, msg string) error {
	if !meta.SetStatusCondition(&cw.Status.Conditions, metav1.Condition{Type: "Ready", Status: status, Reason: reason, Message: msg, ObservedGeneration: cw.Generation}) {
		return nil
	}
	if err := r.Status().Update(ctx, cw); err != nil {
		return fmt.Errorf("update the CronWorkflow status: %w", err)
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager. It watches the Workflows that the controller owns.
func (r *CronWorkflowReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Now == nil {
		r.Now = time.Now
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&chroniclev1alpha1.CronWorkflow{}).
		Owns(&chroniclev1alpha1.Workflow{}).
		Named("cronworkflow").
		Complete(r)
}
