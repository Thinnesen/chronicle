package server

import (
	"context"
	"errors"
	"maps"
	"slices"

	"connectrpc.com/connect"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

func (s *workflowService) SubmitWorkflow(ctx context.Context, req *connect.Request[chroniclev1.SubmitWorkflowRequest]) (*connect.Response[chroniclev1.SubmitWorkflowResponse], error) {
	m := req.Msg
	c, err := s.client(ctx, m.Namespace)
	if err != nil {
		return nil, err
	}
	wf, err := newSubmission(m)
	if err != nil {
		return nil, err
	}
	if err := c.Create(ctx, wf); err != nil {
		return nil, toConnect(err)
	}
	return connect.NewResponse(&chroniclev1.SubmitWorkflowResponse{Summary: summary(wf)}), nil
}

// newSubmission builds the Workflow of a submit request.
func newSubmission(m *chroniclev1.SubmitWorkflowRequest) (*v1alpha1.Workflow, error) {
	var wf *v1alpha1.Workflow
	switch src := m.Source.(type) {
	case *chroniclev1.SubmitWorkflowRequest_Workflow:
		var err error
		if wf, err = fromStruct(src.Workflow); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		// The status belongs to the controller.
		wf.Status = v1alpha1.WorkflowStatus{}
		wf.ResourceVersion, wf.UID = "", ""
	case *chroniclev1.SubmitWorkflowRequest_Template:
		t := src.Template
		if t.Name == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("give a template name"))
		}
		wf = &v1alpha1.Workflow{GenerateName: cmpOr(t.GenerateName, t.Name+"-")}
		wf.Spec.WorkflowTemplateRef = &v1alpha1.WorkflowTemplateRef{Name: t.Name, ClusterScope: t.ClusterScope}
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("give a workflow or a template"))
	}
	wf.Namespace = m.Namespace
	if wf.Name == "" && wf.GenerateName == "" {
		wf.GenerateName = "workflow-"
	}
	if len(m.Labels) > 0 && wf.Labels == nil {
		wf.Labels = map[string]string{}
	}
	maps.Copy(wf.Labels, m.Labels)
	setParameters(&wf.Spec.Arguments, m.Parameters)
	return wf, nil
}

// setParameters sets workflow parameters by name, in a stable order.
func setParameters(args *v1alpha1.Arguments, values map[string]string) {
	for _, name := range slices.Sorted(maps.Keys(values)) {
		v := values[name]
		i := slices.IndexFunc(args.Parameters, func(p v1alpha1.Parameter) bool { return p.Name == name })
		if i < 0 {
			args.Parameters = append(args.Parameters, v1alpha1.Parameter{Name: name, Value: &v})
			continue
		}
		args.Parameters[i].Value = &v
	}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
