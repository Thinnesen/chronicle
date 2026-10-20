package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/internal/archive"
)

// ArchiveReader reads finished runs and the earlier tries of retried runs from the archive.
type ArchiveReader interface {
	List(ctx context.Context, q archive.Query) ([]archive.Row, error)
	Get(ctx context.Context, namespace, name string) (*v1alpha1.Workflow, error)
	Tries(ctx context.Context, uid string) ([]archive.Try, error)
	GetTry(ctx context.Context, uid string, number int32) (*v1alpha1.Workflow, error)
}

// archivedRuns returns the archived runs after the cursor that the live list does not have. The caller already listed
// the namespace with its own permissions, so it may read the archive of the namespace too. It reads more than one page,
// because the archive can also have the live runs.
func (s *workflowService) archivedRuns(ctx context.Context, m *chroniclev1.ListWorkflowsRequest, live []v1alpha1.Workflow, after *cursor, limit int) ([]*chroniclev1.WorkflowSummary, error) {
	if s.archive == nil {
		return nil, nil
	}
	match, ok := equalityLabels(m.LabelSelector)
	if !ok {
		// The archive filters only by label equality. Other selectors show only live runs.
		return nil, nil
	}
	q := archive.Query{Namespace: m.Namespace, Labels: match, Phases: m.Phases, Limit: limit + len(live) + 1}
	if after != nil {
		q.After = &archive.Position{CreatedAt: after.Time, Name: after.Name}
	}
	rows, err := s.archive.List(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list the archived runs: %w", err)
	}
	seen := map[string]bool{}
	for _, wf := range live {
		seen[string(wf.UID)] = true
	}
	var out []*chroniclev1.WorkflowSummary
	for _, r := range rows {
		if !seen[r.UID] {
			out = append(out, rowSummary(r))
		}
	}
	return out, nil
}

// archivedWorkflow returns a run from the archive after an access review of the caller.
func (s *workflowService) archivedWorkflow(ctx context.Context, c client.Client, namespace, name string) (*v1alpha1.Workflow, error) {
	if s.archive == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("the run does not exist"))
	}
	if !allowed(ctx, c, namespace, "get") {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("you cannot get runs in this namespace"))
	}
	wf, err := s.archive.Get(ctx, namespace, name)
	if errors.Is(err, archive.ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("the run does not exist, not even in the archive"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, err)
	}
	return wf, nil
}

// equalityLabels turns a selector of "key=value" terms into a map. It returns false for other operators.
func equalityLabels(selector string) (map[string]string, bool) {
	sel, err := labels.Parse(selector)
	if err != nil {
		return nil, false
	}
	reqs, _ := sel.Requirements()
	out := map[string]string{}
	for _, r := range reqs {
		vals := r.Values().UnsortedList()
		if (r.Operator() != selection.Equals && r.Operator() != selection.DoubleEquals) || len(vals) != 1 {
			return nil, false
		}
		out[r.Key()] = vals[0]
	}
	return out, true
}

func rowSummary(r archive.Row) *chroniclev1.WorkflowSummary {
	s := &chroniclev1.WorkflowSummary{
		Namespace: r.Namespace, Name: r.Name, Uid: r.UID, Phase: r.Phase, Message: r.Message, Labels: r.Labels,
		Template: r.Template, NodesDone: r.NodesDone, NodesTotal: r.NodesTotal, Archived: true,
		CreatedAt: timestamppb.New(r.CreatedAt),
	}
	if r.StartedAt != nil {
		s.StartedAt = timestamppb.New(*r.StartedAt)
	}
	if r.FinishedAt != nil {
		s.FinishedAt = timestamppb.New(*r.FinishedAt)
	}
	return s
}

// tries returns the earlier tries of a run. The caller already read the run with the permissions of the user. Without
// the archive, or when it does not answer, a run has no earlier tries.
func (s *workflowService) tries(ctx context.Context, wf *v1alpha1.Workflow) []*chroniclev1.Try {
	if s.archive == nil {
		return nil
	}
	tries, err := s.archive.Tries(ctx, string(wf.UID))
	if err != nil {
		logf.FromContext(ctx).Error(err, "Cannot list the tries of a run", "workflow", wf.Name)
		return nil
	}
	out := make([]*chroniclev1.Try, 0, len(tries))
	for _, t := range tries {
		out = append(out, &chroniclev1.Try{Number: t.Number, Phase: t.Phase, FinishedAt: timestampOf(t.FinishedAt)})
	}
	return out
}

// workflowTry returns a run, or one of its earlier tries. The run is read first, so that the access review of the
// run also protects its tries.
func (s *workflowService) workflowTry(ctx context.Context, namespace, name string, try *int32) (*v1alpha1.Workflow, bool, error) {
	wf, archived, err := s.getAny(ctx, namespace, name)
	if err != nil || try == nil || *try == wf.Status.Restarts {
		return wf, archived, err
	}
	if s.archive == nil {
		return nil, false, connect.NewError(connect.CodeNotFound, errors.New("the earlier tries are in the archive, which is off"))
	}
	old, err := s.archive.GetTry(ctx, string(wf.UID), *try)
	if errors.Is(err, archive.ErrNotFound) {
		return nil, false, connect.NewError(connect.CodeNotFound, fmt.Errorf("the archive has no try %d of the run", *try))
	}
	if err != nil {
		return nil, false, connect.NewError(connect.CodeUnavailable, err)
	}
	return old, true, nil
}

func timestampOf(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}
