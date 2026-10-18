package server

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

// cronService implements the CronService API.
type cronService struct {
	clients *clientFactory
}

func (s *cronService) ListCronWorkflows(ctx context.Context, req *connect.Request[chroniclev1.ListCronWorkflowsRequest]) (*connect.Response[chroniclev1.ListCronWorkflowsResponse], error) {
	if req.Msg.Namespace == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("give a namespace"))
	}
	c, err := s.clients.clientFor(ctx)
	if err != nil {
		return nil, err
	}
	var list v1alpha1.CronWorkflowList
	if err := c.List(ctx, &list, client.InNamespace(req.Msg.Namespace)); err != nil {
		return nil, toConnect(err)
	}
	resp := &chroniclev1.ListCronWorkflowsResponse{}
	for i := range list.Items {
		resp.CronWorkflows = append(resp.CronWorkflows, cronSummary(&list.Items[i]))
	}
	slices.SortFunc(resp.CronWorkflows, func(a, b *chroniclev1.CronSummary) int { return strings.Compare(a.Name, b.Name) })
	return connect.NewResponse(resp), nil
}

func (s *cronService) GetCronWorkflow(ctx context.Context, req *connect.Request[chroniclev1.GetCronWorkflowRequest]) (*connect.Response[chroniclev1.GetCronWorkflowResponse], error) {
	c, err := s.clients.clientFor(ctx)
	if err != nil {
		return nil, err
	}
	var cw v1alpha1.CronWorkflow
	if err := c.Get(ctx, client.ObjectKey{Namespace: req.Msg.Namespace, Name: req.Msg.Name}, &cw); err != nil {
		return nil, toConnect(err)
	}
	cw.ManagedFields = nil
	st, err := toStruct(&cw)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&chroniclev1.GetCronWorkflowResponse{Summary: cronSummary(&cw), CronWorkflow: st}), nil
}

func (s *cronService) SuspendCronWorkflow(ctx context.Context, req *connect.Request[chroniclev1.SuspendCronWorkflowRequest]) (*connect.Response[chroniclev1.SuspendCronWorkflowResponse], error) {
	sum, err := s.setSuspend(ctx, req.Msg.Namespace, req.Msg.Name, true)
	return respond(&chroniclev1.SuspendCronWorkflowResponse{Summary: sum}, err)
}

func (s *cronService) ResumeCronWorkflow(ctx context.Context, req *connect.Request[chroniclev1.ResumeCronWorkflowRequest]) (*connect.Response[chroniclev1.ResumeCronWorkflowResponse], error) {
	sum, err := s.setSuspend(ctx, req.Msg.Namespace, req.Msg.Name, false)
	return respond(&chroniclev1.ResumeCronWorkflowResponse{Summary: sum}, err)
}

// setSuspend changes spec.suspend as the caller. It refuses an object that a GitOps tool manages.
func (s *cronService) setSuspend(ctx context.Context, namespace, name string, suspend bool) (*chroniclev1.CronSummary, error) {
	c, err := s.clients.clientFor(ctx)
	if err != nil {
		return nil, err
	}
	var cw v1alpha1.CronWorkflow
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &cw); err != nil {
			return err
		}
		if tool := managedBy(&cw); tool != "" {
			return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("%s manages this CronWorkflow: change it in Git", tool))
		}
		cw.Spec.Suspend = suspend
		return c.Update(ctx, &cw)
	})
	if err != nil {
		return nil, toConnect(err)
	}
	return cronSummary(&cw), nil
}

func cronSummary(cw *v1alpha1.CronWorkflow) *chroniclev1.CronSummary {
	s := &chroniclev1.CronSummary{
		Namespace: cw.Namespace, Name: cw.Name, Schedules: cw.Spec.Schedules, Timezone: cw.Spec.Timezone,
		Suspend: cw.Spec.Suspend, ConcurrencyPolicy: string(cw.Spec.ConcurrencyPolicy),
		LastScheduledAt: timestamp(cw.Status.LastScheduledTime), NextScheduledAt: timestamp(cw.Status.NextScheduledTime),
		Active: int32(len(cw.Status.Active)), ManagedBy: managedBy(cw), Description: cw.Annotations[DescriptionAnnotation],
	}
	if ref := cw.Spec.WorkflowSpec.WorkflowTemplateRef; ref != nil {
		s.Template = ref.Name
	}
	return s
}
