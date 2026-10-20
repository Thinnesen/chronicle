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

// TestHTTPTemplate checks that an http template runs as a Pod with its placeholders replaced, and that its result
// reaches a later step.
func TestHTTPTemplate(t *testing.T) {
	s := newSim(t, `
entrypoint: main
arguments: {parameters: [{name: env, value: prod}]}
templates:
- name: main
  steps:
  - - {name: notify, template: notify}
  - - {name: show, template: show, arguments: {parameters: [{name: x, value: "{{steps.notify.outputs.result}}"}]}}
- name: notify
  http:
    url: "https://chat.example.com/{{workflow.parameters.env}}"
    method: POST
    headers: [{name: X-Run, value: "{{workflow.name}}"}, {name: Authorization, valueFrom: {secretKeyRef: {name: chat, key: token}}}]
    body: '{"text": "{{workflow.name}} is done"}'
- name: show
  inputs: {parameters: [{name: x}]}
  container: {image: x, command: [echo, "{{inputs.parameters.x}}"]}
`, nil)
	s.outputs["wf[0].notify"] = &v1alpha1.Outputs{Result: new("sent")}
	if st := s.run(); st.Phase != v1alpha1.WorkflowSucceeded {
		t.Fatalf("phase %s: %s", st.Phase, st.Message)
	}
	h := s.pods["wf[0].notify"].Template.HTTP
	if h == nil || h.URL != "https://chat.example.com/prod" || h.Body != `{"text": "wf is done"}` || *h.Headers[0].Value != "wf" || h.Headers[1].ValueFrom == nil {
		t.Errorf("http = %+v", h)
	}
	if cmd := s.pods["wf[1].show"].Template.Container.Command; cmd[1] != "sent" {
		t.Errorf("show = %v", cmd)
	}
}
