package controller

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/telemetry"
)

// envTraceparent is the standard variable that carries the trace context to a process.
const envTraceparent = "TRACEPARENT"

// spanKey is the key of the span of a node in the current try of a run. Each try has its own spans.
func spanKey(wf *chroniclev1alpha1.Workflow, node string) string {
	return strconv.Itoa(int(wf.Status.Restarts)) + "/" + node
}

// traceparent returns the trace context that makes the spans of a step children of its node span.
// It is empty without traces.
func (r *WorkflowReconciler) traceparent(wf *chroniclev1alpha1.Workflow, node string) string {
	if r.Tracer == nil {
		return ""
	}
	return telemetry.Traceparent(string(wf.UID), spanKey(wf, node))
}

// traceRun records the spans of a finished run, when traces are on.
func (r *WorkflowReconciler) traceRun(ctx context.Context, wf *chroniclev1alpha1.Workflow) {
	if r.Tracer == nil {
		return
	}
	run, nodes := runSpans(wf)
	telemetry.EmitRun(ctx, r.Tracer, string(wf.UID), run, nodes)
}

// runSpans returns the span of a finished run and the spans of the nodes that ran in its current try.
func runSpans(wf *chroniclev1alpha1.Workflow) (telemetry.NodeSpan, []telemetry.NodeSpan) {
	st := wf.Status
	end := st.FinishedAt.Time
	start := end
	if st.StartedAt != nil {
		start = st.StartedAt.Time
	}
	l := runLabels(wf)
	run := telemetry.NodeSpan{ID: strconv.Itoa(int(st.Restarts)),
		Name: wf.Name, Start: start, End: end,
		Attributes: map[string]string{
			"chronicle.namespace": l[0], "chronicle.template": l[1], "chronicle.cron": l[2],
			"chronicle.workflow": wf.Name, "chronicle.phase": string(st.Phase), "chronicle.try": strconv.Itoa(int(st.Restarts) + 1),
		},
	}
	if st.Phase != chroniclev1alpha1.WorkflowSucceeded {
		run.Error = cmp.Or(st.Message, string(st.Phase))
	}
	parents := map[string]string{}
	for id, n := range st.Nodes {
		for _, c := range n.Children {
			parents[c] = id
		}
	}
	ran := func(n chroniclev1alpha1.NodeStatus) bool {
		// A retry creates the nodes that it runs again, so a node that started before the try comes from an earlier try.
		// Its finish time can be in the same second as the start of the try.
		return n.StartedAt != nil && !n.StartedAt.Time.Before(start)
	}
	var nodes []telemetry.NodeSpan
	for id, n := range st.Nodes {
		if !ran(n) {
			continue
		}
		s := telemetry.NodeSpan{ID: spanKey(wf, id),
			Name: n.Name, Start: n.StartedAt.Time, End: end,
			Attributes: map[string]string{
				"chronicle.node.type": string(n.Type), "chronicle.node.template": n.TemplateName,
				"chronicle.node.phase": string(n.Phase), "chronicle.node.pod": n.PodName,
			},
		}
		if n.FinishedAt != nil {
			s.End = n.FinishedAt.Time
		}
		if p, ok := parents[id]; ok && ran(st.Nodes[p]) {
			s.Parent = spanKey(wf, p)
		}
		if n.Phase == chroniclev1alpha1.NodeFailed || n.Phase == chroniclev1alpha1.NodeError {
			s.Error = cmp.Or(n.Message, string(n.Phase))
		}
		nodes = append(nodes, s)
	}
	slices.SortFunc(nodes, func(a, b telemetry.NodeSpan) int {
		return cmp.Or(a.Start.Compare(b.Start), strings.Compare(a.ID, b.ID))
	})
	return run, nodes
}
