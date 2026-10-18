package engine

import (
	"strings"
	"testing"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

const approvalSpec = `
entrypoint: main
onExit: report
templates:
- name: main
  steps:
  - - {name: gate, template: gate}
  - - {name: deploy, template: echo, arguments: {parameters: [{name: x, value: "{{steps.gate.outputs.parameters.env}}"}]}}
- name: gate
  outputs: {parameters: [{name: env, default: staging}]}
  suspend: {}
- name: echo
  inputs: {parameters: [{name: x}]}
  container: {image: x, command: [echo, "{{inputs.parameters.x}}"]}
- name: report
  inputs: {parameters: [{name: s, value: "{{workflow.status}}"}]}
  container: {image: x, command: [echo, "{{inputs.parameters.s}}"]}
`

func TestEngineApproval(t *testing.T) {
	s := newSim(t, approvalSpec, nil)
	for i := range 3 {
		if r := s.step(i); r.Status.Phase != v1alpha1.WorkflowRunning || len(r.Actions) != 0 {
			t.Fatalf("step %d: phase %s, actions %v", i, r.Status.Phase, r.Actions)
		}
	}
	gate := nodeByName(s.in.Status, "wf[0].gate")
	if gate.Type != v1alpha1.NodeSuspend || gate.Message != "waiting for an approval" {
		t.Fatalf("gate = %+v", gate)
	}
	s.in.Approvals = []v1alpha1.Approval{{Node: gate.ID, Decision: v1alpha1.DecisionApprove,
		Parameters: []v1alpha1.Parameter{{Name: "env", Value: new("prod")}}, Message: "ok by ops"}}
	st := s.run()
	if st.Phase != v1alpha1.WorkflowSucceeded || nodeByName(st, "wf[0].gate").Message != "approved: ok by ops" {
		t.Fatalf("phase %s, gate %+v", st.Phase, nodeByName(st, "wf[0].gate"))
	}
	if cmd := s.pods["wf[1].deploy"].Template.Container.Command; cmd[1] != "prod" {
		t.Errorf("deploy command = %v", cmd)
	}
}

func TestEngineControls(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		setup   func(*sim)
		phase   v1alpha1.WorkflowPhase
		message string
		exit    string // the workflow status that the exit handler sees, or "" when it does not run
	}{
		{"reject", approvalSpec, func(s *sim) {
			s.in.Approvals = []v1alpha1.Approval{{Node: "wf[0].gate", Decision: v1alpha1.DecisionReject}}
		},
			v1alpha1.WorkflowFailed, `task "gate" failed`, "Failed"},
		{"duration", strings.Replace(approvalSpec, "suspend: {}", "suspend: {duration: 5s}", 1), func(*sim) {},
			v1alpha1.WorkflowSucceeded, "", "Succeeded"},
		{"stop runs the exit handler", approvalSpec, func(s *sim) { s.in.Shutdown = v1alpha1.ShutdownStop },
			v1alpha1.WorkflowFailed, "the workflow was stopped", "Failed"},
		{"terminate skips the exit handler", approvalSpec, func(s *sim) { s.in.Shutdown = v1alpha1.ShutdownTerminate },
			v1alpha1.WorkflowFailed, "the workflow was terminated", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSim(t, tt.spec, nil)
			s.step(0)
			tt.setup(s)
			st := s.run()
			if st.Phase != tt.phase || !strings.Contains(st.Message, tt.message) {
				t.Errorf("phase %s: %q", st.Phase, st.Message)
			}
			exit, ran := s.pods["wf.onExit"]
			if ran != (tt.exit != "") || ran && exit.Template.Container.Command[1] != tt.exit {
				t.Errorf("exit handler: ran %v, %+v", ran, exit.Template)
			}
		})
	}
}

func TestEngineSuspendAndStop(t *testing.T) {
	s := newSim(t, dagSpec("    - {name: a, template: c}\n    - {name: b, template: c, depends: a}"), map[string]v1alpha1.NodePhase{"wf.b": running})
	s.in.Suspend = true
	if r := s.step(0); r.Status.Phase != v1alpha1.WorkflowSuspended || len(r.Actions) != 0 {
		t.Fatalf("a suspended workflow started: %v", r.Actions)
	}
	s.in.Suspend = false
	s.step(1) // a starts
	s.in.Suspend = true
	s.step(2) // a ends, b must not start
	if _, ok := s.pods["wf.b"]; ok {
		t.Fatal("b started while the workflow was suspended")
	}
	s.in.Suspend = false
	s.step(3) // b starts and keeps running
	s.in.Shutdown = v1alpha1.ShutdownStop
	st := s.run()
	if st.Phase != v1alpha1.WorkflowFailed || len(s.deleted) != 1 || nodeByName(st, "wf.b").Message != "the workflow was stopped" {
		t.Errorf("phase %s, deleted %v, b %+v", st.Phase, s.deleted, nodeByName(st, "wf.b"))
	}
}
