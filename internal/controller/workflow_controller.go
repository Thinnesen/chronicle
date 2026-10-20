package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/artifact"
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
	// Cache keeps the results of templates with memoize, or is nil without the archive database.
	Cache StepCache
	// Recorder writes Kubernetes Events about runs, or is nil.
	Recorder events.EventRecorder
	// APIReader reads objects that the manager does not cache, such as the artifact repository ConfigMap.
	APIReader client.Reader
	// Workers is the number of runs that the controller reconciles at the same time. A run is never reconciled
	// twice at the same time.
	Workers int
	// MaxObjectBytes is the largest Workflow that the controller writes; 0 means 1 MiB. A larger run fails.
	MaxObjectBytes int

	created createdPods
	// wake queues runs that wait for a lock, when a holder gives the lock back.
	wake chan event.GenericEvent
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
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;create;delete
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;create;update
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
	if res, wait, err := r.gate(ctx, &wf, spec, observed); wait || err != nil {
		return res, err
	}
	pods, err := r.observePods(ctx, &wf)
	if err != nil {
		return ctrl.Result{}, err
	}
	tasks, err := r.observeTasks(ctx, &wf)
	if err != nil {
		return ctrl.Result{}, err
	}
	held, err := r.heldLocks(ctx, &wf, spec)
	if err != nil {
		return ctrl.Result{}, err
	}
	in := engine.Input{
		Name: wf.Name, Namespace: wf.Namespace, UID: string(wf.UID),
		CreatedAt: wf.CreationTimestamp, ScheduledTime: wf.Annotations[AnnotationScheduledTime], Labels: wf.Labels, Annotations: wf.Annotations,
		Spec: spec, Status: wf.Status, Pods: pods, Tasks: tasks, TasksEnabled: r.Tasks != nil, CacheEnabled: r.Cache != nil,
		Now: metav1.Now(), Restarts: wf.Status.Restarts,
		Suspend: wf.Spec.Suspend, Shutdown: wf.Spec.Shutdown, Approvals: wf.Spec.Approvals, HeldLocks: held,
	}
	res, err := r.runWithCache(ctx, &wf, in)
	if err != nil {
		return ctrl.Result{}, err
	}
	// The phase after restart: the first reconcile of a retry must also respect the limit.
	if size := r.tooLarge(&wf, res.Status); size > 0 && !finished(wf.Status.Phase) {
		return r.failTooLarge(ctx, &wf, size)
	}
	if err := r.claims(ctx, &wf, spec, &res.Status); errors.Is(err, resolve.ErrInvalid) {
		return r.setError(ctx, &wf, err.Error())
	} else if err != nil {
		return ctrl.Result{}, err
	}
	took, messages, err := r.applyAll(ctx, &wf, spec, res.Actions)
	if err != nil {
		return ctrl.Result{}, err
	}
	result := ctrl.Result{RequeueAfter: res.RequeueAfter}
	if took {
		// The engine starts the node in the next step, now that it holds the lock.
		result.RequeueAfter = lockStart
	}
	wf.Status = res.Status
	for id, msg := range messages {
		n := wf.Status.Nodes[id]
		n.Message = msg
		wf.Status.Nodes[id] = n
	}
	if err := r.unlock(ctx, &wf, spec); err != nil {
		return ctrl.Result{}, err
	}
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
	done, total := wf.Status.Progress()
	if err := r.Archive.Save(ctx, wf, int(done), int(total)); err != nil {
		return fmt.Errorf("archive the run: %w", err)
	}
	meta.SetStatusCondition(&wf.Status.Conditions, metav1.Condition{Type: conditionArchived, Status: metav1.ConditionTrue,
		Reason: "Saved", Message: "the archive has the run", ObservedGeneration: wf.Generation})
	return nil
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
	if err == nil && wf.Spec.Shutdown != "" {
		// The engine would stop the new try at once. The API removes the shutdown with the retry.
		err = errors.New("the run was stopped: remove spec.shutdown in the same change as the retry")
	}
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

// spec returns the spec to run. It resolves the template references once and keeps the result in the status.
func (r *WorkflowReconciler) spec(ctx context.Context, wf *chroniclev1alpha1.Workflow) (*chroniclev1alpha1.WorkflowTemplateSpec, error) {
	if wf.Status.StoredSpec != nil {
		return wf.Status.StoredSpec, nil
	}
	s := &wf.Spec.WorkflowTemplateSpec
	if resolve.Needed(&wf.Spec) {
		var err error
		if s, err = resolve.Workflow(ctx, templateGetter{r.Client, wf.Namespace}, &wf.Spec); err != nil {
			return nil, fmt.Errorf("resolve the template references: %w", err)
		}
	} else if !resolve.UsesConfigMaps(s) {
		return s, nil
	}
	if resolve.UsesConfigMaps(s) {
		s = s.DeepCopy()
		if err := resolve.ConfigMapValues(ctx, configMapGetter{r.reader(), wf.Namespace}, s); err != nil {
			return nil, fmt.Errorf("read the ConfigMap parameters: %w", err)
		}
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

// applyWorkers is the number of actions of one run that the controller applies at the same time. A large fan-out starts
// many Pods in one step. One API call after the other made the first status wait for all of them.
const applyWorkers = 8

// lockStart is the wait before the step that starts a node which has just taken its lock.
const lockStart = 100 * time.Millisecond

// applyAll applies the actions of one engine step. It returns true when a node took a lock, so that the run goes on
// soon. It also returns the message of each node that waits for a lock: the holders, or the problem of its limit. The lock actions run one after the other, because they change the same Leases. The other actions are
// independent and run in parallel. The artifact repository is read once.
func (r *WorkflowReconciler) applyAll(ctx context.Context, wf *chroniclev1alpha1.Workflow, spec *chroniclev1alpha1.WorkflowTemplateSpec,
	actions []engine.Action) (bool, map[string]string, error) {
	took := false
	messages := map[string]string{}
	rest := make([]engine.Action, 0, len(actions))
	for _, a := range actions {
		switch a.(type) {
		case engine.AcquireLock, engine.ReleaseLock:
			held, msg, err := r.applyLock(ctx, wf, a)
			acquire, isAcquire := a.(engine.AcquireLock)
			if isAcquire && errors.Is(err, resolve.ErrInvalid) {
				messages[acquire.NodeID] = lockProblem(acquire.Lock, err)
				continue
			}
			if err != nil {
				return false, nil, err
			}
			if isAcquire && msg != "" {
				messages[acquire.NodeID] = msg
			}
			took = took || held
		default:
			rest = append(rest, a)
		}
	}
	repo := sync.OnceValues(func() (*artifact.Repository, error) { return r.repository(ctx, wf.Namespace) })
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(applyWorkers)
	for _, a := range rest {
		g.Go(func() error {
			if err := r.apply(gctx, wf, spec, a, repo); err != nil {
				return err
			}
			countAction(a)
			return nil
		})
	}
	return took, messages, g.Wait()
}

func (r *WorkflowReconciler) apply(ctx context.Context, wf *chroniclev1alpha1.Workflow, spec *chroniclev1alpha1.WorkflowTemplateSpec, a engine.Action,
	repo func() (*artifact.Repository, error)) error {
	switch a := a.(type) {
	case engine.CreatePod:
		var files *executor.ArtifactSpec
		if usesExecutor(a.Template) {
			var err error
			if files, err = artifactSpec(ctx, wf, a, repo); err != nil {
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
	case engine.SaveCache:
		if r.Cache != nil {
			return r.Cache.SaveCache(ctx, wf.Namespace, a.Key, wf.Name, a.NodeName, a.Outputs)
		}
	case engine.DeletePod:
		pod := &corev1.Pod{Name: a.PodName, Namespace: wf.Namespace}
		if err := r.Delete(ctx, pod); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("delete the Pod %s: %w", a.PodName, err)
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager. It watches the Pods that the controller owns, and the
// runs that a lock wakes.
func (r *WorkflowReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.wake = make(chan event.GenericEvent, wakeBuffer)
	return ctrl.NewControllerManagedBy(mgr).
		For(&chroniclev1alpha1.Workflow{}).
		Owns(&corev1.Pod{}).
		WatchesRawSource(source.Channel(r.wake, &handler.EnqueueRequestForObject{})).
		Named("workflow").
		WithOptions(controller.Options{MaxConcurrentReconciles: max(r.Workers, 1)}).
		Complete(r)
}
