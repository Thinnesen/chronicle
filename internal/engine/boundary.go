package engine

import (
	"fmt"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/expr"
)

// task is a DAG task or a step, with its dependencies. Steps depend on all steps of the previous group.
type task struct {
	name     string
	nodeName string
	call     v1alpha1.Call
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
	inputs   *v1alpha1.Inputs
	prefix   string // "tasks" or "steps", for output placeholders
	tasks    []task // in dependency order
	byName   map[string]*task
	failFast bool
	depth    int
}

func newBoundary(e *engine, n v1alpha1.NodeStatus, prefix string, tasks []task, failFast bool, depth int) *boundary {
	b := &boundary{e: e, nodeID: n.ID, inputs: n.Inputs, prefix: prefix, tasks: tasks, byName: map[string]*task{},
		failFast: failFast, depth: depth}
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
		case !ok && (stopped != "" || b.e.in.Suspend || !b.ready(t)):
			continue
		case !ok && !t.depends.Eval(b.lookup):
			b.finishEarly(t, t.nodeName, v1alpha1.NodeOmitted, "the depends condition is false", b.nodeID)
		case t.call.FanOut():
			b.runGroup(t)
		default:
			b.runOne(t, t.nodeName, nil, b.nodeID)
		}
		if stopped == "" {
			stopped = b.stopReason()
		}
	}
	return b.outcome(stopped)
}

// runOne starts or continues one node of a task. item is the fan-out item, or nil.
func (b *boundary) runOne(t *task, nodeName string, item *apiextensionsv1.JSON, parentID string) {
	if n, ok := b.e.nodes[b.e.nodeID(nodeName)]; ok {
		n = b.e.execute(call{template: t.call.Template, nodeName: nodeName}, b.depth+1)
		b.link(t, parentID, n.ID)
		return
	}
	resolve := b.scope(item)
	if t.call.When != "" {
		w, _ := expr.ParseWhen(t.call.When)
		run, err := w.Eval(resolve)
		if err != nil {
			b.finishEarly(t, nodeName, v1alpha1.NodeError, "when: "+err.Error(), parentID)
			return
		}
		if !run {
			b.finishEarly(t, nodeName, v1alpha1.NodeSkipped, "the when condition is false", parentID)
			return
		}
	}
	args, err := resolveArgs(t.call.Arguments, resolve)
	if err != nil {
		b.finishEarly(t, nodeName, v1alpha1.NodeError, err.Error(), parentID)
		return
	}
	artifacts := resolveArtifacts(t.call.Arguments.Artifacts, b.e.nodes[b.nodeID].Inputs, resolve)
	n := b.e.execute(call{template: t.call.Template, nodeName: nodeName, args: args, artifacts: artifacts}, b.depth+1)
	b.link(t, parentID, n.ID)
}

// finishEarly records a node that never runs: Omitted, Skipped, or Error before the start.
func (b *boundary) finishEarly(t *task, nodeName string, p v1alpha1.NodePhase, msg, parentID string) {
	n := b.e.newNode(call{template: t.call.Template, nodeName: nodeName}, nodeType(b.e.templates[t.call.Template]))
	b.e.setPhase(&n, p, msg)
	b.e.save(n)
	b.link(t, parentID, n.ID)
}

// link adds a node to its parent: the boundary (through the step group) or a task group.
func (b *boundary) link(t *task, parentID, id string) {
	if parentID != b.nodeID {
		b.e.addChild(parentID, id)
		return
	}
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
	case expr.ResultAnySucceeded:
		return b.anyChild(n, v1alpha1.NodeSucceeded)
	case expr.ResultAllFailed:
		return b.allChildren(n, v1alpha1.NodeFailed)
	case expr.ResultSucceeded:
		// A failure with continueOn counts as success for the tasks after it, as in Argo.
		return n.Phase == v1alpha1.NodeSucceeded || b.continues(name, n.Phase)
	case expr.ResultFailed:
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
		if !ok || !isFailure(n.Phase) || b.handledByAny(t.name, n.Phase) || b.continues(t.name, n.Phase) {
			continue
		}
		return fmt.Sprintf("not started because task %q failed", t.name)
	}
	return ""
}

// continues tells if the run goes on after the task failed with the phase p, because of its continueOn.
func (b *boundary) continues(name string, p v1alpha1.NodePhase) bool {
	return continuesOn(b.byName[name].call.ContinueOn, p)
}

func continuesOn(c *v1alpha1.ContinueOn, p v1alpha1.NodePhase) bool {
	return c != nil && (p == v1alpha1.NodeFailed && c.Failed || p == v1alpha1.NodeError && c.Error)
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
		b.finishEarly(t, t.nodeName, v1alpha1.NodeOmitted, stopped, b.nodeID)
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
	if b.continues(name, p) {
		p = v1alpha1.NodeSucceeded
	}
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
	if n, _ := b.node(name); isFailure(n.Phase) && !b.continues(name, n.Phase) {
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
