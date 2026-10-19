package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/engine"
	"github.com/Thinnesen/chronicle/internal/executor"
	"github.com/Thinnesen/chronicle/internal/resolve"
)

// WorkflowReconciler runs Workflows: it runs the engine and applies its actions.
type WorkflowReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// ExecutorImage is the image with the chronicle-exec binary.
	ExecutorImage string
	// Archive keeps finished runs, or is nil without an archive.
	Archive Archiver
	// Tasks is the task queue of the task templates, or nil without the archive database.
	Tasks TaskQueue
	// Recorder writes Kubernetes Events about runs, or is nil.
	Recorder events.EventRecorder
	// APIReader reads objects that the manager does not cache, such as the artifact repository ConfigMap.
	APIReader client.Reader

	created createdPods
}

// Archiver saves a finished run with its node counts.
type Archiver interface {
	Save(ctx context.Context, wf *chroniclev1alpha1.Workflow, nodesDone, nodesTotal int) error
}

// conditionArchived tells that the archive has the finished run.
const conditionArchived = "Archived"

// +kubebuilder:rbac:groups=chronicle.thinnesen.com,resources=workflows,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=chronicle.thinnesen.com,resources=workflows/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=chronicle.thinnesen.com,resources=workflows/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=chronicle.thinnesen.com,resources=workflowtemplates;clusterworkflowtemplates,verbs=get;list;watch

// Reconcile runs one step of the engine for a Workflow.
func (r *WorkflowReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	var wf chroniclev1alpha1.Workflow
	if err := r.Get(ctx, req.NamespacedName, &wf); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	spec, err := r.spec(ctx, &wf)
	if apierrors.IsNotFound(err) || errors.Is(err, resolve.ErrInvalid) {
		return r.setError(ctx, &wf, err.Error())
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	observed := wf.Status.DeepCopy()
	if err := r.restart(ctx, &wf, spec); err != nil {
		return ctrl.Result{}, err
	}
	pods, err := r.observePods(ctx, &wf)
	if err != nil {
		return ctrl.Result{}, err
	}
	tasks, err := r.observeTasks(ctx, &wf)
	if err != nil {
		return ctrl.Result{}, err
	}
	res := engine.Run(engine.Input{
		Name: wf.Name, Namespace: wf.Namespace, UID: string(wf.UID),
		CreatedAt: wf.CreationTimestamp, ScheduledTime: wf.Annotations[AnnotationScheduledTime],
		Spec: spec, Status: wf.Status, Pods: pods, Tasks: tasks, TasksEnabled: r.Tasks != nil,
		Now: metav1.Now(), Restarts: wf.Status.Restarts,
		Suspend: wf.Spec.Suspend, Shutdown: wf.Spec.Shutdown, Approvals: wf.Spec.Approvals,
	})
	for _, a := range res.Actions {
		if err := r.apply(ctx, &wf, spec, a); err != nil {
			return ctrl.Result{}, err
		}
		countAction(a)
	}
	result := ctrl.Result{RequeueAfter: res.RequeueAfter}
	wf.Status = res.Status
	archiveErr := r.archive(ctx, &wf)
	if equality.Semantic.DeepEqual(*observed, wf.Status) {
		if archiveErr != nil {
			return ctrl.Result{}, archiveErr
		}
		return r.applyTTL(ctx, &wf, result)
	}
	if wf.Status.Phase != observed.Phase {
		log.Info("Workflow phase changed", "phase", wf.Status.Phase)
	}
	if err := r.Status().Update(ctx, &wf); err != nil {
		if apierrors.IsConflict(err) {
			// The cache had an old Workflow. Run again with the new one.
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
		return ctrl.Result{}, fmt.Errorf("update the workflow status: %w", err)
	}
	if finished(wf.Status.Phase) && wf.Status.Phase != observed.Phase {
		r.finishedRun(&wf)
	}
	if archiveErr != nil {
		return ctrl.Result{}, archiveErr
	}
	return r.applyTTL(ctx, &wf, result)
}

// archive saves a finished run once and marks it with the condition Archived.
func (r *WorkflowReconciler) archive(ctx context.Context, wf *chroniclev1alpha1.Workflow) error {
	if r.Archive == nil || !finished(wf.Status.Phase) || meta.IsStatusConditionTrue(wf.Status.Conditions, conditionArchived) {
		return nil
	}
	done, total := nodeCounts(wf)
	if err := r.Archive.Save(ctx, wf, done, total); err != nil {
		return fmt.Errorf("archive the run: %w", err)
	}
	meta.SetStatusCondition(&wf.Status.Conditions, metav1.Condition{Type: conditionArchived, Status: metav1.ConditionTrue,
		Reason: "Saved", Message: "the archive has the run", ObservedGeneration: wf.Generation})
	return nil
}

// nodeCounts counts the Pod and suspend nodes, and those that finished.
func nodeCounts(wf *chroniclev1alpha1.Workflow) (done, total int) {
	for _, n := range wf.Status.Nodes {
		if n.Type == chroniclev1alpha1.NodePod || n.Type == chroniclev1alpha1.NodeSuspend {
			total++
			if n.FinishedAt != nil {
				done++
			}
		}
	}
	return done, total
}

// applyTTL deletes a finished workflow when its TTL is over, or runs again when it will be over.
func (r *WorkflowReconciler) applyTTL(ctx context.Context, wf *chroniclev1alpha1.Workflow, result ctrl.Result) (ctrl.Result, error) {
	spec := &wf.Spec.WorkflowTemplateSpec
	if wf.Status.StoredSpec != nil {
		spec = wf.Status.StoredSpec
	}
	when, ok := engine.TTL(spec, wf.Status)
	// With an archive, the TTL deletes a run only after the archive has it.
	if !ok || r.Archive != nil && !meta.IsStatusConditionTrue(wf.Status.Conditions, conditionArchived) {
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

// restart applies a new retry request of a finished run: it resets the nodes and deletes their Pods.
// A refused retry becomes the condition Restarted=False, so that it does not repeat.
func (r *WorkflowReconciler) restart(ctx context.Context, wf *chroniclev1alpha1.Workflow, spec *chroniclev1alpha1.WorkflowTemplateSpec) error {
	rs := wf.Spec.Restart
	if rs == nil || rs.Generation <= wf.Status.Restarts || !finished(wf.Status.Phase) {
		return nil
	}
	st, pods, err := engine.Restart(spec, wf.Status, wf.Name, rs.FromNode)
	if err != nil {
		wf.Status.Restarts = rs.Generation
		meta.SetStatusCondition(&wf.Status.Conditions, metav1.Condition{Type: "Restarted", Status: metav1.ConditionFalse,
			Reason: "Refused", Message: err.Error(), ObservedGeneration: wf.Generation})
		return nil
	}
	for _, name := range pods {
		pod := &corev1.Pod{Name: name, Namespace: wf.Namespace}
		if err := r.Delete(ctx, pod); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("delete the Pod %s for the retry: %w", name, err)
		}
	}
	st.Restarts = rs.Generation
	meta.RemoveStatusCondition(&st.Conditions, conditionArchived)
	meta.SetStatusCondition(&st.Conditions, metav1.Condition{Type: "Restarted", Status: metav1.ConditionTrue,
		Reason: "Retried", Message: fmt.Sprintf("retry %d", rs.Generation), ObservedGeneration: wf.Generation})
	logf.FromContext(ctx).Info("Retrying the workflow", "generation", rs.Generation, "fromNode", rs.FromNode)
	workflowRetries.Inc()
	wf.Status = st
	return nil
}

func finished(p chroniclev1alpha1.WorkflowPhase) bool {
	return p == chroniclev1alpha1.WorkflowSucceeded || p == chroniclev1alpha1.WorkflowFailed || p == chroniclev1alpha1.WorkflowError
}

// spec returns the spec to run. It resolves the template references once and keeps the result in the status.
func (r *WorkflowReconciler) spec(ctx context.Context, wf *chroniclev1alpha1.Workflow) (*chroniclev1alpha1.WorkflowTemplateSpec, error) {
	if wf.Status.StoredSpec != nil {
		return wf.Status.StoredSpec, nil
	}
	if !resolve.Needed(&wf.Spec) {
		return &wf.Spec.WorkflowTemplateSpec, nil
	}
	s, err := resolve.Workflow(ctx, templateGetter{r.Client, wf.Namespace}, &wf.Spec)
	if err != nil {
		return nil, fmt.Errorf("resolve the template references: %w", err)
	}
	wf.Status.StoredSpec = s
	return s, nil
}

// templateGetter reads WorkflowTemplates in one namespace and ClusterWorkflowTemplates.
type templateGetter struct {
	client.Reader
	namespace string
}

func (g templateGetter) Get(ctx context.Context, name string, clusterScope bool) (*chroniclev1alpha1.WorkflowTemplateSpec, error) {
	if clusterScope {
		var t chroniclev1alpha1.ClusterWorkflowTemplate
		if err := g.Reader.Get(ctx, client.ObjectKey{Name: name}, &t); err != nil {
			return nil, fmt.Errorf("read ClusterWorkflowTemplate %q: %w", name, err)
		}
		return &t.Spec, nil
	}
	var t chroniclev1alpha1.WorkflowTemplate
	if err := g.Reader.Get(ctx, client.ObjectKey{Namespace: g.namespace, Name: name}, &t); err != nil {
		return nil, fmt.Errorf("read WorkflowTemplate %q: %w", name, err)
	}
	return &t.Spec, nil
}

// observePods returns the state of the Pods of a workflow by Pod name.
func (r *WorkflowReconciler) observePods(ctx context.Context, wf *chroniclev1alpha1.Workflow) (map[string]engine.PodResult, error) {
	var list corev1.PodList
	if err := r.List(ctx, &list, client.InNamespace(wf.Namespace), client.MatchingLabels{LabelWorkflowUID: string(wf.UID)}); err != nil {
		return nil, fmt.Errorf("list the workflow Pods: %w", err)
	}
	pods := make(map[string]engine.PodResult, len(list.Items))
	for i := range list.Items {
		pods[list.Items[i].Name] = podResult(&list.Items[i])
	}
	r.created.fill(wf.UID, pods, time.Now())
	return pods, nil
}

func (r *WorkflowReconciler) apply(ctx context.Context, wf *chroniclev1alpha1.Workflow, spec *chroniclev1alpha1.WorkflowTemplateSpec, a engine.Action) error {
	switch a := a.(type) {
	case engine.CreatePod:
		var files *executor.ArtifactSpec
		if usesExecutor(a.Template) {
			var err error
			if files, err = r.artifactSpec(ctx, wf, a); err != nil {
				return err
			}
		}
		pod := buildPod(wf, spec, a, r.ExecutorImage, files)
		if err := r.Create(ctx, pod); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create the Pod of node %s: %w", a.NodeName, err)
		}
		r.created.add(wf.UID, pod.Name, time.Now())
	case engine.EnqueueTask, engine.CancelTask:
		return r.applyTask(ctx, wf, a)
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
