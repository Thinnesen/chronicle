package server

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"

	"connectrpc.com/connect"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/internal/engine"
)

// LabelRerunOf names the run that a re-run copies.
const LabelRerunOf = "chronicle.thinnesen.com/rerun-of"

func (s *workflowService) RetryWorkflow(ctx context.Context, req *connect.Request[chroniclev1.RetryWorkflowRequest]) (*connect.Response[chroniclev1.RetryWorkflowResponse], error) {
	m := req.Msg
	sum, err := s.update(ctx, m.Namespace, m.Name, func(wf *v1alpha1.Workflow) error {
		switch wf.Status.Phase {
		case v1alpha1.WorkflowSucceeded, v1alpha1.WorkflowFailed, v1alpha1.WorkflowError:
		default:
			return connect.NewError(connect.CodeFailedPrecondition, errors.New("the run has not finished: stop it first or wait"))
		}
		if m.Node == "" && wf.Status.Phase == v1alpha1.WorkflowSucceeded {
			return connect.NewError(connect.CodeFailedPrecondition, errors.New("no node failed: retry from a node or re-run the run"))
		}
		// A dry run of the retry finds the nodes that run again, and refuses a node that does not exist.
		spec := wf.Status.StoredSpec
		if spec == nil {
			spec = &wf.Spec.WorkflowTemplateSpec
		}
		next, _, err := engine.Restart(spec, wf.Status, wf.Name, m.Node)
		if err != nil {
			return connect.NewError(connect.CodeFailedPrecondition, err)
		}
		// A suspend node that runs again needs a new approval. A stop or a suspend of the earlier try must not end the
		// retry at once.
		wf.Spec.Approvals = slices.DeleteFunc(wf.Spec.Approvals, func(a v1alpha1.Approval) bool { return !kept(next.Nodes, a.Node) })
		wf.Spec.Shutdown, wf.Spec.Suspend = "", false
		gen := wf.Status.Restarts + 1
		if wf.Spec.Restart != nil && wf.Spec.Restart.Generation >= gen {
			gen = wf.Spec.Restart.Generation + 1
		}
		wf.Spec.Restart = &v1alpha1.Restart{Generation: gen, FromNode: m.Node}
		return nil
	})
	if connect.CodeOf(err) == connect.CodeNotFound {
		if _, archived, aerr := s.getAny(ctx, m.Namespace, m.Name); aerr == nil && archived {
			return nil, connect.NewError(connect.CodeFailedPrecondition,
				errors.New("the run is only in the archive, so it cannot continue: re-run it to start it again"))
		}
	}
	return respond(&chroniclev1.RetryWorkflowResponse{Summary: sum}, err)
}

func (s *workflowService) RerunWorkflow(ctx context.Context, req *connect.Request[chroniclev1.RerunWorkflowRequest]) (*connect.Response[chroniclev1.RerunWorkflowResponse], error) {
	m := req.Msg
	old, _, err := s.getAny(ctx, m.Namespace, m.Name)
	if err != nil {
		return nil, err
	}
	c, err := s.client(ctx, m.Namespace)
	if err != nil {
		return nil, err
	}
	declared := old.Spec.Arguments
	if old.Status.StoredSpec != nil {
		declared = old.Status.StoredSpec.Arguments
	}
	if err := unknownParameter(declared, m.Parameters); err != nil {
		return nil, err
	}
	wf := rerunOf(old)
	setParameters(&wf.Spec.Arguments, m.Parameters)
	if err := c.Create(ctx, wf); err != nil {
		return nil, toConnect(err)
	}
	return connect.NewResponse(&chroniclev1.RerunWorkflowResponse{Summary: summary(wf)}), nil
}

// rerunOf returns a new run with the spec of a run. The run controls and the controller labels stay behind.
func rerunOf(old *v1alpha1.Workflow) *v1alpha1.Workflow {
	labels := maps.Clone(old.Labels)
	maps.DeleteFunc(labels, func(k, _ string) bool { return strings.HasPrefix(k, "chronicle.thinnesen.com/") })
	if labels == nil {
		labels = map[string]string{}
	}
	labels[LabelRerunOf] = old.Name
	spec := *old.Spec.DeepCopy()
	spec.Suspend, spec.Shutdown, spec.Approvals, spec.Restart = false, "", nil, nil
	return &v1alpha1.Workflow{
		GenerateName: rerunPrefix(old.Name),
		Namespace:    old.Namespace,
		Labels:       labels,
		Spec:         spec,
	}
}

// rerunPrefix removes the random suffix that GenerateName added to the name of the run. So all re-runs keep one name
// stem.
func rerunPrefix(name string) string {
	if i := strings.LastIndexByte(name, '-'); i > 0 && len(name)-i == 6 {
		return name[:i+1]
	}
	return name + "-"
}

// kept tells if a node, by ID or by name, stays after a retry.
func kept(nodes map[string]v1alpha1.NodeStatus, node string) bool {
	if _, ok := nodes[node]; ok {
		return true
	}
	for _, n := range nodes {
		if n.Name == node {
			return true
		}
	}
	return false
}
