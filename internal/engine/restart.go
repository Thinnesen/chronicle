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
		n, err := r.find(fromNode)
		if err != nil {
			return status, nil, err
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
		n := out.Nodes[id]
		if fromNode == "" && r.keepsChild(n) {
			// The failed child run retries its own failed nodes: the node waits for it with the same child run.
			n.Phase, n.Message, n.StartedAt, n.FinishedAt, n.Outputs = v1alpha1.NodePending, "", nil, nil, nil
			out.Nodes[id] = n
			continue
		}
		if n.PodName != "" {
			pods = append(pods, n.PodName)
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

// find returns a node by its ID, its name or its short name: the display name or the last part of the name. A short
// name, such as "build", must name one node.
func (r *restarter) find(node string) (v1alpha1.NodeStatus, error) {
	if n, ok := r.nodes[node]; ok {
		return n, nil
	}
	if n, ok := r.byName[node]; ok {
		return n, nil
	}
	var found []v1alpha1.NodeStatus
	for _, n := range r.nodes {
		if n.DisplayName == node || strings.HasSuffix(n.Name, "."+node) {
			found = append(found, n)
		}
	}
	switch len(found) {
	case 0:
		return v1alpha1.NodeStatus{}, fmt.Errorf("the run has no node %q", node)
	case 1:
		return found[0], nil
	}
	return v1alpha1.NodeStatus{}, fmt.Errorf("%d nodes have the name %q: give its ID", len(found), node)
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

// downstream resets the nodes after a node: the dependent DAG tasks and the later step groups. It does this in the
// boundary of the node and in each boundary above it, because each of these boundaries runs again.
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

// keepsChild tells if a retry keeps the node of a failed child run. An attempt of a retry strategy gets a new child run.
func (r *restarter) keepsChild(n v1alpha1.NodeStatus) bool {
	if n.Type != v1alpha1.NodeChildWorkflow || n.ChildWorkflow == "" || !isFailure(n.Phase) {
		return false
	}
	return r.nodes[r.parent[n.ID]].Type != v1alpha1.NodeRetry
}
