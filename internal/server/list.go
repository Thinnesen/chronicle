package server

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"connectrpc.com/connect"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

// maxListNamespaces limits a list of all namespaces, because it lists each namespace as the caller. A caller who may
// list Workflows in the whole cluster gets one list instead, without this limit. Tests change it.
var maxListNamespaces = 100

func (s *workflowService) ListWorkflows(ctx context.Context, req *connect.Request[chroniclev1.ListWorkflowsRequest]) (*connect.Response[chroniclev1.ListWorkflowsResponse], error) {
	m := req.Msg
	namespaces, cluster, err := s.listNamespaces(ctx, m)
	if err != nil {
		return nil, err
	}
	after, err := parseCursor(m.PageToken)
	if err != nil {
		return nil, err
	}
	live, err := s.liveRuns(ctx, m, namespaces, cluster, after)
	if err != nil {
		return nil, err
	}
	items := make([]*chroniclev1.WorkflowSummary, 0, len(live))
	for i := range live {
		items = append(items, summary(&live[i]))
	}
	limit := pageSize(m.Limit)
	// Without the archive, the live runs still come, with a mark. So the list does not fail as a whole.
	archived, archiveErr := s.archivedRuns(ctx, m, namespaces, live, after, limit)
	if archiveErr != nil {
		log.FromContext(ctx).Error(archiveErr, "The archive did not answer", "namespaces", len(namespaces))
	}
	items = append(items, archived...)
	slices.SortFunc(items, compareRuns)
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1]
		next = cursor{Time: last.CreatedAt.AsTime(), Name: last.Name, Namespace: last.Namespace}.token()
	}
	return connect.NewResponse(&chroniclev1.ListWorkflowsResponse{Workflows: items, NextPageToken: next, ArchiveUnavailable: archiveErr != nil}), nil
}

// listNamespaces returns the namespaces of a list: the one of the request, or all where the caller can list Workflows.
// cluster is true when the caller may list Workflows in the whole cluster.
func (s *workflowService) listNamespaces(ctx context.Context, m *chroniclev1.ListWorkflowsRequest) (names []string, cluster bool, err error) {
	if !m.AllNamespaces {
		if m.Namespace == "" {
			return nil, false, connect.NewError(connect.CodeInvalidArgument, errors.New("give a namespace"))
		}
		return []string{m.Namespace}, false, nil
	}
	if m.Namespace != "" {
		return nil, false, connect.NewError(connect.CodeInvalidArgument, errors.New("give a namespace or all namespaces, not both"))
	}
	c, err := s.clients.clientFor(ctx)
	if err != nil {
		return nil, false, err
	}
	cluster = allowed(ctx, c, "", "list")
	access, err := s.namespaces.access(ctx)
	if err != nil {
		return nil, false, err
	}
	if !cluster && len(access) > maxListNamespaces {
		return nil, false, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("you can see %d namespaces, and a list of all namespaces has at most %d: choose a namespace", len(access), maxListNamespaces))
	}
	names = make([]string, len(access))
	for i, a := range access {
		names[i] = a.Name
	}
	return names, cluster, nil
}

// liveRuns lists the Workflows of the namespaces as the caller, after the cursor and in the phases of the request.
func (s *workflowService) liveRuns(ctx context.Context, m *chroniclev1.ListWorkflowsRequest, namespaces []string, cluster bool,
	after *cursor) ([]v1alpha1.Workflow, error) {
	c, err := s.clients.clientFor(ctx)
	if err != nil {
		return nil, err
	}
	var sel labels.Selector
	if m.LabelSelector != "" {
		if sel, err = labels.Parse(m.LabelSelector); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
	}
	if cluster {
		live, err := listIn(ctx, c, "", sel)
		if err != nil {
			return nil, toConnect(err)
		}
		return keep(live, m, namespaces, after), nil
	}
	lists := make([][]v1alpha1.Workflow, len(namespaces))
	errs := make([]error, len(namespaces))
	sem := make(chan struct{}, accessWorkers)
	var wg sync.WaitGroup
	for i, ns := range namespaces {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			lists[i], errs[i] = listIn(ctx, c, ns, sel)
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, toConnect(err)
		}
	}
	return keep(slices.Concat(lists...), m, namespaces, after), nil
}

// keep removes the runs outside the namespaces and the phases of the request, and those before the cursor.
func keep(live []v1alpha1.Workflow, m *chroniclev1.ListWorkflowsRequest, namespaces []string, after *cursor) []v1alpha1.Workflow {
	return slices.DeleteFunc(live, func(wf v1alpha1.Workflow) bool {
		return !slices.Contains(namespaces, wf.Namespace) || len(m.Phases) > 0 && !slices.Contains(m.Phases, string(wf.Status.Phase)) ||
			after.before(wf.CreationTimestamp.Time, wf.Name, wf.Namespace)
	})
}

// listIn lists the Workflows of one namespace. The UI polls this list. ResourceVersion "0" serves it from the watch
// cache of the API server, not from etcd.
func listIn(ctx context.Context, c client.Client, namespace string, sel labels.Selector) ([]v1alpha1.Workflow, error) {
	opts := []client.ListOption{client.InNamespace(namespace), &client.ListOptions{Raw: &metav1.ListOptions{ResourceVersion: "0"}}}
	if sel != nil {
		opts = append(opts, client.MatchingLabelsSelector{Selector: sel})
	}
	var list v1alpha1.WorkflowList
	if err := c.List(ctx, &list, opts...); err != nil {
		return nil, err
	}
	return list.Items, nil
}
