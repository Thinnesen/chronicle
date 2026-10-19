package engine

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/expr"
)

// Restart prepares the status of a finished workflow for a retry. Without fromNode, it resets the nodes
// that failed. With fromNode, it resets that node, the nodes under it and all nodes downstream of it.
// It also resets the boundaries above each reset node and the exit handler, so that the engine walks them
// again; nodes that succeeded keep their outputs. It returns the new status and the Pods to delete.
func Restart(spec *v1alpha1.WorkflowTemplateSpec, status v1alpha1.WorkflowStatus, wfName, fromNode string) (v1alpha1.WorkflowStatus, []string, error) {
	if !isFinished(status.Phase) {
		return status, nil, errors.New("only a finished run can be retried")
	}
	r := newRestarter(spec, status.Nodes)
	if fromNode == "" {
		for id, n := range status.Nodes {
			if isFailure(n.Phase) || n.Phase == v1alpha1.NodeOmitted {
				r.reset[id] = true
			}
		}
		if len(r.reset) == 0 {
			return status, nil, errors.New("no node failed: use a re-run to run it again")
		}
	} else {
		n, ok := r.find(fromNode)
		if !ok {
			return status, nil, fmt.Errorf("the run has no node %q", fromNode)
		}
		r.subtree(n.ID)
		r.downstream(n.ID)
	}
	for id := range maps.Clone(r.reset) {
		r.ancestors(id)
	}
	if exit, ok := r.byName[wfName+".onExit"]; ok {
		r.subtree(exit.ID)
	}
	out := *status.DeepCopy()
	var pods []string
	for _, id := range slices.Sorted(maps.Keys(r.reset)) {
		if p := out.Nodes[id].PodName; p != "" {
			pods = append(pods, p)
		}
		delete(out.Nodes, id)
	}
	out.Phase, out.Message, out.FinishedAt, out.StartedAt = v1alpha1.WorkflowRunning, "", nil, nil
	return out, pods, nil
}

type restarter struct {
	nodes     map[string]v1alpha1.NodeStatus
	byName    map[string]v1alpha1.NodeStatus
	parent    map[string]string
	templates map[string]*v1alpha1.Template
	reset     map[string]bool
}

func newRestarter(spec *v1alpha1.WorkflowTemplateSpec, nodes map[string]v1alpha1.NodeStatus) *restarter {
	r := &restarter{nodes: nodes, byName: map[string]v1alpha1.NodeStatus{}, parent: map[string]string{},
		templates: map[string]*v1alpha1.Template{}, reset: map[string]bool{}}
	for id, n := range nodes {
		r.byName[n.Name] = n
		for _, c := range n.Children {
			r.parent[c] = id
		}
	}
	for i := range spec.Templates {
		r.templates[spec.Templates[i].Name] = &spec.Templates[i]
	}
	return r
}

func (r *restarter) find(idOrName string) (v1alpha1.NodeStatus, bool) {
	if n, ok := r.nodes[idOrName]; ok {
		return n, true
	}
	n, ok := r.byName[idOrName]
	return n, ok
}

func (r *restarter) subtree(id string) {
	if r.reset[id] {
		return
	}
	r.reset[id] = true
	for _, c := range r.nodes[id].Children {
		r.subtree(c)
	}
}

// ancestors resets the boundaries above a node. A retry node above it starts all its attempts again.
func (r *restarter) ancestors(id string) {
	for p, ok := r.parent[id]; ok; p, ok = r.parent[p] {
		if r.nodes[p].Type == v1alpha1.NodeRetry {
			r.subtree(p)
		}
		r.reset[p] = true
	}
}

// downstream resets the nodes after a node: dependent DAG tasks and later step groups, in its boundary
// and, because the boundary runs again, in each boundary above it.
func (r *restarter) downstream(id string) {
	for {
		p, ok := r.parent[id]
		if !ok {
			return
		}
		parent := r.nodes[p]
		switch parent.Type {
		case v1alpha1.NodeDAG:
			r.dagDependents(parent, strings.TrimPrefix(r.nodes[id].Name, parent.Name+"."))
		case v1alpha1.NodeStepGroup:
			r.laterGroups(parent)
		}
		id = p
	}
}

func (r *restarter) dagDependents(dag v1alpha1.NodeStatus, task string) {
	t := r.templates[dag.TemplateName]
	if t == nil || t.DAG == nil {
		return
	}
	users := map[string][]string{}
	for _, dt := range t.DAG.Tasks {
		for _, d := range taskDeps(dt) {
			users[d] = append(users[d], dt.Name)
		}
	}
	queue := []string{task}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, u := range users[cur] {
			if n, ok := r.byName[dag.Name+"."+u]; ok && !r.reset[n.ID] {
				r.subtree(n.ID)
				queue = append(queue, u)
			}
		}
	}
}

func taskDeps(t v1alpha1.DAGTask) []string {
	if len(t.Dependencies) > 0 {
		return t.Dependencies
	}
	d, _ := expr.ParseDepends(t.Depends)
	var out []string
	for _, ref := range d.Refs() {
		out = append(out, ref.Task)
	}
	return out
}

// laterGroups resets the step groups after a group of a steps node.
func (r *restarter) laterGroups(group v1alpha1.NodeStatus) {
	steps := r.nodes[r.parent[group.ID]]
	i := slices.Index(steps.Children, group.ID)
	for _, g := range steps.Children[i+1:] {
		r.subtree(g)
	}
}
