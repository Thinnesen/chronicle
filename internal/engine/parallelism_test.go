package engine

import (
	"strings"
	"testing"
)

func TestParallelism(t *testing.T) {
	tests := []struct {
		name  string
		spec  string
		waves []int
	}{
		{"the workflow limit holds back a fan-out", `
entrypoint: main
parallelism: 2
templates:
- name: main
  dag:
    tasks:
    - {name: item, template: c, withItems: [1, 2, 3, 4, 5]}
- {name: c, container: {image: x, command: [run]}}`, []int{2, 2, 1}},
		{"a DAG limit runs its tasks one by one", `
entrypoint: main
templates:
- name: main
  parallelism: 1
  dag:
    tasks:
    - {name: a, template: c}
    - {name: b, template: c}
    - {name: d, template: c}
- {name: c, container: {image: x, command: [run]}}`, []int{1, 1, 1}},
		{"a limit of steps counts the Pods below the steps node", `
entrypoint: main
templates:
- name: main
  steps:
  - - {name: a, template: inner}
    - {name: b, template: inner}
- name: inner
  parallelism: 1
  steps:
  - - {name: x, template: c}
    - {name: y, template: c}
- {name: c, container: {image: x, command: [run]}}`, []int{2, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSim(t, tt.spec, nil)
			st := s.run()
			if st.Phase != "Succeeded" {
				t.Fatalf("phase %s: %v", st.Phase, phases(st))
			}
			var got []int
			for _, w := range s.waves {
				got = append(got, len(w))
			}
			if !equalInts(got, tt.waves) {
				t.Errorf("waves %v, want %v", got, tt.waves)
			}
		})
	}
}

// TestParallelismCacheLag checks that a Pod that the engine asked for still holds its slot when the cache does not show
// it yet.
func TestParallelismCacheLag(t *testing.T) {
	s := newSim(t, `
entrypoint: main
parallelism: 1
templates:
- name: main
  dag:
    tasks:
    - {name: a, template: c}
    - {name: b, template: c}
- {name: c, container: {image: x, command: [run]}}`, nil)
	first := Run(s.in)
	s.in.Status = first.Status
	s.in.Now = at(1)
	again := Run(s.in) // The cache has no Pod yet.
	var created []string
	for _, a := range again.Actions {
		if cp, ok := a.(CreatePod); ok {
			created = append(created, cp.NodeName)
		}
	}
	if len(created) != 1 || created[0] != "wf.a" {
		t.Fatalf("created %v, want only the Pod of wf.a again", created)
	}
	if b := nodeByName(again.Status, "wf.b"); !strings.HasPrefix(b.Message, waitingForSlot) {
		t.Errorf("wf.b: %q", b.Message)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestParallelismUnknownPod checks that a running Pod whose node the status does not have yet holds a slot.
func TestParallelismUnknownPod(t *testing.T) {
	s := newSim(t, `
entrypoint: main
parallelism: 1
templates:
- name: main
  dag:
    tasks:
    - {name: a, template: c}
- {name: c, container: {image: x, command: [run]}}`, nil)
	s.in.Pods["wf-from-an-older-status"] = PodResult{Phase: "Running"}
	for _, a := range Run(s.in).Actions {
		if _, ok := a.(CreatePod); ok {
			t.Fatal("a Pod started although the only slot is taken")
		}
	}
}
