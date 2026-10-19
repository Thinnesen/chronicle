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
		Spec: v1alpha1.WorkflowSpec{Suspend: true, WorkflowTemplateSpec: v1alpha1.WorkflowTemplateSpec{
			Entrypoint: "c", Arguments: v1alpha1.Arguments{Parameters: []v1alpha1.Parameter{{Name: "day", Value: new("mon")}, {Name: "size", Value: new("s")}}},
			Templates: []v1alpha1.Template{{Name: "c", Container: &v1alpha1.ContainerTemplate{Image: "alpine"}}},
		}}}
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
	wf.Status.Phase = v1alpha1.WorkflowFailed
	if err := admin.Status().Update(ctx, wf); err != nil {
		t.Fatal(err)
	}
	for gen, node := range []string{"", "nightly-x7k2p.b"} {
		if err := retry(node); err != nil {
			t.Fatal(err)
		}
		if err := admin.Get(ctx, client.ObjectKeyFromObject(wf), wf); err != nil {
			t.Fatal(err)
		}
		if r := wf.Spec.Restart; r == nil || r.Generation != int32(gen+1) || r.FromNode != node {
			t.Errorf("restart = %+v", r)
		}
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
