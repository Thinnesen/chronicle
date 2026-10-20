package engine

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/expr"
)

// runBoundary walks the tasks of a DAG or steps node. When the node finishes, it resolves the template outputs.
func (e *engine) runBoundary(t *v1alpha1.Template, n v1alpha1.NodeStatus, prefix string, tasks []task, failFast bool, depth int) v1alpha1.NodeStatus {
	b := newBoundary(e, n, prefix, tasks, failFast, depth)
	phase, msg := b.walk()
	n = e.nodes[n.ID]
	if phase == v1alpha1.NodeRunning {
		return n
	}
	if phase == v1alpha1.NodeSucceeded {
		out, err := resolveOutputs(t, b.scope(nil))
		if err != nil {
			phase, msg = v1alpha1.NodeError, err.Error()
		}
		n.Outputs = out
	}
	e.setPhase(&n, phase, msg)
	return e.save(n)
}

// resolveOutputs resolves the output parameters of a DAG or steps template. It also resolves the artifacts that the
// template passes on from its tasks or steps.
func resolveOutputs(t *v1alpha1.Template, resolve expr.Resolver) (*v1alpha1.Outputs, error) {
	if len(t.Outputs.Parameters) == 0 && len(t.Outputs.Artifacts) == 0 {
		return nil, nil
	}
	out := &v1alpha1.Outputs{}
	for _, p := range t.Outputs.Parameters {
		v, err := expr.Substitute(*p.Value, resolve)
		if err != nil {
			return nil, fmt.Errorf("output %q: %w", p.Name, err)
		}
		out.Parameters = append(out.Parameters, v1alpha1.Parameter{Name: p.Name, Value: &v})
	}
	for _, a := range t.Outputs.Artifacts {
		key, err := expr.Substitute(a.From, resolve)
		switch {
		case err == nil && key != "":
			out.Artifacts = append(out.Artifacts, v1alpha1.Artifact{Name: a.Name, Key: key})
		case !a.Optional:
			return nil, fmt.Errorf("output artifact %q: the task or step has no such artifact", a.Name)
		}
	}
	return out, nil
}

func (e *engine) runDAG(c call, t *v1alpha1.Template, n v1alpha1.NodeStatus, depth int) v1alpha1.NodeStatus {
	tasks := make([]task, 0, len(t.DAG.Tasks))
	for _, dt := range t.DAG.Tasks {
		cond := dt.Depends
		if cond == "" && len(dt.Dependencies) > 0 {
			cond = strings.Join(dt.Dependencies, " && ")
		}
		tasks = append(tasks, newTask(dt.Name, c.nodeName+"."+dt.Name, dt.Call, cond, ""))
	}
	failFast := t.DAG.FailFast == nil || *t.DAG.FailFast
	return e.runBoundary(t, n, "tasks", topoOrder(tasks), failFast, depth)
}

func (e *engine) runSteps(c call, t *v1alpha1.Template, n v1alpha1.NodeStatus, depth int) v1alpha1.NodeStatus {
	var tasks []task
	var prev []string
	for i, group := range t.Steps {
		groupName := c.nodeName + "[" + strconv.Itoa(i) + "]"
		cond := strings.Join(prev, " && ")
		prev = prev[:0:0]
		for _, s := range group {
			tasks = append(tasks, newTask(s.Name, groupName+"."+s.Name, s.Call, cond, groupName))
			prev = append(prev, s.Name)
		}
	}
	n = e.runBoundary(t, n, "steps", tasks, false, depth)
	e.updateGroups(n, tasks)
	return n
}

// updateGroups sets the phase of each step group from its steps. A failure that continues counts as success.
func (e *engine) updateGroups(steps v1alpha1.NodeStatus, tasks []task) {
	continueOn := map[string]*v1alpha1.ContinueOn{}
	for _, t := range tasks {
		continueOn[t.nodeName] = t.call.ContinueOn
	}
	for _, gid := range steps.Children {
		g := e.nodes[gid]
		// An exit hook of a step is also a child of the steps node.
		if g.Type != v1alpha1.NodeStepGroup || IsTerminal(g.Phase) {
			continue
		}
		phase, omitted := v1alpha1.NodeSucceeded, true
		for _, cid := range g.Children {
			c := e.nodes[cid]
			p := c.Phase
			if continuesOn(continueOn[c.Name], p) {
				p = v1alpha1.NodeSucceeded
			}
			if !IsTerminal(p) {
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
func newTask(name, nodeName string, c v1alpha1.Call, cond, group string) task {
	t := task{name: name, nodeName: nodeName, call: c, group: group, tests: map[string][]expr.Result{}}
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
