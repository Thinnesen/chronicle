package server

import (
	"bytes"
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

// summary returns the short form of a run.
func summary(wf *v1alpha1.Workflow) *chroniclev1.WorkflowSummary {
	s := &chroniclev1.WorkflowSummary{
		Namespace:  wf.Namespace,
		Name:       wf.Name,
		Uid:        string(wf.UID),
		Phase:      string(wf.Status.Phase),
		Message:    wf.Status.Message,
		CreatedAt:  timestamp(&wf.CreationTimestamp),
		StartedAt:  timestamp(wf.Status.StartedAt),
		FinishedAt: timestamp(wf.Status.FinishedAt),
		Labels:     wf.Labels,
	}
	if ref := wf.Spec.WorkflowTemplateRef; ref != nil {
		s.Template = ref.Name
	}
	s.NodesDone, s.NodesTotal = wf.Status.Progress()
	for _, n := range wf.Status.Nodes {
		if n.Type == v1alpha1.NodeSuspend && n.Phase == v1alpha1.NodeRunning {
			s.ApprovalsWaiting++
		}
	}
	return s
}

func timestamp(t *metav1.Time) *timestamppb.Timestamp {
	if t == nil || t.IsZero() {
		return nil
	}
	return timestamppb.New(t.Time)
}

// toStruct converts an object to a protobuf Struct through JSON.
func toStruct(obj any) (*structpb.Struct, error) {
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("encode the object: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("decode the object: %w", err)
	}
	return structpb.NewStruct(m)
}

// fromStruct converts a protobuf Struct to a Workflow. Unknown fields are errors.
func fromStruct(s *structpb.Struct) (*v1alpha1.Workflow, error) {
	data, err := s.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("encode the workflow: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var wf v1alpha1.Workflow
	if err := dec.Decode(&wf); err != nil {
		return nil, fmt.Errorf("the workflow is not valid: %w", err)
	}
	return &wf, nil
}
