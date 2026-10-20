package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/engine"
	"github.com/Thinnesen/chronicle/internal/resolve"
)

// conditionLocked tells that a run holds the lock of its synchronization. conditionSkipped tells that a run did not run,
// because its lock was busy and whenBusy is Skip.
const (
	conditionLocked  = "Synchronized"
	conditionSkipped = "Skipped"
)

// annotationHolders lists the runs that hold a lock, on the Lease of the lock.
const annotationHolders = "chronicle.thinnesen.com/holders"

// lockPoll is the wait of a run for a lock between two tries. A holder that gives the lock back wakes the run sooner.
const lockPoll = 15 * time.Second

// lockPrefix starts the name of the Lease of each lock.
const lockPrefix = "chronicle-lock-"

// holder is a run that holds a lock. For the lock of a template, it is one node of a run.
type holder struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
	Node string `json:"node,omitempty"`
}

// lockOf returns the lock of a run and how many runs can hold it.
func lockOf(spec *chroniclev1alpha1.WorkflowTemplateSpec) (string, int, bool) {
	return spec.Synchronization.Lock()
}

// reader reads past the cache. It reads the Lease and its holders, so two reconciles never see an old holder list. It
// also reads ConfigMaps, which the manager does not cache.
func (r *WorkflowReconciler) reader() client.Reader {
	if r.APIReader != nil {
		return r.APIReader
	}
	return r.Client
}

// acquire takes the lock for a run if a slot is free. It returns false and a message when the run must wait.
func (r *WorkflowReconciler) acquire(ctx context.Context, wf *chroniclev1alpha1.Workflow, name string, limit int) (held, full bool, msg string, err error) {
	return r.take(ctx, wf.Namespace, name, limit, holder{UID: string(wf.UID), Name: wf.Name})
}

// acquireAs takes the lock for a run or a node if a slot is free. It returns false and a message when it must wait.
func (r *WorkflowReconciler) acquireAs(ctx context.Context, namespace, name string, limit int, me holder) (bool, string, error) {
	held, _, msg, err := r.take(ctx, namespace, name, limit, me)
	return held, msg, err
}

// take takes the lock if a slot is free. full is true when live holders use all slots; a lost race is not full. The
// Lease update has the resource version of the read. So a concurrent change makes it fail, and nothing is lost.
func (r *WorkflowReconciler) take(ctx context.Context, namespace, name string, limit int, me holder) (held, full bool, msg string, err error) {
	var lease coordinationv1.Lease
	err = r.reader().Get(ctx, client.ObjectKey{Namespace: namespace, Name: lockPrefix + name}, &lease)
	if apierrors.IsNotFound(err) {
		lease = coordinationv1.Lease{Name: lockPrefix + name, Namespace: namespace}
		setHolders(&lease, []holder{me})
		if err := r.Create(ctx, &lease); apierrors.IsAlreadyExists(err) {
			return false, false, engine.WaitingForLock + " " + name, nil
		} else if err != nil {
			return false, false, "", fmt.Errorf("create the lock %s: %w", name, err)
		}
		return true, false, "", nil
	}
	if err != nil {
		return false, false, "", fmt.Errorf("read the lock %s: %w", name, err)
	}
	all := holdersOf(&lease)
	if slices.Contains(all, me) {
		return true, false, "", nil
	}
	alive, err := r.liveHolders(ctx, namespace, all)
	if err != nil {
		return false, false, "", err
	}
	if len(alive) >= limit {
		return false, true, fmt.Sprintf("%s %s, which %s holds", engine.WaitingForLock, name, holderNames(alive)), nil
	}
	setHolders(&lease, append(alive, me))
	// A conflict, or a Lease that its last holder deleted in the meantime, is a lost race: ask again.
	if err := r.Update(ctx, &lease); apierrors.IsConflict(err) || apierrors.IsNotFound(err) {
		return false, false, engine.WaitingForLock + " " + name, nil
	} else if err != nil {
		return false, false, "", fmt.Errorf("take the lock %s: %w", name, err)
	}
	return true, false, "", nil
}

// release gives the lock of a run back, and the locks that its nodes still hold under the same name.
func (r *WorkflowReconciler) release(ctx context.Context, wf *chroniclev1alpha1.Workflow, name string) error {
	return r.releaseWhere(ctx, wf.Namespace, name, func(h holder) bool { return h.UID == string(wf.UID) })
}

// releaseWhere removes the holders that match from a lock.
func (r *WorkflowReconciler) releaseWhere(ctx context.Context, namespace, name string, match func(holder) bool) error {
	var lease coordinationv1.Lease
	err := r.reader().Get(ctx, client.ObjectKey{Namespace: namespace, Name: lockPrefix + name}, &lease)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read the lock %s: %w", name, err)
	}
	rest := slices.DeleteFunc(holdersOf(&lease), match)
	if len(rest) == 0 {
		// The precondition keeps a Lease that a run took in the meantime. take creates a deleted Lease again.
		rv := lease.ResourceVersion
		err := r.Delete(ctx, &lease, client.Preconditions{ResourceVersion: &rv})
		if client.IgnoreNotFound(err) != nil && !apierrors.IsConflict(err) {
			return fmt.Errorf("delete the lock %s: %w", name, err)
		}
		return r.wakeWaiters(ctx, namespace, name)
	}
	setHolders(&lease, rest)
	if err := r.Update(ctx, &lease); err != nil {
		return fmt.Errorf("give back the lock %s: %w", name, err)
	}
	return r.wakeWaiters(ctx, namespace, name)
}

// wakeBuffer is the number of runs that can wait in the wake queue for a lock.
const wakeBuffer = 256

// wakeWaiters queues the runs that wait for a lock, so that they ask for it again at once. The poll of the waiting
// runs stays as a fallback, for example for a holder that was deleted.
func (r *WorkflowReconciler) wakeWaiters(ctx context.Context, namespace, name string) error {
	if r.wake == nil {
		return nil
	}
	// The runs come from the cache without a copy, so this code must not change them.
	var list chroniclev1alpha1.WorkflowList
	if err := r.List(ctx, &list, client.InNamespace(namespace), client.UnsafeDisableDeepCopy); err != nil {
		return fmt.Errorf("list the runs that wait for the lock %s: %w", name, err)
	}
	for i := range list.Items {
		if !waitsFor(&list.Items[i], name) {
			continue
		}
		select {
		case r.wake <- event.GenericEvent{Object: &list.Items[i]}:
		default: // The queue is full. The poll wakes the run later.
		}
	}
	return nil
}

// waitsFor tells if an unfinished run, or one of its nodes, waits for a lock.
func waitsFor(wf *chroniclev1alpha1.Workflow, name string) bool {
	if finished(wf.Status.Phase) {
		return false
	}
	names := func(msg string) bool {
		rest, ok := strings.CutPrefix(msg, engine.WaitingForLock+" "+name)
		return ok && (rest == "" || rest[0] == ',' || rest[0] == ':')
	}
	if c := meta.FindStatusCondition(wf.Status.Conditions, conditionLocked); c != nil && c.Status == metav1.ConditionFalse && names(c.Message) {
		return true
	}
	for _, n := range wf.Status.Nodes {
		if n.Phase == chroniclev1alpha1.NodePending && names(n.Message) {
			return true
		}
	}
	return false
}

// liveHolders drops the holders whose run was deleted or has finished. An example is a run that was deleted while it
// ran. It also drops the node holders whose node has ended.
func (r *WorkflowReconciler) liveHolders(ctx context.Context, namespace string, all []holder) ([]holder, error) {
	var out []holder
	runs := map[string]*chroniclev1alpha1.Workflow{}
	for _, h := range all {
		wf, ok := runs[h.Name]
		if !ok {
			wf = &chroniclev1alpha1.Workflow{}
			err := r.reader().Get(ctx, client.ObjectKey{Namespace: namespace, Name: h.Name}, wf)
			if apierrors.IsNotFound(err) {
				wf = nil
			} else if err != nil {
				return nil, fmt.Errorf("read the holder %s: %w", h.Name, err)
			}
			runs[h.Name] = wf
		}
		if wf == nil || string(wf.UID) != h.UID || finished(wf.Status.Phase) {
			continue
		}
		if n, ok := wf.Status.Nodes[h.Node]; h.Node != "" && ok && engine.IsTerminal(n.Phase) {
			continue
		}
		out = append(out, h)
	}
	return out, nil
}

func holdersOf(lease *coordinationv1.Lease) []holder {
	var out []holder
	_ = json.Unmarshal([]byte(lease.Annotations[annotationHolders]), &out)
	return out
}

func setHolders(lease *coordinationv1.Lease, hs []holder) {
	data, _ := json.Marshal(hs)
	if lease.Annotations == nil {
		lease.Annotations = map[string]string{}
	}
	lease.Annotations[annotationHolders] = string(data)
}

// setLocked records in the status whether the run holds its lock.
func setLocked(wf *chroniclev1alpha1.Workflow, held bool, reason, msg string) {
	status := metav1.ConditionFalse
	if held {
		status = metav1.ConditionTrue
	}
	meta.SetStatusCondition(&wf.Status.Conditions, metav1.Condition{Type: conditionLocked, Status: status, Reason: reason, Message: msg, ObservedGeneration: wf.Generation})
}

// gate takes the lock of an unfinished run before the engine runs. It returns true when the run must wait; then it
// has written the status Pending with the reason.
func (r *WorkflowReconciler) gate(ctx context.Context, wf *chroniclev1alpha1.Workflow, spec *chroniclev1alpha1.WorkflowTemplateSpec,
	observed *chroniclev1alpha1.WorkflowStatus) (ctrl.Result, bool, error) {
	name, limit, ok := lockOf(spec)
	if !ok || finished(wf.Status.Phase) || meta.IsStatusConditionTrue(wf.Status.Conditions, conditionLocked) {
		return ctrl.Result{}, false, nil
	}
	if wf.Spec.Shutdown != "" {
		// A run that waits has no nodes yet: a stop or a terminate ends it at once.
		now := metav1.Now()
		wf.Status.Phase, wf.Status.FinishedAt = chroniclev1alpha1.WorkflowFailed, &now
		wf.Status.Message = "the workflow was stopped while it waited for the lock " + name
		setLocked(wf, false, "Stopped", wf.Status.Message)
		return r.writeWaiting(ctx, wf, observed, ctrl.Result{})
	}
	limit, err := r.lockLimit(ctx, wf.Namespace, limit, spec.Synchronization.LimitFrom())
	held, full, msg := false, false, ""
	switch {
	case errors.Is(err, resolve.ErrInvalid):
		msg = lockProblem(name, err)
	case err != nil:
		return ctrl.Result{}, true, err
	default:
		if held, full, msg, err = r.acquire(ctx, wf, name, limit); err != nil {
			return ctrl.Result{}, true, err
		}
	}
	if full && spec.Synchronization.WhenBusy == chroniclev1alpha1.WhenBusySkip {
		now := metav1.Now()
		wf.Status.Phase, wf.Status.FinishedAt = chroniclev1alpha1.WorkflowSucceeded, &now
		wf.Status.Message = "skipped: the lock " + name + " is busy"
		setLocked(wf, false, "Skipped", wf.Status.Message)
		meta.SetStatusCondition(&wf.Status.Conditions, metav1.Condition{Type: conditionSkipped, Status: metav1.ConditionTrue,
			Reason: "LockBusy", Message: wf.Status.Message, ObservedGeneration: wf.Generation})
		res, wait, err := r.writeWaiting(ctx, wf, observed, ctrl.Result{RequeueAfter: time.Second})
		if err == nil {
			r.finishedRun(wf)
		}
		return res, wait, err
	}
	if held {
		if strings.HasPrefix(wf.Status.Message, engine.WaitingForLock) {
			wf.Status.Message = ""
		}
		setLocked(wf, true, "Acquired", "the run holds the lock "+name)
		return ctrl.Result{}, false, nil
	}
	wf.Status.Phase, wf.Status.Message = chroniclev1alpha1.WorkflowPending, msg
	setLocked(wf, false, "Waiting", msg)
	// A holder that gives the lock back wakes the run. The poll covers a holder that was deleted and a new limit.
	return r.writeWaiting(ctx, wf, observed, ctrl.Result{RequeueAfter: lockPoll})
}

// writeWaiting writes the status of a run that the gate holds back, and tells the caller to stop there.
func (r *WorkflowReconciler) writeWaiting(ctx context.Context, wf *chroniclev1alpha1.Workflow, observed *chroniclev1alpha1.WorkflowStatus,
	result ctrl.Result) (ctrl.Result, bool, error) {
	if !equality.Semantic.DeepEqual(*observed, wf.Status) {
		if err := r.Status().Update(ctx, wf); apierrors.IsConflict(err) {
			return ctrl.Result{RequeueAfter: time.Second}, true, nil
		} else if err != nil {
			return ctrl.Result{}, true, fmt.Errorf("update the workflow status: %w", err)
		}
	}
	return result, true, nil
}

// unlock gives the lock back when the run has finished.
func (r *WorkflowReconciler) unlock(ctx context.Context, wf *chroniclev1alpha1.Workflow, spec *chroniclev1alpha1.WorkflowTemplateSpec) error {
	name, _, ok := lockOf(spec)
	if !ok || !finished(wf.Status.Phase) || !meta.IsStatusConditionTrue(wf.Status.Conditions, conditionLocked) {
		return nil
	}
	if err := r.release(ctx, wf, name); err != nil {
		return err
	}
	setLocked(wf, false, "Released", "the run gave the lock "+name+" back")
	return nil
}

// holderNames lists the runs that hold a lock, each run once. A run can hold a template lock with several nodes.
func holderNames(hs []holder) string {
	var names []string
	for _, h := range hs {
		if !slices.Contains(names, h.Name) {
			names = append(names, h.Name)
		}
	}
	return strings.Join(names, ", ")
}
