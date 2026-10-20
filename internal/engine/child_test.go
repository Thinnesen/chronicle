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
		{name: "no output", child: ChildResult{Phase: v1alpha1.WorkflowSucceeded}, phase: v1alpha1.NodeFailed, message: `has no output parameter "rows"`},
		// A failed grandchild does not repeat its whole chain of messages in each parent.
		{name: "nested", child: ChildResult{Phase: v1alpha1.WorkflowFailed, Message: "the child run x-1 Failed: the child run x-2 Failed: boom"}, phase: v1alpha1.NodeFailed,
			message: "Failed; open it for the reason"},
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

// TestChildWorkflowTemplateName resolves a placeholder in the name of the template of the child run.
func TestChildWorkflowTemplateName(t *testing.T) {
	in := childInput(t)
	for i := range in.Spec.Templates {
		if c := in.Spec.Templates[i].ChildWorkflow; c != nil {
			c.WorkflowTemplateRef.Name = "etl-{{workflow.parameters.day}}"
		}
	}
	if created, _ := next[CreateWorkflow](&in, 0); len(created) != 1 || created[0].TemplateRef.Name != "etl-2026-10-20" {
		t.Fatalf("create = %+v", created)
	}
}

// TestChildWorkflowRetry retries the failed child run itself when the parent run is retried, not a new child run.
func TestChildWorkflowRetry(t *testing.T) {
	failed := func(t *testing.T) (Input, string) {
		in := childInput(t)
		created, _ := next[CreateWorkflow](&in, 0)
		name := created[0].Name
		in.Children[name] = ChildResult{Phase: v1alpha1.WorkflowFailed, Message: "load failed"}
		if _, r := next[CreateWorkflow](&in, 1); r.Status.Phase != v1alpha1.WorkflowFailed {
			t.Fatalf("phase = %s", r.Status.Phase)
		}
		st, _, err := Restart(in.Spec, in.Status, wfName, "")
		if err != nil {
			t.Fatal(err)
		}
		in.Status, in.Restarts = st, 1
		return in, name
	}

	t.Run("retried", func(t *testing.T) {
		in, name := failed(t)
		retries, r := next[RetryWorkflow](&in, 2)
		if len(retries) != 1 || retries[0].Name != name || retries[0].Generation != 1 || r.RequeueAfter == 0 {
			t.Fatalf("retries = %+v", retries)
		}
		if created, _ := next[CreateWorkflow](&in, 3); len(created) != 0 {
			t.Fatalf("a new child run: %+v", created)
		}
		// The child has the request, but its controller has not applied it yet.
		in.Children[name] = ChildResult{Phase: v1alpha1.WorkflowFailed, RestartGeneration: 1}
		if again, _ := next[RetryWorkflow](&in, 4); len(again) != 0 || nodeByName(in.Status, wfName+".a").Phase != v1alpha1.NodePending {
			t.Fatalf("retries again = %+v, node %+v", again, nodeByName(in.Status, wfName+".a"))
		}
		in.Children[name] = ChildResult{Phase: v1alpha1.WorkflowRunning, RestartGeneration: 1, Restarts: 1}
		next[CreateWorkflow](&in, 5)
		done := at(6)
		in.Children[name] = ChildResult{Phase: v1alpha1.WorkflowSucceeded, RestartGeneration: 1, Restarts: 1, FinishedAt: &done, Outputs: rows("7")}
		if pods, _ := next[CreatePod](&in, 7); len(pods) != 1 || pods[0].Template.Container.Command[1] != "7" {
			t.Fatalf("pods = %+v", pods)
		}
	})

	t.Run("refused", func(t *testing.T) {
		in, name := failed(t)
		next[RetryWorkflow](&in, 2)
		in.Children[name] = ChildResult{Phase: v1alpha1.WorkflowFailed, RestartGeneration: 1, Restarts: 1, RestartRefused: "no node failed"}
		next[RetryWorkflow](&in, 3)
		if n := nodeByName(in.Status, wfName+".a"); n.Phase != v1alpha1.NodeError || !strings.Contains(n.Message, "refused the retry: no node failed") {
			t.Errorf("node = %s %q", n.Phase, n.Message)
		}
	})
}
