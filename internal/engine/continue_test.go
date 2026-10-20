package engine

import (
	"strings"
	"testing"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

const continueSpec = `
entrypoint: main
templates:
- name: main
  steps:
  - - {name: flaky, template: c, continueOn: {failed: true}}
  - - {name: next, template: c}
- name: c
  container: {image: x}
`

func TestContinueOn(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		outcome v1alpha1.NodePhase
		phase   v1alpha1.WorkflowPhase
		next    bool // the next step runs
	}{
		{"a failure that continues", continueSpec, v1alpha1.NodeFailed, v1alpha1.WorkflowSucceeded, true},
		{"an error does not continue without error: true", continueSpec, v1alpha1.NodeError, v1alpha1.WorkflowError, false},
		{"without continueOn", strings.Replace(continueSpec, ", continueOn: {failed: true}", "", 1), v1alpha1.NodeFailed, v1alpha1.WorkflowFailed, false},
		{"a DAG task that continues", `
entrypoint: main
templates:
- name: main
  dag:
    tasks:
    - {name: flaky, template: c, continueOn: {failed: true}}
    - {name: next, template: c, depends: flaky}
- name: c
  container: {image: x}
`, v1alpha1.NodeFailed, v1alpha1.WorkflowSucceeded, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flaky := "wf[0].flaky"
			next := "wf[1].next"
			if strings.Contains(tt.spec, "dag:") {
				flaky, next = "wf.flaky", "wf.next"
			}
			s := newSim(t, tt.spec, map[string]v1alpha1.NodePhase{flaky: tt.outcome})
			st := s.run()
			_, ran := s.pods[next]
			if st.Phase != tt.phase || ran != tt.next {
				t.Errorf("phase %s (%s), next ran %v; want %s, %v", st.Phase, st.Message, ran, tt.phase, tt.next)
			}
		})
	}
}
