package server

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

func TestApproveChecks(t *testing.T) {
	srv := setup(t)
	ctx := context.Background()
	wf := &v1alpha1.Workflow{Name: "gated", Namespace: "team-a", Spec: v1alpha1.WorkflowSpec{WorkflowTemplateSpec: v1alpha1.WorkflowTemplateSpec{
		Entrypoint: "gate", Templates: []v1alpha1.Template{{Name: "gate", Suspend: &v1alpha1.SuspendTemplate{},
			Outputs: v1alpha1.Outputs{Parameters: []v1alpha1.Parameter{{Name: "env", Default: new("staging"), Enum: []string{"staging", "prod"}}}}}},
	}}}
	if err := admin.Create(ctx, wf); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Delete(ctx, wf) })
	wf.Status.Nodes = map[string]v1alpha1.NodeStatus{"gated-1": {ID: "gated-1", Name: "gated", Type: v1alpha1.NodeSuspend, TemplateName: "gate", Phase: v1alpha1.NodeRunning}}
	if err := admin.Status().Update(ctx, wf); err != nil {
		t.Fatal(err)
	}
	approve := func(node, env string) error {
		_, err := newClient(srv, "alice").ApproveNode(ctx, connect.NewRequest(&chroniclev1.ApproveNodeRequest{
			Namespace: "team-a", Name: "gated", Node: node, Approve: true, Parameters: map[string]string{"env": env}}))
		return err
	}
	// An approval of an unknown node would block the approval of the real node.
	if err := approve("gated.typo", "prod"); code(err) != connect.CodeFailedPrecondition {
		t.Errorf("an unknown node: %v", err)
	}
	if err := approve("gated", "hacked"); code(err) != connect.CodeInvalidArgument {
		t.Errorf("a value outside the enum: %v", err)
	}
	if err := approve("gated-1", "prod"); err != nil {
		t.Errorf("an allowed value: %v", err)
	}
}
