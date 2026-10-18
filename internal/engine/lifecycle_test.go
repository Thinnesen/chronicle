package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

const running = v1alpha1.NodeRunning

func TestEngineRetries(t *testing.T) {
	tests := []struct {
		name     string
		strategy string
		outcomes map[string]v1alpha1.NodePhase
		phase    v1alpha1.WorkflowPhase
		attempts int
		message  string
	}{
		{"succeeds on the third attempt", "{limit: 2}", map[string]v1alpha1.NodePhase{"wf(0)": failed, "wf(1)": failed}, v1alpha1.WorkflowSucceeded, 3, ""},
		{"limit reached", "{limit: 1}", map[string]v1alpha1.NodePhase{"wf(0)": failed, "wf(1)": failed}, v1alpha1.WorkflowFailed, 2, "(after 2 attempts)"},
		{"OnError does not retry a failure", "{limit: 3, retryPolicy: OnError}", map[string]v1alpha1.NodePhase{"wf(0)": failed}, v1alpha1.WorkflowFailed, 1, "exit"},
		{"Always retries an error", "{limit: 3, retryPolicy: Always}", map[string]v1alpha1.NodePhase{"wf(0)": errored}, v1alpha1.WorkflowSucceeded, 2, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSim(t, "entrypoint: c\ntemplates:\n- {name: c, retryStrategy: "+tt.strategy+", container: {image: x, command: [run]}}", tt.outcomes)
			for k := range tt.outcomes {
				s.outcomes[k] = tt.outcomes[k]
			}
			st := s.run()
			root := nodeByName(st, "wf")
			if st.Phase != tt.phase || root.Type != v1alpha1.NodeRetry || len(root.Children) != tt.attempts {
				t.Errorf("phase %s, root %+v", st.Phase, root)
			}
			if tt.message != "exit" && !strings.Contains(st.Message, tt.message) {
				t.Errorf("message = %q", st.Message)
			}
		})
	}
}

func TestEngineBackoff(t *testing.T) {
	s := newSim(t, "entrypoint: c\ntemplates:\n- {name: c, retryStrategy: {limit: 3, backoff: {duration: 10s, factor: 2, maxDuration: 15s}}, container: {image: x, command: [run]}}",
		map[string]v1alpha1.NodePhase{"wf(0)": failed, "wf(1)": failed})
	s.step(0)
	if r := s.step(1); r.RequeueAfter != 10*time.Second {
		t.Errorf("RequeueAfter = %s, want 10s", r.RequeueAfter)
	}
	s.run()
	// Attempt 0 fails at second 1. The waits are 10s, then 20s capped at 15s.
	if s.startStep["wf(1)"] != 11 || s.startStep["wf(2)"] != 27 {
		t.Errorf("attempt starts = %v", s.startStep)
	}
}

func TestEngineTimeoutAndDeadline(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		s := newSim(t, dagSpec("    - {name: slow, template: slow}\n    - {name: next, template: c, depends: slow.Failed}")+
			"\n- {name: slow, timeout: 5s, container: {image: x, command: [run]}}", map[string]v1alpha1.NodePhase{"wf.slow": running})
		st := s.run()
		slow := nodeByName(st, "wf.slow")
		if st.Phase != v1alpha1.WorkflowSucceeded || slow.Phase != failed || !strings.Contains(slow.Message, "timeout of 5s") {
			t.Errorf("phase %s, slow %+v", st.Phase, slow)
		}
		if len(s.deleted) != 1 || s.deleted[0] != slow.PodName || s.startStep["wf.next"] != 5 {
			t.Errorf("deleted %v, next started at %d", s.deleted, s.startStep["wf.next"])
		}
	})
	t.Run("deadline with exit handler", func(t *testing.T) {
		s := newSim(t, "entrypoint: main\nactiveDeadlineSeconds: 3\nonExit: report\ntemplates:\n- {name: main, steps: [[{name: a, template: c}]]}"+pod+
			"\n- {name: report, inputs: {parameters: [{name: s, value: '{{workflow.status}}'}]}, container: {image: x, command: [echo, '{{inputs.parameters.s}}']}}",
			map[string]v1alpha1.NodePhase{"wf[0].a": running})
		st := s.run()
		if st.Phase != v1alpha1.WorkflowFailed || !strings.Contains(st.Message, "deadline") {
			t.Errorf("phase %s: %s", st.Phase, st.Message)
		}
		if args := s.pods["wf.onExit"].Template.Container.Command; len(args) != 2 || args[1] != "Failed" {
			t.Errorf("exit handler command = %v", args)
		}
	})
	t.Run("a failed exit handler fails the workflow", func(t *testing.T) {
		s := newSim(t, "entrypoint: c\nonExit: c\ntemplates:"+pod, map[string]v1alpha1.NodePhase{"wf.onExit": failed})
		if st := s.run(); st.Phase != v1alpha1.WorkflowFailed || !strings.Contains(st.Message, "the exit handler failed") {
			t.Errorf("phase %s: %s", st.Phase, st.Message)
		}
	})
}
