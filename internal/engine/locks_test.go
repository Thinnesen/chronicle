package engine

import (
	"strings"
	"testing"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// TestTemplateLock checks that a node starts only with the lock of its template, and gives it back when it ends.
func TestTemplateLock(t *testing.T) {
	s := newSim(t, `
entrypoint: main
templates:
- name: main
  dag: {tasks: [{name: a, template: deploy}, {name: b, template: deploy}]}
- name: deploy
  synchronization: {mutex: {name: prod}}
  container: {image: x, command: [deploy]}
`, nil)
	r := s.step(0)
	acquire := map[string]bool{}
	for _, a := range r.Actions {
		if l, ok := a.(AcquireLock); ok && l.Lock == "prod" && l.Limit == 1 {
			acquire[l.NodeID] = true
		}
	}
	a, b := nodeByName(r.Status, "wf.a"), nodeByName(r.Status, "wf.b")
	if len(s.pods) != 0 || !acquire[a.ID] || !acquire[b.ID] || !strings.HasPrefix(b.Message, "waiting for the lock prod") {
		t.Fatalf("pods %v, acquire %v, b %q", s.pods, acquire, b.Message)
	}
	s.in.HeldLocks = map[string]string{a.ID: "prod"}
	s.step(1)
	if _, ok := s.pods["wf.a"]; !ok || len(s.pods) != 1 {
		t.Fatalf("pods = %v", s.pods)
	}
	r = s.step(2) // the Pod of a finishes
	released := false
	for _, act := range r.Actions {
		if l, ok := act.(ReleaseLock); ok && l.NodeID == a.ID && l.Lock == "prod" {
			released = true
		}
	}
	if !released || nodeByName(r.Status, "wf.a").Phase != v1alpha1.NodeSucceeded {
		t.Errorf("released %v, actions %v", released, r.Actions)
	}
}
