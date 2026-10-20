package controller

import (
	"context"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/archive"
)

// memCache is a step cache in memory.
type memCache struct {
	mu      sync.Mutex
	entries map[string]archive.CacheEntry
}

func (m *memCache) LoadCache(_ context.Context, namespace string, keys []string) (map[string]archive.CacheEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]archive.CacheEntry{}
	for _, k := range keys {
		if e, ok := m.entries[namespace+"/"+k]; ok {
			out[k] = e
		}
	}
	return out, nil
}

func (m *memCache) SaveCache(_ context.Context, namespace, key, workflow, _ string, outputs *chroniclev1alpha1.Outputs) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[namespace+"/"+key] = archive.CacheEntry{Outputs: outputs, Workflow: workflow, SavedAt: time.Now()}
	return nil
}

var _ = Describe("memoize", func() {
	ctx := context.Background()
	const spec = `
entrypoint: main
templates:
- name: main
  memoize: {key: "build-v1"}
  container: {image: alpine, command: [build]}
`

	It("saves the result of the first run and reuses it in the second", func() {
		cache := &memCache{entries: map[string]archive.CacheEntry{}}
		r := testReconciler()
		r.Cache = cache

		first := reconcileWith(ctx, r, newWorkflow(ctx, spec))
		pods := podsOf(ctx, first)
		Expect(pods).To(HaveLen(1))
		Expect(first.Status.Nodes[first.Name].Cache).To(Equal(&chroniclev1alpha1.NodeCache{Key: "build-v1"}))
		finishPod(ctx, &pods[0])
		first = reconcileWith(ctx, r, client.ObjectKeyFromObject(first))
		Expect(first.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowSucceeded))
		Expect(cache.entries).To(HaveKey("default/build-v1"))

		second := reconcileWith(ctx, r, newWorkflow(ctx, spec))
		Expect(second.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowSucceeded))
		Expect(podsOf(ctx, second)).To(BeEmpty())
		Expect(second.Status.Nodes[second.Name].Cache).To(Equal(&chroniclev1alpha1.NodeCache{Key: "build-v1", Hit: true, Workflow: first.Name}))
	})

	It("fails a memoize node without the archive database", func() {
		wf := reconcileWith(ctx, testReconciler(), newWorkflow(ctx, spec))
		Expect(wf.Status.Phase).To(Equal(chroniclev1alpha1.WorkflowError))
		Expect(wf.Status.Nodes[wf.Name].Message).To(Equal("memoize needs the archive database"))
	})
})
