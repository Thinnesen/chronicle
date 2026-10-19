package server

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

func TestListNodeEvents(t *testing.T) {
	srv := setup(t)
	ctx := context.Background()
	wf := &v1alpha1.Workflow{Name: "events", Namespace: "team-a", Spec: v1alpha1.WorkflowSpec{WorkflowTemplateSpec: v1alpha1.WorkflowTemplateSpec{
		Entrypoint: "c", Templates: []v1alpha1.Template{{Name: "c", Container: &v1alpha1.ContainerTemplate{Image: "x"}}},
	}}}
	if err := admin.Create(ctx, wf); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Delete(ctx, wf) })
	wf.Status.Nodes = map[string]v1alpha1.NodeStatus{"events-1": {ID: "events-1", Name: "events", Type: v1alpha1.NodePod, PodName: "events-1"}}
	if err := admin.Status().Update(ctx, wf); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	for i, e := range []struct{ pod, reason string }{{"events-1", "Pulled"}, {"events-1", "FailedScheduling"}, {"other", "Pulled"}} {
		ev := &corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "ev-" + e.reason + "-" + e.pod, Namespace: "team-a"},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: e.pod, Namespace: "team-a"},
			Type:           corev1.EventTypeWarning, Reason: e.reason, Message: "msg", Count: 3,
			LastTimestamp: metav1.NewTime(t0.Add(time.Duration(i) * time.Minute)),
		}
		if err := admin.Create(ctx, ev); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = admin.Delete(ctx, ev) })
	}
	req := func(node string) *connect.Request[chroniclev1.ListNodeEventsRequest] {
		return connect.NewRequest(&chroniclev1.ListNodeEventsRequest{Namespace: "team-a", Workflow: "events", Node: node})
	}

	got, err := newClient(srv, "alice").ListNodeEvents(ctx, req("events-1"))
	if err != nil || len(got.Msg.Events) != 2 || got.Msg.Events[0].Reason != "Pulled" || got.Msg.Events[1].Reason != "FailedScheduling" || got.Msg.Events[1].Count != 3 {
		t.Fatalf("alice: %v %+v", err, got)
	}
	if _, err := newGroupClient(srv, "carol", "oidc:viewers").ListNodeEvents(ctx, req("events-1")); code(err) != connect.CodePermissionDenied {
		t.Errorf("a user without list events: %v", err)
	}
	if _, err := newClient(srv, "alice").ListNodeEvents(ctx, req("nope")); code(err) != connect.CodeNotFound {
		t.Errorf("an unknown node: %v", err)
	}
}
