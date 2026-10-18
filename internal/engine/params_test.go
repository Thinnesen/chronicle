package engine

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

func TestEngineParametersAndOutputs(t *testing.T) {
	s := newSim(t, `
entrypoint: main
arguments: {parameters: [{name: who, value: world}]}
templates:
- name: main
  inputs: {parameters: [{name: who}]}
  outputs: {parameters: [{name: final, value: "{{tasks.b.outputs.parameters.p}}"}]}
  dag:
    tasks:
    - {name: a, template: echo, arguments: {parameters: [{name: msg, value: "hi {{inputs.parameters.who}} in {{workflow.namespace}}"}]}}
    - {name: b, template: echo, depends: a, arguments: {parameters: [{name: msg, value: "{{tasks.a.outputs.result}}"}]}}
    - {name: skip, template: echo, depends: a, when: "{{tasks.a.outputs.result}} == nothing", arguments: {parameters: [{name: msg, value: x}]}}
- name: echo
  inputs: {parameters: [{name: msg}, {name: color, default: blue}]}
  outputs: {parameters: [{name: p, valueFrom: {path: /tmp/p}}]}
  container: {image: "alpine:{{inputs.parameters.color}}", command: [echo], args: ["{{inputs.parameters.msg}}"], env: [{name: WF, value: "{{workflow.name}}"}]}
`, nil)
	s.outputs["wf.a"] = &v1alpha1.Outputs{Result: new("A-OUT")}
	s.outputs["wf.b"] = &v1alpha1.Outputs{Parameters: []v1alpha1.Parameter{{Name: "p", Value: new("pb")}}}
	st := s.run()
	if st.Phase != v1alpha1.WorkflowSucceeded {
		t.Fatalf("phase = %s: %s", st.Phase, st.Message)
	}
	a := s.pods["wf.a"].Template.Container
	if !reflect.DeepEqual(a.Args, []string{"hi world in ns"}) || a.Image != "alpine:blue" || a.Env[0].Value != wfName {
		t.Errorf("Pod a = %+v", a)
	}
	if got := s.pods["wf.b"].Template.Container.Args; !reflect.DeepEqual(got, []string{"A-OUT"}) {
		t.Errorf("Pod b args = %v", got)
	}
	if n := nodeByName(st, "wf.b"); len(n.Inputs.Parameters) != 2 || *n.Inputs.Parameters[0].Value != "A-OUT" {
		t.Errorf("node b inputs = %+v", n.Inputs)
	}
	if n := nodeByName(st, "wf.skip"); n.Phase != v1alpha1.NodeSkipped {
		t.Errorf("node skip = %s", n.Phase)
	}
	if root := nodeByName(st, "wf"); root.Outputs == nil || *root.Outputs.Parameters[0].Value != "pb" {
		t.Errorf("root outputs = %+v", root.Outputs)
	}
}

func TestEngineFanOut(t *testing.T) {
	s := newSim(t, dagSpec(`
    - {name: gen, template: c}
    - {name: each, template: p, depends: gen, withParam: "{{tasks.gen.outputs.result}}", when: "{{item}} != y", arguments: {parameters: [{name: x, value: "{{item}}"}]}}
    - {name: lit, template: p, withItems: [{k: a}, {k: b}], arguments: {parameters: [{name: x, value: "{{item.k}}"}]}}
    - {name: anyok, template: c, depends: lit.AnySucceeded}
    - {name: allfail, template: c, depends: lit.AllFailed}
    - {name: none, template: c, depends: gen, withParam: "[]"}
    - {name: agg, template: p, depends: each && none, arguments: {parameters: [{name: x, value: "{{tasks.each.outputs.result}}"}]}}`)+`
- {name: p, inputs: {parameters: [{name: x}]}, container: {image: alpine, command: [run], args: ["{{inputs.parameters.x}}"]}}`,
		map[string]v1alpha1.NodePhase{`wf.lit(1:{"k":"b"})`: failed})
	s.outputs["wf.gen"] = &v1alpha1.Outputs{Result: new(`["x", "y", "z"]`)}
	st := s.run()
	if st.Phase != v1alpha1.WorkflowSucceeded {
		t.Fatalf("phase = %s: %s", st.Phase, st.Message)
	}
	want := map[string]v1alpha1.NodePhase{
		"wf.each": succeeded, "wf.each(0:x)": succeeded, "wf.each(1:y)": v1alpha1.NodeSkipped, "wf.each(2:z)": succeeded,
		"wf.lit": failed, "wf.anyok": succeeded, "wf.allfail": omitted, "wf.none": v1alpha1.NodeSkipped, "wf.agg": succeeded,
	}
	got := phases(st)
	for name, p := range want {
		if got[name] != p {
			t.Errorf("node %s = %q, want %q", name, got[name], p)
		}
	}
	if g := nodeByName(st, "wf.each"); g.Type != v1alpha1.NodeTaskGroup || len(g.Children) != 3 {
		t.Errorf("group = %+v", g)
	}
	if args := s.pods[`wf.lit(0:{"k":"a"})`].Template.Container.Args; args[0] != "a" {
		t.Errorf("item field = %v", args)
	}
	if args := s.pods["wf.agg"].Template.Container.Args; args[0] != `["out-each(0:x)","out-each(2:z)"]` {
		t.Errorf("aggregated outputs = %v", args)
	}
}

func TestEngineRuntimeErrors(t *testing.T) {
	const p = "\n- {name: p, inputs: {parameters: [{name: x, enum: [a, b]}]}, container: {image: alpine}}"
	tests := []struct {
		name, task, want string
	}{
		{"withParam is not a list", `{name: t, template: c, depends: gen, withParam: "{{tasks.gen.outputs.result}}"}`, "withParam is not a JSON list"},
		{"value not in enum", `{name: t, template: p, depends: gen, arguments: {parameters: [{name: x, value: "{{tasks.gen.outputs.result}}"}]}}`, `is not one of`},
		{"when is not a condition", `{name: t, template: c, depends: gen, when: "{{tasks.gen.outputs.result}}"}`, "is not true or false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := dagSpec("    - {name: gen, template: c, outputs: {}}\n    - "+tt.task) + p
			spec = strings.Replace(spec, ", outputs: {}", "", 1)
			s := newSim(t, spec, nil)
			s.outputs["wf.gen"] = &v1alpha1.Outputs{Result: new("not-json")}
			st := s.run()
			if st.Phase != v1alpha1.WorkflowError || !strings.Contains(nodeByName(st, "wf.t").Message, tt.want) {
				t.Errorf("phase %s, node t = %+v", st.Phase, nodeByName(st, "wf.t"))
			}
		})
	}
}
