package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/engine"
	"github.com/Thinnesen/chronicle/internal/resolve"
)

// heldLocks returns the nodes of a run that hold the lock of their template: node ID to lock name.
func (r *WorkflowReconciler) heldLocks(ctx context.Context, wf *chroniclev1alpha1.Workflow, spec *chroniclev1alpha1.WorkflowTemplateSpec) (map[string]string, error) {
	var names []string
	for i := range spec.Templates {
		if name, _, ok := spec.Templates[i].Synchronization.Lock(); ok && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	held := map[string]string{}
	for _, name := range names {
		var lease coordinationv1.Lease
		err := r.reader().Get(ctx, client.ObjectKey{Namespace: wf.Namespace, Name: lockPrefix + name}, &lease)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read the lock %s: %w", name, err)
		}
		for _, h := range holdersOf(&lease) {
			if h.UID == string(wf.UID) && h.Node != "" {
				held[h.Node] = name
			}
		}
	}
	return held, nil
}

// applyLock takes or gives back the lock of a node. It returns true when it took a lock, or else the message of a
// node that waits, which names the holders.
func (r *WorkflowReconciler) applyLock(ctx context.Context, wf *chroniclev1alpha1.Workflow, a engine.Action) (bool, string, error) {
	switch a := a.(type) {
	case engine.AcquireLock:
		limit, err := r.lockLimit(ctx, wf.Namespace, a.Limit, a.LimitFrom)
		if err != nil {
			return false, "", err
		}
		return r.acquireAs(ctx, wf.Namespace, a.Lock, limit, holder{UID: string(wf.UID), Name: wf.Name, Node: a.NodeID})
	case engine.ReleaseLock:
		return false, "", r.releaseWhere(ctx, wf.Namespace, a.Lock, func(h holder) bool { return h.UID == string(wf.UID) && h.Node == a.NodeID })
	}
	return false, "", nil
}

// lockLimit returns the limit of a lock: the number of the spec, or the number in a key of a labeled ConfigMap. The
// controller reads the ConfigMap each time, so a new limit counts at the next try. A bad ConfigMap is ErrInvalid, and
// the run or the node waits until a user repairs it.
func (r *WorkflowReconciler) lockLimit(ctx context.Context, namespace string, limit int, from *chroniclev1alpha1.ConfigMapKeyRef) (int, error) {
	if from == nil {
		return limit, nil
	}
	v, found, err := configMapGetter{r.reader(), namespace}.ConfigMapValue(ctx, from.Name, from.Key)
	if apierrors.IsNotFound(err) {
		return 0, resolve.Invalidf("ConfigMap %q does not exist", from.Name)
	}
	if err != nil && !errors.Is(err, resolve.ErrInvalid) {
		err = fmt.Errorf("read the limit of a lock: %w", err)
	}
	if err != nil {
		return 0, err
	}
	n, perr := strconv.Atoi(strings.TrimSpace(v))
	if !found || perr != nil || n < 1 {
		return 0, resolve.Invalidf("ConfigMap %q needs a number of 1 or more in key %q", from.Name, from.Key)
	}
	return n, nil
}

// lockProblem is the message of a run or a node that waits, because the limit of its lock is not valid.
func lockProblem(lock string, err error) string {
	return fmt.Sprintf("%s %s: %v", engine.WaitingForLock, lock, err)
}
