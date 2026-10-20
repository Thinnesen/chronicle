package engine

import (
	"strings"
	"testing"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

func TestCheckApproval(t *testing.T) {
	wf := &v1alpha1.Workflow{Name: "wf"}
	wf.Spec.Templates = []v1alpha1.Template{{Name: "gate", Suspend: &v1alpha1.SuspendTemplate{},
		Outputs: v1alpha1.Outputs{Parameters: []v1alpha1.Parameter{{Name: "env", Enum: []string{"staging", "prod"}}, {Name: "note"}}}}}
	wf.Status.Nodes = map[string]v1alpha1.NodeStatus{"g": {ID: "g", Name: "wf.gate", Type: v1alpha1.NodeSuspend, TemplateName: "gate", Phase: v1alpha1.NodeRunning}}
	param := func(name, value string) v1alpha1.Parameter { return v1alpha1.Parameter{Name: name, Value: &value} }
	tests := []struct {
		name   string
		params []v1alpha1.Parameter
		err    string
	}{
		{"allowed values", []v1alpha1.Parameter{param("env", "prod"), param("note", "ok")}, ""},
		{"a value outside the enum", []v1alpha1.Parameter{param("env", "dev")}, "is not one of"},
		// A typo must not approve with the defaults.
		{"an unknown parameter", []v1alpha1.Parameter{param("target", "prod")}, `the step has no output parameter "target"`},
	}
	for _, tt := range tests {
		err := CheckApproval(wf, v1alpha1.Approval{Node: "g", Decision: v1alpha1.DecisionApprove, Parameters: tt.params})
		if tt.err == "" && err != nil || tt.err != "" && (err == nil || !strings.Contains(err.Error(), tt.err)) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.err)
		}
	}
}
