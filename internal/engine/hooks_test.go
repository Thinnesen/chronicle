package engine

import (
	"strings"
	"testing"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

const hookSpec = `
entrypoint: main
templates:
- name: main
  steps:
  - - {name: build, template: c, onExit: {template: report, arguments: {parameters: [{name: s, value: "{{steps.build.status}}"}]}}}
  - - {name: deploy, template: c}
- name: c
  container: {image: x, command: [run]}
- name: report
  inputs: {parameters: [{name: s}]}
  container: {image: x, command: [report, "{{inputs.parameters.s}}"]}
`

func TestStepExitHook(t *testing.T) {
	tests := []struct {
		name     string
		outcomes map[string]v1alpha1.NodePhase
		phase    v1alpha1.WorkflowPhase
		status   string // the argument of the hook
		deploy   bool
	}{
		{"the hook runs before the next step", nil, v1alpha1.WorkflowSucceeded, "Succeeded", true},
		{"a failed step still runs its hook", map[string]v1alpha1.NodePhase{"wf[0].build": v1alpha1.NodeFailed}, v1alpha1.WorkflowFailed, "Failed", false},
		{"a failed hook does not count", map[string]v1alpha1.NodePhase{"wf[0].build.onExit": v1alpha1.NodeFailed}, v1alpha1.WorkflowSucceeded, "Succeeded", true},
		{"a hook error fails the steps", map[string]v1alpha1.NodePhase{"wf[0].build.onExit": v1alpha1.NodeError}, v1alpha1.WorkflowError, "Succeeded", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSim(t, hookSpec, tt.outcomes)
			st := s.run()
			hook, ok := s.pods["wf[0].build.onExit"]
			if !ok || hook.Template.Container.Command[1] != tt.status {
				t.Fatalf("hook %+v", hook)
			}
			if d := nodeByName(st, "wf[0].build.onExit").DisplayName; d != "build.onExit" {
				t.Errorf("display name %q", d)
			}
			_, deployed := s.pods["wf[1].deploy"]
			if st.Phase != tt.phase || deployed != tt.deploy {
				t.Errorf("phase %s (%s), deploy ran %v", st.Phase, st.Message, deployed)
			}
			if deployed && s.startStep["wf[1].deploy"] <= s.startStep["wf[0].build.onExit"] {
				t.Errorf("deploy started at step %d, before the hook ended (started at %d)", s.startStep["wf[1].deploy"], s.startStep["wf[0].build.onExit"])
			}
			if tt.phase == v1alpha1.WorkflowError && !strings.Contains(st.Message, `the exit hook of task "build" failed`) {
				t.Errorf("message %q", st.Message)
			}
		})
	}
}
