package server

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

func TestSubmitStartAt(t *testing.T) {
	srv := setup(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	spec, _ := structpb.NewStruct(map[string]any{
		"metadata": map[string]any{"generateName": "later-"},
		"spec":     map[string]any{"entrypoint": "gate", "templates": []any{map[string]any{"name": "gate", "suspend": map[string]any{}}}},
	})
	sub, err := newClient(srv, "alice").SubmitWorkflow(ctx, connect.NewRequest(&chroniclev1.SubmitWorkflowRequest{
		Namespace: "team-a", Source: &chroniclev1.SubmitWorkflowRequest_Workflow{Workflow: spec}, StartAt: timestamppb.New(at),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !sub.Msg.Summary.StartAt.AsTime().Equal(at) {
		t.Errorf("summary start time = %v", sub.Msg.Summary.StartAt)
	}
	var wf v1alpha1.Workflow
	if err := admin.Get(ctx, client.ObjectKey{Namespace: "team-a", Name: sub.Msg.Summary.Name}, &wf); err != nil {
		t.Fatal(err)
	}
	if wf.Spec.StartAt == nil || !wf.Spec.StartAt.Time.Equal(at) {
		t.Errorf("start time = %v", wf.Spec.StartAt)
	}
}
