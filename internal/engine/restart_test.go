package engine

import (
	"slices"
	"strings"
	"testing"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// retryRun runs the spec, restarts it from a node (or the failed nodes) and runs it again with new outcomes.
func retryRun(t *testing.T, spec, from string, first, second map[string]v1alpha1.NodePhase) *sim {
	t.Helper()
	s := newSim(t, spec, first)
	s.run()
	st, pods, err := Restart(s.in.Spec, s.in.Status, wfName, from)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pods {
		delete(s.in.Pods, p)
	}
	s.in.Status, s.in.Restarts, s.outcomes = st, 1, second
	s.pods = map[string]CreatePod{}
	s.run()
	return s
}

func TestRestart(t *testing.T) {
	chain := dagSpec("    - {name: a, template: c}\n    - {name: b, template: c, depends: a}\n    - {name: d, template: c, depends: b}\n    - {name: x, template: c}")
	tests := []struct {
		name  string
		spec  string
		from  string
		first map[string]v1alpha1.NodePhase
		rerun []string // the Pods of the second try, by node name
		phase v1alpha1.WorkflowPhase
	}{
		{"retry the failed nodes", chain, "", map[string]v1alpha1.NodePhase{"wf.b": failed}, []string{"wf.b", "wf.d"}, v1alpha1.WorkflowSucceeded},
		{"retry from a node", chain, "wf.b", nil, []string{"wf.b", "wf.d"}, v1alpha1.WorkflowSucceeded},
		{"retry from a node by its short name", chain, "b", nil, []string{"wf.b", "wf.d"}, v1alpha1.WorkflowSucceeded},
		{"retry from the first node", chain, "wf.a", nil, []string{"wf.a", "wf.b", "wf.d"}, v1alpha1.WorkflowSucceeded},
		{"retry from a step resets the later groups", "entrypoint: main\ntemplates:\n- {name: main, steps: [[{name: a, template: c}, {name: b, template: c}], [{name: d, template: c}]]}" + pod,
			"wf[0].a", nil, []string{"wf[0].a", "wf[1].d"}, v1alpha1.WorkflowSucceeded},
		{"a retry node starts its attempts again", "entrypoint: c\ntemplates:\n- {name: c, retryStrategy: {limit: 1}, container: {image: x, command: [run]}}",
			"", map[string]v1alpha1.NodePhase{"wf(0)": failed, "wf(1)": failed}, []string{"wf(0)"}, v1alpha1.WorkflowSucceeded},
		{"the exit handler runs again", "entrypoint: c\nonExit: c\ntemplates:" + pod, "", map[string]v1alpha1.NodePhase{"wf": failed}, []string{"wf", "wf.onExit"}, v1alpha1.WorkflowSucceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := retryRun(t, tt.spec, tt.from, tt.first, nil)
			if s.in.Status.Phase != tt.phase {
				t.Errorf("phase = %s: %s", s.in.Status.Phase, s.in.Status.Message)
			}
			var got []string
			for name, p := range s.pods {
				got = append(got, name)
				if !strings.HasSuffix(p.PodName, "-r1") {
					t.Errorf("Pod %s of the second try has no generation suffix", p.PodName)
				}
			}
			if strings.Join(slices.Sorted(slices.Values(got)), ",") != strings.Join(tt.rerun, ",") {
				t.Errorf("second try ran %v, want %v", slices.Sorted(slices.Values(got)), tt.rerun)
			}
		})
	}
}

func TestRestartErrors(t *testing.T) {
	s := newSim(t, dagSpec("    - {name: a, template: c}"), nil)
	s.step(0)
	if _, _, err := Restart(s.in.Spec, s.in.Status, wfName, ""); err == nil {
		t.Error("a running workflow was restarted")
	}
	s.run()
	if _, _, err := Restart(s.in.Spec, s.in.Status, wfName, ""); err == nil || !strings.Contains(err.Error(), "re-run") {
		t.Errorf("a retry without failed nodes: %v", err)
	}
	if _, _, err := Restart(s.in.Spec, s.in.Status, wfName, "nope"); err == nil {
		t.Error("an unknown node was accepted")
	}
	two := newSim(t, "entrypoint: main\ntemplates:\n- {name: main, dag: {tasks: [{name: x, template: inner}, {name: y, template: inner}]}}\n"+
		"- {name: inner, dag: {tasks: [{name: a, template: c}]}}"+pod, nil)
	two.run()
	if _, _, err := Restart(two.in.Spec, two.in.Status, wfName, "a"); err == nil || !strings.Contains(err.Error(), "give its ID") {
		t.Errorf("a short name of two nodes: %v", err)
	}
	st, pods, err := Restart(s.in.Spec, s.in.Status, wfName, nodeByName(s.in.Status, "wf.a").ID)
	if err != nil || len(pods) != 1 || st.Phase != v1alpha1.WorkflowRunning || st.StartedAt != nil || len(st.Nodes) != 0 {
		t.Errorf("retry by node ID: %v, pods %v, status %+v", err, pods, st)
	}
}
