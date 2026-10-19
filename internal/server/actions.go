package server

import (
	"context"
	"errors"
	"maps"
	"slices"

	"connectrpc.com/connect"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/internal/engine"
)

func (s *workflowService) SuspendWorkflow(ctx context.Context, req *connect.Request[chroniclev1.SuspendWorkflowRequest]) (*connect.Response[chroniclev1.SuspendWorkflowResponse], error) {
	sum, err := s.update(ctx, req.Msg.Namespace, req.Msg.Name, func(wf *v1alpha1.Workflow) error {
		wf.Spec.Suspend = true
		return nil
	})
	return respond(&chroniclev1.SuspendWorkflowResponse{Summary: sum}, err)
}

func (s *workflowService) ResumeWorkflow(ctx context.Context, req *connect.Request[chroniclev1.ResumeWorkflowRequest]) (*connect.Response[chroniclev1.ResumeWorkflowResponse], error) {
	sum, err := s.update(ctx, req.Msg.Namespace, req.Msg.Name, func(wf *v1alpha1.Workflow) error {
		wf.Spec.Suspend = false
		return nil
	})
	return respond(&chroniclev1.ResumeWorkflowResponse{Summary: sum}, err)
}

func (s *workflowService) StopWorkflow(ctx context.Context, req *connect.Request[chroniclev1.StopWorkflowRequest]) (*connect.Response[chroniclev1.StopWorkflowResponse], error) {
	sum, err := s.update(ctx, req.Msg.Namespace, req.Msg.Name, func(wf *v1alpha1.Workflow) error {
		if wf.Spec.Shutdown == "" {
			wf.Spec.Shutdown = v1alpha1.ShutdownStop
		}
		return nil
	})
	return respond(&chroniclev1.StopWorkflowResponse{Summary: sum}, err)
}

func (s *workflowService) TerminateWorkflow(ctx context.Context, req *connect.Request[chroniclev1.TerminateWorkflowRequest]) (*connect.Response[chroniclev1.TerminateWorkflowResponse], error) {
	sum, err := s.update(ctx, req.Msg.Namespace, req.Msg.Name, func(wf *v1alpha1.Workflow) error {
		wf.Spec.Shutdown = v1alpha1.ShutdownTerminate
		return nil
	})
	return respond(&chroniclev1.TerminateWorkflowResponse{Summary: sum}, err)
}

func (s *workflowService) ApproveNode(ctx context.Context, req *connect.Request[chroniclev1.ApproveNodeRequest]) (*connect.Response[chroniclev1.ApproveNodeResponse], error) {
	m := req.Msg
	if m.Node == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("give a node"))
	}
	a := v1alpha1.Approval{Node: m.Node, Decision: v1alpha1.DecisionReject, Message: m.Message}
	if m.Approve {
		a.Decision = v1alpha1.DecisionApprove
	}
	for _, name := range slices.Sorted(maps.Keys(m.Parameters)) {
		a.Parameters = append(a.Parameters, v1alpha1.Parameter{Name: name, Value: new(m.Parameters[name])})
	}
	sum, err := s.update(ctx, m.Namespace, m.Name, func(wf *v1alpha1.Workflow) error {
		for _, old := range wf.Spec.Approvals {
			if old.Node == m.Node {
				return connect.NewError(connect.CodeFailedPrecondition, errors.New("the node already has a decision"))
			}
		}
		if err := engine.CheckApproval(wf, a); errors.Is(err, engine.ErrNotWaiting) {
			return connect.NewError(connect.CodeFailedPrecondition, err)
		} else if err != nil {
			return connect.NewError(connect.CodeInvalidArgument, err)
		}
		wf.Spec.Approvals = append(wf.Spec.Approvals, a)
		return nil
	})
	return respond(&chroniclev1.ApproveNodeResponse{Summary: sum}, err)
}

func respond[T any](msg *T, err error) (*connect.Response[T], error) {
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(msg), nil
}
