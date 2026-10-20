package engine

import (
	"testing"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// TestArtifactSources checks that raw and http artifacts reach the Pod: from the workflow arguments, from a step
// argument, passed on with from, and from the template itself with a URL placeholder. A retry gives the same artifacts again.
func TestArtifactSources(t *testing.T) {
	s := newSim(t, `
entrypoint: main
arguments:
  parameters: [{name: v, value: "1.2"}]
  artifacts: [{name: cfg, raw: {data: "a: 1"}}]
templates:
- name: main
  inputs: {artifacts: [{name: cfg, path: /tmp/cfg}]}
  steps: [[{name: s, template: use, arguments: {artifacts: [{name: note, raw: {data: hi}}, {name: cfg, from: "{{inputs.artifacts.cfg}}"}]}}]]
- name: use
  retryStrategy: {limit: 1}
  inputs:
    artifacts:
    - {name: note, path: /tmp/note}
    - {name: cfg, path: /tmp/c}
    - {name: tool, path: /tmp/tool, mode: 493, http: {url: "https://example.com/{{workflow.parameters.v}}/tool"}}
  container: {image: x, command: [run]}
`, map[string]v1alpha1.NodePhase{"wf[0].s(0)": v1alpha1.NodeFailed})
	if st := s.run(); st.Phase != v1alpha1.WorkflowSucceeded {
		t.Fatalf("phase %s: %s", st.Phase, st.Message)
	}
	main := s.in.Status.Nodes[s.in.Status.Nodes["wf"].ID]
	if in := main.Inputs; in == nil || len(in.Artifacts) != 1 || in.Artifacts[0].Raw == nil || in.Artifacts[0].Raw.Data != "a: 1" {
		t.Errorf("inputs of main = %+v", in)
	}
	for _, name := range []string{"wf[0].s(0)", "wf[0].s(1)"} {
		in := s.pods[name].Inputs
		if in == nil || len(in.Artifacts) != 3 {
			t.Fatalf("%s: inputs %+v", name, in)
		}
		note, cfg, tool := in.Artifacts[0], in.Artifacts[1], in.Artifacts[2]
		if cfg.Raw == nil || cfg.Raw.Data != "a: 1" || cfg.Path != "/tmp/c" {
			t.Errorf("%s: passed on cfg %+v", name, cfg)
		}
		if note.Raw == nil || note.Raw.Data != "hi" || note.Path != "/tmp/note" {
			t.Errorf("%s: note %+v", name, note)
		}
		if tool.HTTP == nil || tool.HTTP.URL != "https://example.com/1.2/tool" || tool.Mode == nil || *tool.Mode != 493 {
			t.Errorf("%s: tool %+v", name, tool)
		}
	}
}
