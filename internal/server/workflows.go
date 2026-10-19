package server

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strconv"

	"connectrpc.com/connect"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

// maxPage is the default and the largest page size of a list.
const maxPage = 500

// workflowService implements the WorkflowService API.
type workflowService struct {
	clients *clientFactory
	// archive holds finished runs whose Workflow was deleted, or is nil.
	archive ArchiveReader
}

func (s *workflowService) ListWorkflows(ctx context.Context, req *connect.Request[chroniclev1.ListWorkflowsRequest]) (*connect.Response[chroniclev1.ListWorkflowsResponse], error) {
	m := req.Msg
	c, err := s.client(ctx, m.Namespace)
	if err != nil {
		return nil, err
	}
	opts := []client.ListOption{client.InNamespace(m.Namespace)}
	if m.LabelSelector != "" {
		sel, err := labels.Parse(m.LabelSelector)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		opts = append(opts, client.MatchingLabelsSelector{Selector: sel})
	}
	var list v1alpha1.WorkflowList
	if err := c.List(ctx, &list, opts...); err != nil {
		return nil, toConnect(err)
	}
	live := slices.DeleteFunc(list.Items, func(wf v1alpha1.Workflow) bool {
		return len(m.Phases) > 0 && !slices.Contains(m.Phases, string(wf.Status.Phase))
	})
	items := make([]*chroniclev1.WorkflowSummary, 0, len(live))
	for i := range live {
		items = append(items, summary(&live[i]))
	}
	archived, err := s.archivedRuns(ctx, m, live)
	if err != nil {
		return nil, err
	}
	items = append(items, archived...)
	slices.SortFunc(items, func(a, b *chroniclev1.WorkflowSummary) int {
		return cmp.Or(b.CreatedAt.AsTime().Compare(a.CreatedAt.AsTime()), cmp.Compare(a.Name, b.Name))
	})
	page, next, err := paginate(items, m.Limit, m.PageToken)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&chroniclev1.ListWorkflowsResponse{Workflows: page, NextPageToken: next}), nil
}

func pageOffset(token string) (int, error) {
	offset, err := strconv.Atoi(token)
	if err != nil || offset < 0 {
		return 0, connect.NewError(connect.CodeInvalidArgument, errors.New("bad page token"))
	}
	return offset, nil
}

// paginate returns one page. The page token is the offset of the page.
func paginate[T any](items []T, limit int32, token string) ([]T, string, error) {
	if limit <= 0 || limit > maxPage {
		limit = maxPage
	}
	offset := 0
	if token != "" {
		var err error
		if offset, err = pageOffset(token); err != nil {
			return nil, "", err
		}
	}
	if offset >= len(items) {
		return nil, "", nil
	}
	end := min(offset+int(limit), len(items))
	next := ""
	if end < len(items) {
		next = strconv.Itoa(end)
	}
	return items[offset:end], next, nil
}

func (s *workflowService) GetWorkflow(ctx context.Context, req *connect.Request[chroniclev1.GetWorkflowRequest]) (*connect.Response[chroniclev1.GetWorkflowResponse], error) {
	wf, archived, err := s.getAny(ctx, req.Msg.Namespace, req.Msg.Name)
	if err != nil {
		return nil, err
	}
	wf.ManagedFields = nil
	obj, err := toStruct(wf)
	if err != nil {
		return nil, err
	}
	sum := summary(wf)
	sum.Archived = archived
	return connect.NewResponse(&chroniclev1.GetWorkflowResponse{Summary: sum, Workflow: obj}), nil
}

// getAny returns a live run, or the archived run when the Workflow was deleted.
func (s *workflowService) getAny(ctx context.Context, namespace, name string) (*v1alpha1.Workflow, bool, error) {
	wf, err := s.get(ctx, namespace, name)
	if connect.CodeOf(err) != connect.CodeNotFound {
		return wf, false, err
	}
	c, cerr := s.client(ctx, namespace)
	if cerr != nil {
		return nil, false, cerr
	}
	wf, err = s.archivedWorkflow(ctx, c, namespace, name)
	return wf, true, err
}

func (s *workflowService) DeleteWorkflow(ctx context.Context, req *connect.Request[chroniclev1.DeleteWorkflowRequest]) (*connect.Response[chroniclev1.DeleteWorkflowResponse], error) {
	c, err := s.client(ctx, req.Msg.Namespace)
	if err != nil {
		return nil, err
	}
	wf := &v1alpha1.Workflow{Name: req.Msg.Name, Namespace: req.Msg.Namespace}
	if err := c.Delete(ctx, wf, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
		return nil, toConnect(err)
	}
	return connect.NewResponse(&chroniclev1.DeleteWorkflowResponse{}), nil
}

func (s *workflowService) client(ctx context.Context, namespace string) (client.Client, error) {
	if namespace == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("give a namespace"))
	}
	return s.clients.clientFor(ctx)
}

func (s *workflowService) get(ctx context.Context, namespace, name string) (*v1alpha1.Workflow, error) {
	c, err := s.client(ctx, namespace)
	if err != nil {
		return nil, err
	}
	var wf v1alpha1.Workflow
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &wf); err != nil {
		return nil, toConnect(err)
	}
	return &wf, nil
}

// update changes a run with the identity of the caller and retries on a conflict.
func (s *workflowService) update(ctx context.Context, namespace, name string, change func(*v1alpha1.Workflow) error) (*chroniclev1.WorkflowSummary, error) {
	c, err := s.client(ctx, namespace)
	if err != nil {
		return nil, err
	}
	var wf v1alpha1.Workflow
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &wf); err != nil {
			return err
		}
		if err := change(&wf); err != nil {
			return err
		}
		return c.Update(ctx, &wf)
	})
	if err != nil {
		return nil, toConnect(err)
	}
	return summary(&wf), nil
}
