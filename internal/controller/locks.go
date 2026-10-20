package controller

import (
	"context"
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

// applyLock takes or gives back the lock of a node. It returns true when it took a lock.
func (r *WorkflowReconciler) applyLock(ctx context.Context, wf *chroniclev1alpha1.Workflow, a engine.Action) (bool, error) {
	switch a := a.(type) {
	case engine.AcquireLock:
		limit, err := r.lockLimit(ctx, wf.Namespace, a.Limit, a.LimitFrom)
		if err != nil {
			return false, err
		}
		held, _, err := r.acquireAs(ctx, wf.Namespace, a.Lock, limit, holder{UID: string(wf.UID), Name: wf.Name, Node: a.NodeID})
		return held, err
	case engine.ReleaseLock:
		return false, r.releaseWhere(ctx, wf.Namespace, a.Lock, func(h holder) bool { return h.UID == string(wf.UID) && h.Node == a.NodeID })
	}
	return false, nil
}

// lockLimit returns the limit of a lock: the number of the spec, or the number in a key of a labeled ConfigMap. The
// controller reads the ConfigMap each time, so a new limit counts at the next try. A bad ConfigMap is ErrInvalid.
func (r *WorkflowReconciler) lockLimit(ctx context.Context, namespace string, limit int, from *chroniclev1alpha1.ConfigMapKeyRef) (int, error) {
	if from == nil {
		return limit, nil
	}
	v, found, err := configMapGetter{r.reader(), namespace}.ConfigMapValue(ctx, from.Name, from.Key)
	if err != nil {
		return 0, fmt.Errorf("the limit of a lock: %w", err)
	}
	n, perr := strconv.Atoi(strings.TrimSpace(v))
	if !found || perr != nil || n < 1 {
		return 0, fmt.Errorf("the limit of a lock: ConfigMap %q needs a number of 1 or more in key %q: %w", from.Name, from.Key, resolve.ErrInvalid)
	}
	return n, nil
}
