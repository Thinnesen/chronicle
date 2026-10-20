package server

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

func TestRetryAndRerun(t *testing.T) {
	srv := setup(t)
	ctx := context.Background()
	wf := &v1alpha1.Workflow{Name: "nightly-x7k2p", Namespace: "team-a", Labels: map[string]string{"team": "a", "chronicle.thinnesen.com/cron-workflow": "nightly"},
		Spec: v1alpha1.WorkflowSpec{Suspend: true, Shutdown: v1alpha1.ShutdownStop,
			Approvals:  []v1alpha1.Approval{{Node: "g", Decision: v1alpha1.DecisionApprove}, {Node: "keep", Decision: v1alpha1.DecisionApprove}},
			Entrypoint: "main", Arguments: v1alpha1.Arguments{Parameters: []v1alpha1.Parameter{{Name: "day", Value: new("mon")}, {Name: "size", Value: new("s")}}},
			Templates: []v1alpha1.Template{
				{Name: "main", Steps: [][]v1alpha1.WorkflowStep{{{Name: "gate", Template: "approve"}}, {{Name: "b", Template: "c"}}}},
				{Name: "approve", Suspend: &v1alpha1.SuspendTemplate{}},
				{Name: "c", Container: &v1alpha1.ContainerTemplate{Image: "alpine"}},
			},
		}}
	if err := admin.Create(ctx, wf); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Delete(ctx, wf) })
	alice := newClient(srv, "alice")
	retry := func(node string) error {
		_, err := alice.RetryWorkflow(ctx, connect.NewRequest(&chroniclev1.RetryWorkflowRequest{Namespace: "team-a", Name: wf.Name, Node: node}))
		return err
	}
	if err := retry(""); code(err) != connect.CodeFailedPrecondition {
		t.Errorf("a retry of a running run: %v", err)
	}
	root := wf.Name
	wf.Status.Phase = v1alpha1.WorkflowFailed
	wf.Status.Nodes = map[string]v1alpha1.NodeStatus{
		root:   {ID: root, Name: root, Type: v1alpha1.NodeSteps, TemplateName: "main", Phase: v1alpha1.NodeFailed, Children: []string{"s0", "s1"}},
		"s0":   {ID: "s0", Name: root + "[0]", Type: v1alpha1.NodeStepGroup, Phase: v1alpha1.NodeSucceeded, Children: []string{"g"}},
		"g":    {ID: "g", Name: root + "[0].gate", Type: v1alpha1.NodeSuspend, TemplateName: "approve", Phase: v1alpha1.NodeSucceeded},
		"s1":   {ID: "s1", Name: root + "[1]", Type: v1alpha1.NodeStepGroup, Phase: v1alpha1.NodeFailed, Children: []string{"b"}},
		"b":    {ID: "b", Name: root + "[1].b", Type: v1alpha1.NodePod, TemplateName: "c", Phase: v1alpha1.NodeFailed},
		"keep": {ID: "keep", Name: "other", Type: v1alpha1.NodeSuspend, Phase: v1alpha1.NodeSucceeded},
	}
	if err := admin.Status().Update(ctx, wf); err != nil {
		t.Fatal(err)
	}
	if err := retry("no-such-node"); code(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "no node") {
		t.Errorf("a retry from a node that does not exist: %v", err)
	}
	for gen, node := range []string{"", "g"} {
		if err := retry(node); err != nil {
			t.Fatal(err)
		}
		if err := admin.Get(ctx, client.ObjectKeyFromObject(wf), wf); err != nil {
			t.Fatal(err)
		}
		if r := wf.Spec.Restart; r == nil || r.Generation != int32(gen+1) || r.FromNode != node {
			t.Errorf("restart = %+v", r)
		}
		// A retry runs again: it clears a stop and a suspend of the earlier try.
		if wf.Spec.Shutdown != "" || wf.Spec.Suspend {
			t.Errorf("shutdown %q, suspend %t after a retry", wf.Spec.Shutdown, wf.Spec.Suspend)
		}
	}
	// The retry from the gate runs the gate again, so its old approval must not pass it.
	if len(wf.Spec.Approvals) != 1 || wf.Spec.Approvals[0].Node != "keep" {
		t.Errorf("approvals after a retry from the gate = %+v", wf.Spec.Approvals)
	}

	if _, err := alice.RerunWorkflow(ctx, connect.NewRequest(&chroniclev1.RerunWorkflowRequest{Namespace: "team-a", Name: wf.Name,
		Parameters: map[string]string{"sise": "xl"}})); code(err) != connect.CodeInvalidArgument {
		t.Errorf("a re-run with an unknown parameter: %v", err)
	}
	resp, err := alice.RerunWorkflow(ctx, connect.NewRequest(&chroniclev1.RerunWorkflowRequest{Namespace: "team-a", Name: wf.Name, Parameters: map[string]string{"size": "xl"}}))
	if err != nil {
		t.Fatal(err)
	}
	var rerun v1alpha1.Workflow
	if err := admin.Get(ctx, client.ObjectKey{Namespace: "team-a", Name: resp.Msg.Summary.Name}, &rerun); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Delete(ctx, &rerun) })
	p := rerun.Spec.Arguments.Parameters
	if !strings.HasPrefix(rerun.Name, "nightly-") || rerun.Labels[LabelRerunOf] != wf.Name || rerun.Labels["team"] != "a" ||
		rerun.Labels["chronicle.thinnesen.com/cron-workflow"] != "" || rerun.Spec.Suspend || rerun.Spec.Restart != nil ||
		*p[0].Value != "mon" || *p[1].Value != "xl" {
		t.Errorf("re-run = %+v", rerun)
	}
}
