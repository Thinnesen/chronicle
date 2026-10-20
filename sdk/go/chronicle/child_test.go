package chronicle

import (
	"testing"
)

func TestChildWorkflow(t *testing.T) {
	load := ChildWorkflow("load", "etl").ClusterScope().OutputField("rows")
	load.ChildArg("day", load.Input("day"))
	main := DAG("main")
	main.Task("load", load).Arg("day", "2026-10-20")
	spec, err := New("parent-").Entrypoint(main).Spec()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tmpl := range spec.Templates {
		if c := tmpl.ChildWorkflow; tmpl.Name == "load" && c != nil {
			found = c.WorkflowTemplateRef.Name == "etl" && c.WorkflowTemplateRef.ClusterScope &&
				*c.Arguments.Parameters[0].Value == "{{inputs.parameters.day}}" && tmpl.Outputs.Parameters[0].Name == "rows"
		}
	}
	if !found {
		t.Errorf("no child template in %+v", spec.Templates)
	}
	if _, err := New("x-").Entrypoint(Container("c", "alpine").ChildArg("a", "b")).Spec(); err == nil {
		t.Error("ChildArg on a container template gave no error")
	}
}
