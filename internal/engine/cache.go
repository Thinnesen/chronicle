package engine

import (
	"fmt"
	"time"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/expr"
)

// maxCacheKey is the longest resolved cache key.
const maxCacheKey = 2048

// CacheEntry is the saved result of a node of a template with memoize.
type CacheEntry struct {
	Outputs  *v1alpha1.Outputs
	Workflow string
	SavedAt  time.Time
}

// LookupCache asks the controller for the saved result of a key. The controller runs the engine again with the
// answer in Input.Cache, so the engine itself keeps no state between steps.
type LookupCache struct {
	Key string
}

// SaveCache asks the controller to save the outputs of a succeeded node under a key.
type SaveCache struct {
	Key      string
	NodeID   string
	NodeName string
	Outputs  *v1alpha1.Outputs
}

func (LookupCache) isAction() {}
func (SaveCache) isAction()   {}

// fromCache decides about a pending node of a template with memoize. It returns true when the node does not start now.
// Then the node reused a saved result, waits for the lookup of its key, or failed.
func (e *engine) fromCache(c call, t *v1alpha1.Template, n *v1alpha1.NodeStatus) bool {
	m := t.Memoize
	if m == nil || n.Cache != nil {
		return false
	}
	if !e.in.CacheEnabled {
		e.setPhase(n, v1alpha1.NodeError, "memoize needs the archive database")
		return true
	}
	key, err := expr.Substitute(m.Key, e.nodeScope(c, *n))
	switch {
	case err != nil:
		e.setPhase(n, v1alpha1.NodeError, fmt.Sprintf("memoize key: %v", err))
		return true
	case len(key) > maxCacheKey:
		e.setPhase(n, v1alpha1.NodeError, fmt.Sprintf("the memoize key is longer than %d characters", maxCacheKey))
		return true
	}
	entry, known := e.in.Cache[key]
	if !known {
		e.actions = append(e.actions, LookupCache{Key: key})
		return true
	}
	n.Cache = &v1alpha1.NodeCache{Key: key}
	if entry == nil || m.MaxAge != nil && e.in.Now.Sub(entry.SavedAt) > m.MaxAge.Duration {
		return false
	}
	// The node never has a Pod.
	n.Cache.Hit, n.Cache.Workflow, n.PodName = true, entry.Workflow, ""
	n.Outputs = entry.Outputs.DeepCopy()
	now := e.in.Now
	n.FinishedAt = &now
	e.setPhase(n, v1alpha1.NodeSucceeded, "the result comes from the cache of run "+entry.Workflow)
	return true
}

// saveToCache saves the outputs of a node that ran because its key had no valid result, when the node succeeds.
func (e *engine) saveToCache(c call, n v1alpha1.NodeStatus, wasDone bool) {
	if !wasDone && n.Phase == v1alpha1.NodeSucceeded && n.Cache != nil && !n.Cache.Hit {
		e.actions = append(e.actions, SaveCache{Key: n.Cache.Key, NodeID: n.ID, NodeName: c.nodeName, Outputs: n.Outputs})
	}
}
