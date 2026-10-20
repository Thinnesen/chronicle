package server

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

// workflowService implements the WorkflowService API.
type workflowService struct {
	clients *clientFactory
	// archive holds finished runs whose Workflow was deleted, or is nil.
	archive ArchiveReader
	// namespaces finds the namespaces of the caller for a list of all namespaces.
	namespaces *namespaceService
}

func (s *workflowService) GetWorkflow(ctx context.Context, req *connect.Request[chroniclev1.GetWorkflowRequest]) (*connect.Response[chroniclev1.GetWorkflowResponse], error) {
	wf, archived, err := s.workflowTry(ctx, req.Msg.Namespace, req.Msg.Name, req.Msg.Try)
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
	return connect.NewResponse(&chroniclev1.GetWorkflowResponse{Summary: sum, Workflow: obj, Tries: s.tries(ctx, wf)}), nil
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
