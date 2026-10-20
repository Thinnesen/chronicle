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

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/engine"
	"github.com/Thinnesen/chronicle/internal/resolve"
)

// conditionLocked tells that a run holds the lock of its synchronization.
const conditionLocked = "Synchronized"

// annotationHolders lists the runs that hold a lock, on the Lease of the lock.
const annotationHolders = "chronicle.thinnesen.com/holders"

// lockPoll is the wait of a run for a lock between two tries.
const lockPoll = 5 * time.Second

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
func (r *WorkflowReconciler) acquire(ctx context.Context, wf *chroniclev1alpha1.Workflow, name string, limit int) (bool, string, error) {
	return r.acquireAs(ctx, wf.Namespace, name, limit, holder{UID: string(wf.UID), Name: wf.Name})
}

// acquireAs takes the lock for a run or a node if a slot is free. It returns false and a message when it must wait. The
// Lease update has the resource version of the read. So a concurrent change makes it fail, and nothing is lost.
func (r *WorkflowReconciler) acquireAs(ctx context.Context, namespace, name string, limit int, me holder) (bool, string, error) {
	var lease coordinationv1.Lease
	err := r.reader().Get(ctx, client.ObjectKey{Namespace: namespace, Name: lockPrefix + name}, &lease)
	if apierrors.IsNotFound(err) {
		lease = coordinationv1.Lease{Name: lockPrefix + name, Namespace: namespace}
		setHolders(&lease, []holder{me})
		if err := r.Create(ctx, &lease); apierrors.IsAlreadyExists(err) {
			return false, "waiting for the lock " + name, nil
		} else if err != nil {
			return false, "", fmt.Errorf("create the lock %s: %w", name, err)
		}
		return true, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("read the lock %s: %w", name, err)
	}
	all := holdersOf(&lease)
	if slices.Contains(all, me) {
		return true, "", nil
	}
	alive, err := r.liveHolders(ctx, namespace, all)
	if err != nil {
		return false, "", err
	}
	if len(alive) >= limit {
		names := make([]string, 0, len(alive))
		for _, h := range alive {
			names = append(names, h.Name)
		}
		return false, fmt.Sprintf("waiting for the lock %s, which %s holds", name, strings.Join(names, ", ")), nil
	}
	setHolders(&lease, append(alive, me))
	if err := r.Update(ctx, &lease); apierrors.IsConflict(err) {
		return false, "waiting for the lock " + name, nil
	} else if err != nil {
		return false, "", fmt.Errorf("take the lock %s: %w", name, err)
	}
	return true, "", nil
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
	setHolders(&lease, slices.DeleteFunc(holdersOf(&lease), match))
	if err := r.Update(ctx, &lease); err != nil {
		return fmt.Errorf("give back the lock %s: %w", name, err)
	}
	return nil
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
	if errors.Is(err, resolve.ErrInvalid) {
		res, err := r.setError(ctx, wf, err.Error())
		return res, true, err
	}
	if err != nil {
		return ctrl.Result{}, true, err
	}
	held, msg, err := r.acquire(ctx, wf, name, limit)
	if err != nil {
		return ctrl.Result{}, true, err
	}
	if held {
		setLocked(wf, true, "Acquired", "the run holds the lock "+name)
		return ctrl.Result{}, false, nil
	}
	wf.Status.Phase, wf.Status.Message = chroniclev1alpha1.WorkflowPending, msg
	setLocked(wf, false, "Waiting", msg)
	// A holder that finishes does not wake the waiting runs, so they look again soon.
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
