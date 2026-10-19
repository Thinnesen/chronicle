package controller

import (
	"testing"
	"time"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/engine"
)

func TestCreatedPods(t *testing.T) {
	var c createdPods
	t0 := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	c.add("wf", "a", t0)
	c.add("wf", "b", t0)

	// The cache shows a, but not b yet: b counts as a Pending Pod.
	observed := map[string]engine.PodResult{"a": {Phase: chroniclev1alpha1.NodeRunning}}
	c.fill("wf", observed, t0.Add(time.Second))
	if observed["a"].Phase != chroniclev1alpha1.NodeRunning || observed["b"].Phase != chroniclev1alpha1.NodePending {
		t.Fatalf("observed %v", observed)
	}
	// a is forgotten once seen; b is forgotten after the TTL.
	observed = map[string]engine.PodResult{}
	c.fill("wf", observed, t0.Add(2*time.Second))
	if _, ok := observed["a"]; ok {
		t.Error("a seen Pod must be forgotten")
	}
	if _, ok := observed["b"]; !ok {
		t.Error("b must still count")
	}
	observed = map[string]engine.PodResult{}
	c.fill("wf", observed, t0.Add(2*createdTTL))
	if len(observed) != 0 || len(c.pods) != 0 {
		t.Errorf("after the TTL: %v %v", observed, c.pods)
	}
}
