package controller

import (
	"context"
	"fmt"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/archive"
	"github.com/Thinnesen/chronicle/internal/engine"
)

// StepCache keeps the results of nodes of templates with memoize.
type StepCache interface {
	LoadCache(ctx context.Context, namespace string, keys []string) (map[string]archive.CacheEntry, error)
	SaveCache(ctx context.Context, namespace, key, workflow, node string, outputs *chroniclev1alpha1.Outputs) error
}

// runWithCache runs one engine step. When the step asks for cache keys, it reads them and runs the step again with
// the answers, so that a node with a saved result never starts a Pod.
func (r *WorkflowReconciler) runWithCache(ctx context.Context, wf *chroniclev1alpha1.Workflow, in engine.Input) (engine.Result, error) {
	res := engine.Run(in)
	var keys []string
	for _, a := range res.Actions {
		if l, ok := a.(engine.LookupCache); ok {
			keys = append(keys, l.Key)
		}
	}
	if len(keys) == 0 || r.Cache == nil {
		return res, nil
	}
	saved, err := r.Cache.LoadCache(ctx, wf.Namespace, keys)
	if err != nil {
		return engine.Result{}, fmt.Errorf("read the step cache: %w", err)
	}
	in.Cache = make(map[string]*engine.CacheEntry, len(keys))
	for _, k := range keys {
		in.Cache[k] = nil
		if e, ok := saved[k]; ok {
			in.Cache[k] = &engine.CacheEntry{Outputs: e.Outputs, Workflow: e.Workflow, SavedAt: e.SavedAt}
		}
	}
	return engine.Run(in), nil
}
