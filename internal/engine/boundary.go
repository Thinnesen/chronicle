package engine

import (
	"fmt"
	"strings"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/expr"
)

// task is a DAG task or a step, with its dependencies. Steps depend on all steps of the previous group.
type task struct {
	name     string
	nodeName string
	template string
	args     v1alpha1.Arguments
	deps     []string
	depends  expr.Depends
	// tests are the named results that the depends expression tests for each task, for example "a.Failed".
	tests map[string][]expr.Result
	// group is the node name of the step group, or "" for a DAG task.
	group string
}

// boundary runs the tasks of one DAG or steps node.
type boundary struct {
	e        *engine
	nodeID   string
	tasks    []task // in dependency order
	byName   map[string]*task
	failFast bool
	depth    int
}

func newBoundary(e *engine, nodeID string, tasks []task, failFast bool, depth int) *boundary {
	b := &boundary{e: e, nodeID: nodeID, tasks: tasks, byName: map[string]*task{}, failFast: failFast, depth: depth}
	for i := range tasks {
		b.byName[tasks[i].name] = &b.tasks[i]
	}
	return b
}

func (b *boundary) node(name string) (v1alpha1.NodeStatus, bool) {
	n, ok := b.e.nodes[b.e.nodeID(b.byName[name].nodeName)]
	return n, ok
}

// walk visits each task once in dependency order. It starts the tasks that are ready and
// omits the tasks whose depends expression is false. It returns the phase of the boundary.
func (b *boundary) walk() (v1alpha1.NodePhase, string) {
	stopped := b.stopReason()
	for i := range b.tasks {
		t := &b.tasks[i]
		n, ok := b.node(t.name)
		switch {
		case ok && isTerminal(n.Phase):
			continue
		case ok:
			b.start(t)
		case stopped != "" || !b.ready(t):
			continue
		case !t.depends.Eval(b.lookup):
			b.omit(t, "the depends condition is false")
		default:
			b.start(t)
		}
		if stopped == "" {
			stopped = b.stopReason()
		}
	}
	return b.outcome(stopped)
}

func (b *boundary) start(t *task) {
	n := b.e.execute(call{template: t.template, nodeName: t.nodeName, args: t.args}, b.depth+1)
	b.link(t, n.ID)
}

func (b *boundary) omit(t *task, msg string) {
	n := b.e.newNode(call{template: t.template, nodeName: t.nodeName}, v1alpha1.NodePod)
	if tmpl := b.e.templates[t.template]; tmpl.DAG != nil {
		n.Type = v1alpha1.NodeDAG
	} else if tmpl.Steps != nil {
		n.Type = v1alpha1.NodeSteps
	}
	b.e.setPhase(&n, v1alpha1.NodeOmitted, msg)
	b.e.save(n)
	b.link(t, n.ID)
}

func (b *boundary) link(t *task, id string) {
	if t.group == "" {
		b.e.addChild(b.nodeID, id)
		return
	}
	groupID := b.e.nodeID(t.group)
	if _, ok := b.e.nodes[groupID]; !ok {
		g := b.e.newNode(call{nodeName: t.group}, v1alpha1.NodeStepGroup)
		g.DisplayName = t.group[strings.LastIndex(t.group, "["):]
		g.StartedAt = &b.e.in.Now
		b.e.setPhase(&g, v1alpha1.NodeRunning, "")
		b.e.save(g)
	}
	b.e.addChild(b.nodeID, groupID)
	b.e.addChild(groupID, id)
}

func (b *boundary) ready(t *task) bool {
	for _, d := range t.deps {
		if n, ok := b.node(d); !ok || !isTerminal(n.Phase) {
			return false
		}
	}
	return true
}

func (b *boundary) lookup(name string, r expr.Result) bool {
	n, _ := b.node(name)
	switch r {
	case expr.ResultSucceeded, expr.ResultAnySucceeded:
		return n.Phase == v1alpha1.NodeSucceeded
	case expr.ResultFailed, expr.ResultAllFailed:
		return n.Phase == v1alpha1.NodeFailed
	case expr.ResultErrored:
		return n.Phase == v1alpha1.NodeError
	case expr.ResultSkipped:
		return n.Phase == v1alpha1.NodeSkipped
	case expr.ResultOmitted:
		return n.Phase == v1alpha1.NodeOmitted
	}
	return false
}

// stopReason returns why no new task may start, or "". With failFast, a failed task stops the
// boundary unless another task tests it for its result, for example "a.Failed".
func (b *boundary) stopReason() string {
	if !b.failFast {
		return ""
	}
	for _, t := range b.tasks {
		n, ok := b.node(t.name)
		if !ok || !isFailure(n.Phase) || b.handledByAny(t.name, n.Phase) {
			continue
		}
		return fmt.Sprintf("not started because task %q failed", t.name)
	}
	return ""
}

func (b *boundary) handledByAny(name string, p v1alpha1.NodePhase) bool {
	for i := range b.tasks {
		if b.tasks[i].handles(name, p) {
			return true
		}
	}
	return false
}

// handles tells if the task tests a dependency for the failure phase p.
func (t *task) handles(dep string, p v1alpha1.NodePhase) bool {
	for _, r := range t.tests[dep] {
		if p == v1alpha1.NodeFailed && (r == expr.ResultFailed || r == expr.ResultAllFailed) ||
			p == v1alpha1.NodeError && r == expr.ResultErrored {
			return true
		}
	}
	return false
}

// outcome returns Running while a task runs. Then it omits the tasks that cannot start,
// and the leaf tasks decide the phase.
func (b *boundary) outcome(stopped string) (v1alpha1.NodePhase, string) {
	var unstarted []*task
	for i := range b.tasks {
		n, ok := b.node(b.tasks[i].name)
		if ok && !isTerminal(n.Phase) {
			return v1alpha1.NodeRunning, ""
		}
		if !ok {
			unstarted = append(unstarted, &b.tasks[i])
		}
	}
	if len(unstarted) > 0 && stopped == "" {
		return v1alpha1.NodeRunning, ""
	}
	for _, t := range unstarted {
		b.omit(t, stopped)
	}
	phase, msg := v1alpha1.NodeSucceeded, ""
	memo := map[string]v1alpha1.NodePhase{}
	for _, t := range b.leaves() {
		p := b.branchPhase(t.name, memo)
		if isFailure(p) && msg == "" {
			msg = fmt.Sprintf("task %q failed", b.failedTask(t.name, memo))
		}
		phase = worse(phase, p)
	}
	return phase, msg
}

func (b *boundary) leaves() []*task {
	used := map[string]bool{}
	for _, t := range b.tasks {
		for _, d := range t.deps {
			used[d] = true
		}
	}
	var out []*task
	for i := range b.tasks {
		if !used[b.tasks[i].name] {
			out = append(out, &b.tasks[i])
		}
	}
	return out
}

// branchPhase is the outcome that a task passes down its branch: its own phase when it ran,
// or the worst outcome of its dependencies when it was skipped or omitted.
// A dependency failure that the task tests for, for example with "!a.Failed", does not pass down.
func (b *boundary) branchPhase(name string, memo map[string]v1alpha1.NodePhase) v1alpha1.NodePhase {
	if p, ok := memo[name]; ok {
		return p
	}
	t := b.byName[name]
	n, _ := b.node(name)
	p := n.Phase
	if p == v1alpha1.NodeSkipped || p == v1alpha1.NodeOmitted {
		p = v1alpha1.NodeSucceeded
		for _, d := range t.deps {
			if dn, _ := b.node(d); t.handles(d, dn.Phase) {
				continue
			}
			p = worse(p, b.branchPhase(d, memo))
		}
	}
	memo[name] = p
	return p
}

// failedTask returns the name of the task that failed first in the branch of a task.
func (b *boundary) failedTask(name string, memo map[string]v1alpha1.NodePhase) string {
	if n, _ := b.node(name); isFailure(n.Phase) {
		return name
	}
	t := b.byName[name]
	for _, d := range t.deps {
		if dn, _ := b.node(d); !t.handles(d, dn.Phase) && isFailure(b.branchPhase(d, memo)) {
			return b.failedTask(d, memo)
		}
	}
	return name
}
