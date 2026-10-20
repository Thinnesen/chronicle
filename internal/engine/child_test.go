package engine

import (
	"strings"
	"testing"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

const childSpec = `
entrypoint: main
arguments: {parameters: [{name: day, value: "2026-10-20"}]}
templates:
- name: main
  dag:
    tasks:
    - {name: a, template: etl}
    - {name: b, template: c, depends: a, arguments: {parameters: [{name: x, value: "{{tasks.a.outputs.parameters.rows}}"}]}}
- name: etl
  outputs: {parameters: [{name: rows}]}
  childWorkflow:
    workflowTemplateRef: {name: etl}
    arguments: {parameters: [{name: day, value: "{{workflow.parameters.day}}"}]}
- name: c
  inputs: {parameters: [{name: x}]}
  container: {image: x, command: [echo, "{{inputs.parameters.x}}"]}
`

func childInput(t *testing.T) Input {
	t.Helper()
	in := taskInputFor(t, childSpec)
	in.Children = map[string]ChildResult{}
	return in
}

func rows(v string) *v1alpha1.Outputs {
	return &v1alpha1.Outputs{Parameters: []v1alpha1.Parameter{{Name: "rows", Value: &v}}}
}

// TestChildWorkflow covers the life of a child run: the create, the phase while it runs and its outputs.
func TestChildWorkflow(t *testing.T) {
	in := childInput(t)
	created, r := next[CreateWorkflow](&in, 0)
	if len(created) != 1 || created[0].TemplateRef.Name != "etl" || len(created[0].Parameters) != 1 ||
		*created[0].Parameters[0].Value != "2026-10-20" || !strings.HasPrefix(created[0].Name, wfName+"-") || r.RequeueAfter == 0 {
		t.Fatalf("create = %+v, requeue %s", created, r.RequeueAfter)
	}
	name := created[0].Name
	// The cache can miss a new child run: the engine asks again, and the create is idempotent.
	if again, _ := next[CreateWorkflow](&in, 1); len(again) != 1 || again[0].Name != name {
		t.Fatalf("create again = %+v", again)
	}
	in.Children[name] = ChildResult{Phase: v1alpha1.WorkflowRunning}
	next[CreateWorkflow](&in, 2)
	if n := nodeByName(in.Status, wfName+".a"); n.Phase != v1alpha1.NodeRunning || n.ChildWorkflow != name {
		t.Fatalf("node a = %+v", n)
	}
	done := at(3)
	in.Children[name] = ChildResult{Phase: v1alpha1.WorkflowSucceeded, FinishedAt: &done, Outputs: rows("5")}
	pods, _ := next[CreatePod](&in, 4)
	if len(pods) != 1 || pods[0].Template.Container.Command[1] != "5" {
		t.Fatalf("pods = %+v", pods)
	}
}

func TestChildWorkflowEnds(t *testing.T) {
	tests := []struct {
		name  string
		child ChildResult
		// shutdown is the shutdown of the parent run, or empty.
		shutdown v1alpha1.ShutdownMode
		delete   bool
		phase    v1alpha1.NodePhase
		message  string
		stop     v1alpha1.ShutdownMode
	}{
		{name: "failed", child: ChildResult{Phase: v1alpha1.WorkflowFailed, Message: "load failed"}, phase: v1alpha1.NodeFailed, message: "load failed"},
		{name: "error", child: ChildResult{Phase: v1alpha1.WorkflowError, Message: "no template"}, phase: v1alpha1.NodeError, message: "no template"},
		{name: "no output", child: ChildResult{Phase: v1alpha1.WorkflowSucceeded}, phase: v1alpha1.NodeFailed, message: `no output parameter "rows"`},
		{name: "stop", child: ChildResult{Phase: v1alpha1.WorkflowRunning}, shutdown: v1alpha1.ShutdownStop, phase: v1alpha1.NodeFailed, stop: v1alpha1.ShutdownStop},
		{name: "terminate", child: ChildResult{Phase: v1alpha1.WorkflowRunning}, shutdown: v1alpha1.ShutdownTerminate, phase: v1alpha1.NodeFailed, stop: v1alpha1.ShutdownTerminate},
		{name: "deleted", delete: true, phase: v1alpha1.NodeError, message: "was deleted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := childInput(t)
			created, _ := next[CreateWorkflow](&in, 0)
			name := created[0].Name
			in.Children[name] = ChildResult{Phase: v1alpha1.WorkflowRunning}
			next[CreateWorkflow](&in, 1)
			if tt.delete {
				delete(in.Children, name)
			} else {
				in.Children[name] = tt.child
			}
			in.Shutdown = tt.shutdown
			stops, _ := next[ShutdownWorkflow](&in, 2)
			n := nodeByName(in.Status, wfName+".a")
			if n.Phase != tt.phase || !strings.Contains(n.Message, tt.message) {
				t.Errorf("node = %s %q, want %s %q", n.Phase, n.Message, tt.phase, tt.message)
			}
			if tt.stop != "" && (len(stops) != 1 || stops[0].Name != name || stops[0].Mode != tt.stop) || tt.stop == "" && len(stops) != 0 {
				t.Errorf("shutdowns = %+v, want %s", stops, tt.stop)
			}
		})
	}
}

// TestChildWorkflowDepth stops a template that starts itself as a child run.
func TestChildWorkflowDepth(t *testing.T) {
	in := childInput(t)
	in.Depth = MaxChildDepth
	if created, _ := next[CreateWorkflow](&in, 0); len(created) != 0 {
		t.Fatalf("create = %+v", created)
	}
	if n := nodeByName(in.Status, wfName+".a"); n.Phase != v1alpha1.NodeError || !strings.Contains(n.Message, "nested") {
		t.Errorf("node = %s %q", n.Phase, n.Message)
	}
}
