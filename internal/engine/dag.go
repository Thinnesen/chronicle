package engine

import (
	"slices"
	"strconv"
	"strings"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/expr"
)

// runBoundary creates or continues a DAG or steps node and walks its tasks.
func (e *engine) runBoundary(c call, typ v1alpha1.NodeType, tasks []task, failFast bool, depth int) v1alpha1.NodeStatus {
	id := e.nodeID(c.nodeName)
	if _, ok := e.nodes[id]; !ok {
		n := e.newNode(c, typ)
		n.StartedAt = &e.in.Now
		e.setPhase(&n, v1alpha1.NodeRunning, "")
		e.save(n)
	}
	phase, msg := newBoundary(e, id, tasks, failFast, depth).walk()
	n := e.nodes[id]
	if phase != v1alpha1.NodeRunning {
		e.setPhase(&n, phase, msg)
	}
	return e.save(n)
}

func (e *engine) runDAG(c call, dag *v1alpha1.DAGTemplate, depth int) v1alpha1.NodeStatus {
	tasks := make([]task, 0, len(dag.Tasks))
	for _, t := range dag.Tasks {
		cond := t.Depends
		if cond == "" && len(t.Dependencies) > 0 {
			cond = strings.Join(t.Dependencies, " && ")
		}
		tasks = append(tasks, newTask(t.Name, c.nodeName+"."+t.Name, t.Template, t.Arguments, cond, ""))
	}
	failFast := dag.FailFast == nil || *dag.FailFast
	return e.runBoundary(c, v1alpha1.NodeDAG, topoOrder(tasks), failFast, depth)
}

func (e *engine) runSteps(c call, groups [][]v1alpha1.WorkflowStep, depth int) v1alpha1.NodeStatus {
	var tasks []task
	var prev []string
	for i, group := range groups {
		groupName := c.nodeName + "[" + strconv.Itoa(i) + "]"
		cond := strings.Join(prev, " && ")
		prev = prev[:0:0]
		for _, s := range group {
			tasks = append(tasks, newTask(s.Name, groupName+"."+s.Name, s.Template, s.Arguments, cond, groupName))
			prev = append(prev, s.Name)
		}
	}
	n := e.runBoundary(c, v1alpha1.NodeSteps, tasks, false, depth)
	e.updateGroups(n)
	return n
}

// updateGroups sets the phase of each step group from its steps.
func (e *engine) updateGroups(steps v1alpha1.NodeStatus) {
	for _, gid := range steps.Children {
		g := e.nodes[gid]
		if isTerminal(g.Phase) {
			continue
		}
		phase, omitted := v1alpha1.NodeSucceeded, true
		for _, cid := range g.Children {
			p := e.nodes[cid].Phase
			if !isTerminal(p) {
				phase = v1alpha1.NodeRunning
				break
			}
			omitted = omitted && p == v1alpha1.NodeOmitted
			phase = worse(phase, p)
		}
		if phase == v1alpha1.NodeSucceeded && omitted {
			phase = v1alpha1.NodeOmitted
		}
		if phase != v1alpha1.NodeRunning {
			e.setPhase(&g, phase, "")
			e.save(g)
		}
	}
}

// newTask builds a task. The depends expression was checked by the validation.
func newTask(name, nodeName, template string, args v1alpha1.Arguments, cond, group string) task {
	t := task{name: name, nodeName: nodeName, template: template, args: args, group: group, tests: map[string][]expr.Result{}}
	if cond == "" {
		return t
	}
	t.depends, _ = expr.ParseDepends(cond)
	for _, r := range t.depends.Refs() {
		if !slices.Contains(t.deps, r.Task) {
			t.deps = append(t.deps, r.Task)
		}
		if r.Result != expr.ResultAny {
			t.tests[r.Task] = append(t.tests[r.Task], r.Result)
		}
	}
	return t
}

// topoOrder sorts tasks so that each task comes after its dependencies. It keeps the spec order where it can.
func topoOrder(tasks []task) []task {
	done := map[string]bool{}
	out := make([]task, 0, len(tasks))
	for len(out) < len(tasks) {
		for _, t := range tasks {
			if done[t.name] {
				continue
			}
			ready := true
			for _, d := range t.deps {
				ready = ready && done[d]
			}
			if ready {
				done[t.name] = true
				out = append(out, t)
			}
		}
	}
	return out
}
