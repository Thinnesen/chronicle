package engine

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

const (
	succeeded = v1alpha1.NodeSucceeded
	failed    = v1alpha1.NodeFailed
	errored   = v1alpha1.NodeError
	omitted   = v1alpha1.NodeOmitted
)

const pod = "\n- {name: c, container: {image: alpine, command: [run]}}"

func dagSpec(tasks string) string {
	return "entrypoint: main\ntemplates:\n- name: main\n  dag:\n    tasks:\n" + tasks + pod
}

func TestEngine(t *testing.T) {
	tests := []struct {
		name     string
		spec     string
		outcomes map[string]v1alpha1.NodePhase
		phase    v1alpha1.WorkflowPhase
		message  string
		waves    [][]string
		nodes    map[string]v1alpha1.NodePhase
	}{
		{
			name: "diamond",
			spec: dagSpec(`
    - {name: a, template: c}
    - {name: b, template: c, dependencies: [a]}
    - {name: d, template: c, depends: b && e}
    - {name: e, template: c, depends: a.Succeeded}`),
			phase: v1alpha1.WorkflowSucceeded,
			waves: [][]string{{"wf.a"}, {"wf.b", "wf.e"}, {"wf.d"}},
			nodes: map[string]v1alpha1.NodePhase{"wf": succeeded, "wf.a": succeeded, "wf.b": succeeded, "wf.d": succeeded, "wf.e": succeeded},
		},
		{
			name: "fail fast stops new tasks",
			spec: dagSpec(`
    - {name: a, template: c}
    - {name: b, template: c, dependencies: [a]}
    - {name: x, template: c}
    - {name: z, template: c, dependencies: [x]}`),
			outcomes: map[string]v1alpha1.NodePhase{"wf.a": failed},
			phase:    v1alpha1.WorkflowFailed,
			message:  `task "a" failed`,
			waves:    [][]string{{"wf.a", "wf.x"}},
			nodes:    map[string]v1alpha1.NodePhase{"wf": failed, "wf.a": failed, "wf.b": omitted, "wf.x": succeeded, "wf.z": omitted},
		},
		{
			name: "without fail fast other branches go on",
			spec: "entrypoint: main\ntemplates:\n- name: main\n  dag:\n    failFast: false\n    tasks:\n" + `
    - {name: a, template: c}
    - {name: b, template: c, dependencies: [a]}
    - {name: x, template: c}
    - {name: z, template: c, dependencies: [x]}` + pod,
			outcomes: map[string]v1alpha1.NodePhase{"wf.a": failed},
			phase:    v1alpha1.WorkflowFailed,
			waves:    [][]string{{"wf.a", "wf.x"}, {"wf.z"}},
			nodes:    map[string]v1alpha1.NodePhase{"wf.a": failed, "wf.b": omitted, "wf.z": succeeded},
		},
		{
			name: "a handled failure does not fail the DAG",
			spec: dagSpec(`
    - {name: a, template: c}
    - {name: x, template: c}
    - {name: z, template: c, dependencies: [x]}
    - {name: onfail, template: c, depends: a.Failed || a.Errored}
    - {name: onok, template: c, depends: "!a.Failed && z"}`),
			outcomes: map[string]v1alpha1.NodePhase{"wf.a": failed},
			phase:    v1alpha1.WorkflowSucceeded,
			waves:    [][]string{{"wf.a", "wf.x"}, {"wf.onfail", "wf.z"}},
			nodes:    map[string]v1alpha1.NodePhase{"wf.a": failed, "wf.onfail": succeeded, "wf.onok": omitted},
		},
		{
			name: "an omitted leaf passes down the failure",
			spec: dagSpec(`
    - {name: a, template: c}
    - {name: b, template: c, depends: a.Errored}`),
			outcomes: map[string]v1alpha1.NodePhase{"wf.a": failed},
			phase:    v1alpha1.WorkflowFailed,
			message:  `task "a" failed`,
			nodes:    map[string]v1alpha1.NodePhase{"wf.a": failed, "wf.b": omitted},
		},
		{
			name: "an error is worse than a failure",
			spec: dagSpec(`
    - {name: a, template: c}
    - {name: b, template: c}`),
			outcomes: map[string]v1alpha1.NodePhase{"wf.a": failed, "wf.b": errored},
			phase:    v1alpha1.WorkflowError,
			message:  `task "a" failed`,
		},
		{
			name: "steps",
			spec: `
entrypoint: main
templates:
- name: main
  steps:
  - - {name: a, template: c}
    - {name: b, template: c}
  - - {name: d, template: c}` + pod,
			phase: v1alpha1.WorkflowSucceeded,
			waves: [][]string{{"wf[0].a", "wf[0].b"}, {"wf[1].d"}},
			nodes: map[string]v1alpha1.NodePhase{"wf": succeeded, "wf[0]": succeeded, "wf[1]": succeeded, "wf[0].a": succeeded, "wf[1].d": succeeded},
		},
		{
			name: "a failed step omits the next groups",
			spec: `
entrypoint: main
templates:
- name: main
  steps:
  - - {name: a, template: c}
    - {name: b, template: c}
  - - {name: d, template: c}
  - - {name: e, template: c}` + pod,
			outcomes: map[string]v1alpha1.NodePhase{"wf[0].b": failed},
			phase:    v1alpha1.WorkflowFailed,
			message:  `task "b" failed`,
			waves:    [][]string{{"wf[0].a", "wf[0].b"}},
			nodes: map[string]v1alpha1.NodePhase{"wf[0]": failed, "wf[1]": omitted, "wf[2]": omitted,
				"wf[0].a": succeeded, "wf[0].b": failed, "wf[1].d": omitted, "wf[2].e": omitted},
		},
		{
			name: "nested templates",
			spec: dagSpec(`
    - {name: a, template: inner}
    - {name: b, template: c, dependencies: [a]}`) + `
- name: inner
  steps:
  - - {name: s, template: deep}
- name: deep
  dag:
    tasks:
    - {name: x, template: c}`,
			phase: v1alpha1.WorkflowSucceeded,
			waves: [][]string{{"wf.a[0].s.x"}, {"wf.b"}},
			nodes: map[string]v1alpha1.NodePhase{"wf.a": succeeded, "wf.a[0]": succeeded, "wf.a[0].s": succeeded, "wf.a[0].s.x": succeeded},
		},
		{
			name: "an omitted nested template",
			spec: dagSpec(`
    - {name: a, template: c}
    - {name: b, template: inner, depends: a.Failed}
    - {name: s, template: st, depends: a.Failed}`) + `
- {name: inner, dag: {tasks: [{name: x, template: c}]}}
- {name: st, steps: [[{name: x, template: c}]]}`,
			phase: v1alpha1.WorkflowSucceeded,
			nodes: map[string]v1alpha1.NodePhase{"wf.b": omitted, "wf.s": omitted},
		},
		{
			name:    "recursion without end",
			spec:    "entrypoint: main\ntemplates:\n- {name: main, steps: [[{name: again, template: main}]]}",
			phase:   v1alpha1.WorkflowError,
			message: `task "again" failed`,
		},
		{
			name:    "an invalid spec",
			spec:    "entrypoint: nope\ntemplates: [{name: c, container: {image: x}}]",
			phase:   v1alpha1.WorkflowError,
			message: "invalid spec",
		},
		{
			name:  "a single container",
			spec:  "entrypoint: c\ntemplates:" + pod,
			phase: v1alpha1.WorkflowSucceeded,
			waves: [][]string{{"wf"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSim(t, tt.spec, tt.outcomes)
			st := s.run()
			if st.Phase != tt.phase {
				t.Errorf("phase = %s, want %s (message %q)", st.Phase, tt.phase, st.Message)
			}
			if !strings.Contains(st.Message, tt.message) {
				t.Errorf("message = %q, want %q", st.Message, tt.message)
			}
			if tt.waves != nil && !reflect.DeepEqual(sortedWaves(s.waves), tt.waves) {
				t.Errorf("waves = %v, want %v", sortedWaves(s.waves), tt.waves)
			}
			got := phases(st)
			for name, want := range tt.nodes {
				if got[name] != want {
					t.Errorf("node %s = %q, want %q", name, got[name], want)
				}
			}
			if st.StartedAt == nil || st.FinishedAt == nil {
				t.Error("startedAt and finishedAt must be set")
			}
		})
	}
}

func TestEngineStepGroupDisplayNames(t *testing.T) {
	s := newSim(t, "entrypoint: main\ntemplates:\n- {name: main, steps: [[{name: a, template: c}]]}"+pod, nil)
	st := s.run()
	if g := nodeByName(st, "wf[0]"); g.DisplayName != "[0]" || g.Type != v1alpha1.NodeStepGroup {
		t.Errorf("group node = %+v", g)
	}
	if n := nodeByName(st, "wf[0].a"); n.DisplayName != "a" || n.PodName != n.ID || n.TemplateName != "c" {
		t.Errorf("step node = %+v", n)
	}
	root := nodeByName(st, "wf")
	if root.ID != wfName || len(root.Children) != 1 {
		t.Errorf("root node = %+v", root)
	}
}

func TestEnginePodStates(t *testing.T) {
	s := newSim(t, "entrypoint: c\ntemplates:"+pod, nil)
	r := Run(s.in)
	if len(r.Actions) != 1 {
		t.Fatalf("actions = %v", r.Actions)
	}
	id := r.Actions[0].(CreatePod).NodeID
	s.in.Status = r.Status

	// A Pending node without an observed Pod asks again for the Pod (the create is idempotent).
	if r = Run(s.in); len(r.Actions) != 1 {
		t.Fatalf("actions after a lost create = %v", r.Actions)
	}

	// An observed Pending Pod asks for nothing.
	s.in.Pods[id] = PodResult{Phase: v1alpha1.NodePending}
	if r = Run(s.in); len(r.Actions) != 0 || r.Status.Nodes[id].Phase != v1alpha1.NodePending {
		t.Fatalf("pending: actions %v, node %+v", r.Actions, r.Status.Nodes[id])
	}

	// A Pod that can never start ends its node, and the engine deletes the Pod once.
	s.in.Pods[id] = PodResult{Phase: v1alpha1.NodeError, Message: "InvalidImageName: bad", Stuck: true}
	stuck := Run(s.in)
	if n := stuck.Status.Nodes[id]; n.Phase != v1alpha1.NodeError || len(stuck.Actions) != 1 || stuck.Actions[0] != (DeletePod{PodName: n.PodName}) {
		t.Fatalf("stuck: actions %v, node %+v", stuck.Actions, n)
	}
	s.in.Status = stuck.Status
	if again := Run(s.in); len(again.Actions) != 0 {
		t.Fatalf("stuck again: actions %v", again.Actions)
	}
	s.in.Status = r.Status

	// A Running Pod has the start time of its node. The node keeps the key of its archived log at once, so that a
	// stop that deletes the Pod does not lose the log.
	s.in.Pods[id] = PodResult{Phase: v1alpha1.NodeRunning, LogKey: "ns/uid/pod/main.log"}
	r = Run(s.in)
	if n := r.Status.Nodes[id]; n.Phase != v1alpha1.NodeRunning || n.StartedAt == nil || n.LogKey != "ns/uid/pod/main.log" {
		t.Fatalf("running node = %+v", n)
	}
	s.in.Status = r.Status

	// A Running node whose Pod is gone is an error.
	delete(s.in.Pods, id)
	r = Run(s.in)
	if n := r.Status.Nodes[id]; n.Phase != v1alpha1.NodeError || n.Message != "the Pod was deleted" {
		t.Fatalf("node without Pod = %+v", n)
	}
	if r.Status.Phase != v1alpha1.WorkflowError {
		t.Fatalf("workflow phase = %s", r.Status.Phase)
	}

	// A finished workflow does not change.
	s.in.Status = r.Status
	s.in.Pods[id] = PodResult{Phase: v1alpha1.NodeSucceeded}
	if again := Run(s.in); !reflect.DeepEqual(again.Status, r.Status) || len(again.Actions) != 0 {
		t.Fatal("a finished workflow changed")
	}
}

func TestEngineOutputsAndTimes(t *testing.T) {
	s := newSim(t, "entrypoint: c\ntemplates:"+pod, nil)
	r := Run(s.in)
	id := r.Actions[0].(CreatePod).NodeID
	s.in.Status = r.Status
	start, end := at(0), at(9)
	out := &v1alpha1.Outputs{Parameters: []v1alpha1.Parameter{{Name: "result"}}}
	s.in.Pods[id] = PodResult{Phase: v1alpha1.NodeSucceeded, FinishedAt: &end, Outputs: out}
	n := Run(s.in).Status.Nodes[id]
	if !n.StartedAt.Equal(&start) || !n.FinishedAt.Equal(&end) || n.Outputs != out {
		t.Fatalf("node = %+v", n)
	}
}

func TestEngineIsDeterministic(t *testing.T) {
	spec := dagSpec(`
    - {name: a, template: c}
    - {name: b, template: c}
    - {name: d, template: c, depends: a && b}`)
	s1, s2 := newSim(t, spec, nil), newSim(t, spec, nil)
	for i := range 5 {
		r1, r2 := s1.step(i), s2.step(i)
		if !reflect.DeepEqual(r1, r2) {
			t.Fatalf("step %d: results differ", i)
		}
	}
}

// TestEngineStopCreatesNoPod stops a run when its first step has just succeeded. The engine must not start the next
// step: the controller applies a create and a delete in parallel, so the Pod could run after all.
func TestEngineStopCreatesNoPod(t *testing.T) {
	s := newSim(t, "entrypoint: main\nonExit: c\ntemplates:\n- {name: main, steps: [[{name: first, template: c}], [{name: second, template: c}]]}"+pod, nil)
	r := Run(s.in)
	first := r.Actions[0].(CreatePod)
	s.in.Status = r.Status
	s.in.Pods[first.PodName] = PodResult{Phase: v1alpha1.NodeSucceeded}
	s.in.Shutdown = v1alpha1.ShutdownStop
	r = Run(s.in)
	for _, a := range r.Actions {
		if c, ok := a.(CreatePod); ok && c.NodeName != wfName+".onExit" {
			t.Fatalf("the stopped run creates the Pod of %s", c.NodeName)
		}
	}
	if n := r.Status.Nodes[first.NodeID]; n.Phase != v1alpha1.NodeSucceeded {
		t.Errorf("the first step that finished is %s, want Succeeded", n.Phase)
	}
	// A stopped run still runs its exit handler.
	if len(r.Actions) == 0 {
		t.Error("the exit handler does not start")
	}
}

// TestEngineRetryKeepsTheTree links the nodes that a retry keeps to their new parent. Before, the tree of a retried run
// left them out.
func TestEngineRetryKeepsTheTree(t *testing.T) {
	s := newSim(t, "entrypoint: main\ntemplates:\n- {name: main, dag: {tasks: [{name: a, template: c}, {name: b, template: c, depends: a}]}}"+pod,
		map[string]v1alpha1.NodePhase{"wf.b": v1alpha1.NodeFailed})
	first := s.run()
	next, _, err := Restart(s.in.Spec, first, wfName, "")
	if err != nil {
		t.Fatal(err)
	}
	s.in.Status = next
	after := s.run()
	names := make([]string, 0, len(after.Nodes[wfName].Children))
	for _, id := range after.Nodes[wfName].Children {
		names = append(names, after.Nodes[id].Name)
	}
	if strings.Join(names, " ") != "wf.a wf.b" {
		t.Errorf("children of the root after the retry: %v", names)
	}
}
