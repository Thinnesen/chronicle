package controller

import (
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/engine"
)

// createdTTL is how long a created Pod counts without the cache. The cache shows a new Pod in much less time.
const createdTTL = time.Minute

// createdPods remembers the Pods that the controller created and the cache does not show yet. The ReplicaSet controller
// has the same expectations. Without them, a reconcile with a stale cache would not see such a Pod. The same is true
// after a status update that failed with a conflict. Then it could start the Pod of another node and go over a
// parallelism limit.
type createdPods struct {
	mu   sync.Mutex
	pods map[types.UID]map[string]time.Time
}

// add remembers a created Pod of a workflow.
func (c *createdPods) add(uid types.UID, pod string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pods == nil {
		c.pods = map[types.UID]map[string]time.Time{}
	}
	if c.pods[uid] == nil {
		c.pods[uid] = map[string]time.Time{}
	}
	c.pods[uid][pod] = now
}

// fill adds each remembered Pod that the cache does not show as a Pending Pod. It forgets the Pods that the cache
// shows and the Pods that are older than createdTTL.
func (c *createdPods) fill(uid types.UID, observed map[string]engine.PodResult, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for pod, at := range c.pods[uid] {
		_, seen := observed[pod]
		switch {
		case seen || now.Sub(at) > createdTTL:
			delete(c.pods[uid], pod)
		default:
			observed[pod] = engine.PodResult{Phase: chroniclev1alpha1.NodePending}
		}
	}
	if len(c.pods[uid]) == 0 {
		delete(c.pods, uid)
	}
}
