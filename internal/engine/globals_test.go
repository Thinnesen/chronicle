package engine

import (
	"testing"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// TestGlobalOutputs checks that an output parameter with a globalName becomes a workflow output, which a later step
// and the exit handler can read, as in Argo.
func TestGlobalOutputs(t *testing.T) {
	s := newSim(t, `
entrypoint: main
onExit: report
templates:
- name: main
  steps:
  - - {name: build, template: build}
  - - {name: use, template: echo, arguments: {parameters: [{name: v, value: "{{workflow.outputs.parameters.version}}"}]}}
- name: build
  outputs: {parameters: [{name: tag, globalName: version, valueFrom: {path: /tmp/tag}}]}
  container: {image: x, command: [build]}
- name: echo
  inputs: {parameters: [{name: v}]}
  container: {image: x, command: [echo, "{{inputs.parameters.v}}"]}
- name: report
  container: {image: x, command: [report, "{{workflow.outputs.parameters.version}}"]}
`, nil)
	tag := "1.4.2"
	s.outputs["wf[0].build"] = &v1alpha1.Outputs{Parameters: []v1alpha1.Parameter{{Name: "tag", Value: &tag}}}
	st := s.run()
	if st.Phase != v1alpha1.WorkflowSucceeded {
		t.Fatalf("phase %s: %s", st.Phase, st.Message)
	}
	if cmd := s.pods["wf[1].use"].Template.Container.Command; cmd[1] != "1.4.2" {
		t.Errorf("use: %v", cmd)
	}
	if st.Outputs == nil || len(st.Outputs.Parameters) != 1 || st.Outputs.Parameters[0].Name != "version" || *st.Outputs.Parameters[0].Value != "1.4.2" {
		t.Errorf("status outputs = %+v", st.Outputs)
	}
	for _, p := range s.pods {
		if c := p.Template.Container.Command; c[0] == "report" && c[1] != "1.4.2" {
			t.Errorf("report: %v", c)
		}
	}
}

// TestGlobalArtifact checks that an output artifact with a globalName can be the from of a later argument.
func TestGlobalArtifact(t *testing.T) {
	s := newSim(t, `
entrypoint: main
templates:
- name: main
  steps:
  - - {name: make, template: make}
  - - {name: use, template: use, arguments: {artifacts: [{name: in, from: "{{workflow.outputs.artifacts.data}}"}]}}
- name: make
  outputs: {artifacts: [{name: out, globalName: data, path: /tmp/out}]}
  container: {image: x, command: [make]}
- name: use
  inputs: {artifacts: [{name: in, path: /tmp/in}]}
  container: {image: x, command: [use]}
`, nil)
	s.outputs["wf[0].make"] = &v1alpha1.Outputs{Artifacts: []v1alpha1.Artifact{{Name: "out", Key: "ns/uid/make/out.tgz"}}}
	if st := s.run(); st.Phase != v1alpha1.WorkflowSucceeded || st.Outputs == nil || len(st.Outputs.Artifacts) != 1 {
		t.Fatalf("phase %s (%s), outputs %+v", st.Phase, st.Message, st.Outputs)
	}
	if in := s.pods["wf[1].use"].Inputs; in == nil || in.Artifacts[0].Key != "ns/uid/make/out.tgz" {
		t.Errorf("inputs of use = %+v", in)
	}
}
