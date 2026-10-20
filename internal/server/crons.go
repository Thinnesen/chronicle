package server

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"connectrpc.com/connect"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/internal/schedule"
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
		LastStartedAt: timestamp(cw.Status.LastStartedTime),
		Active:        int32(len(cw.Status.Active)), ManagedBy: managedBy(cw), Description: cw.Annotations[DescriptionAnnotation],
	}
	if ref := cw.Spec.WorkflowSpec.WorkflowTemplateRef; ref != nil {
		s.Template = ref.Name
	}
	return s
}

// maxBackfill limits the runs of one backfill.
const maxBackfill = 100

func (s *cronService) BackfillCronWorkflow(ctx context.Context, req *connect.Request[chroniclev1.BackfillCronWorkflowRequest]) (*connect.Response[chroniclev1.BackfillCronWorkflowResponse], error) {
	m := req.Msg
	if m.From == nil || m.To == nil || m.To.AsTime().Before(m.From.AsTime()) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("give from and to, and to must not be before from"))
	}
	c, err := s.clients.clientFor(ctx)
	if err != nil {
		return nil, err
	}
	var cw v1alpha1.CronWorkflow
	if err := c.Get(ctx, client.ObjectKey{Namespace: m.Namespace, Name: m.Name}, &cw); err != nil {
		return nil, toConnect(err)
	}
	sched, err := schedule.Parse(cw.Spec.Schedules, cw.Spec.Timezone)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	times, err := sched.Between(m.From.AsTime(), m.To.AsTime(), maxBackfill)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	resp := &chroniclev1.BackfillCronWorkflowResponse{}
	for _, at := range times {
		wf := schedule.NewRun(&cw, at)
		// The caller creates the run. Without blockOwnerDeletion, the caller needs no rights on the CronWorkflow itself.
		wf.OwnerReferences[0].BlockOwnerDeletion = nil
		switch err := c.Create(ctx, wf); {
		case apierrors.IsAlreadyExists(err):
			resp.Existing = append(resp.Existing, wf.Name)
		case err != nil:
			return nil, toConnect(err)
		default:
			resp.Created = append(resp.Created, wf.Name)
		}
	}
	return connect.NewResponse(resp), nil
}
