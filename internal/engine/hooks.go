package engine

import (
	"fmt"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// hookName returns the node name of the exit hook of a task.
func hookName(t *task) string { return t.nodeName + ".onExit" }

// done tells if a task has ended, and its exit hook too.
func (b *boundary) done(name string) bool {
	n, ok := b.node(name)
	if !ok || !IsTerminal(n.Phase) {
		return false
	}
	t := b.byName[name]
	if t.call.OnExit == nil || b.e.in.Shutdown == v1alpha1.ShutdownTerminate {
		return true
	}
	h, ok := b.e.nodes[b.e.nodeID(hookName(t))]
	return ok && IsTerminal(h.Phase)
}

// runHook starts or continues the exit hook of a task whose node has ended. A terminated run starts no hook.
func (b *boundary) runHook(t *task) {
	h := t.call.OnExit
	if h == nil || b.e.in.Shutdown == v1alpha1.ShutdownTerminate {
		return
	}
	name := hookName(t)
	if n, ok := b.e.nodes[b.e.nodeID(name)]; ok {
		if !IsTerminal(n.Phase) {
			b.e.execute(call{template: h.Template, nodeName: name}, b.depth+1)
		}
		return
	}
	// The arguments can use the outputs and the status of the task, which has ended.
	resolve := b.scope(nil)
	args, err := resolveArgs(h.Arguments, resolve)
	if err != nil {
		n := b.e.newNode(call{template: h.Template, nodeName: name}, nodeType(b.e.templates[h.Template]))
		b.e.setPhase(&n, v1alpha1.NodeError, err.Error())
		b.e.save(n)
		b.e.addChild(b.nodeID, n.ID)
		return
	}
	artifacts := resolveArtifacts(h.Arguments.Artifacts, b.e.nodes[b.nodeID].Inputs, resolve)
	n := b.e.execute(call{template: h.Template, nodeName: name, args: args, artifacts: artifacts}, b.depth+1)
	// The name tells the hooks of parallel steps apart in the UI and the CLI.
	n.DisplayName = t.name + ".onExit"
	b.e.save(n)
	b.e.addChild(b.nodeID, n.ID)
}

// hookError returns a message for the first exit hook that ended with Error, or "". A hook that ended Failed
// does not count, as in Argo.
func (b *boundary) hookError() string {
	for i := range b.tasks {
		t := &b.tasks[i]
		if t.call.OnExit == nil {
			continue
		}
		if h, ok := b.e.nodes[b.e.nodeID(hookName(t))]; ok && h.Phase == v1alpha1.NodeError {
			return fmt.Sprintf("the exit hook of task %q failed: %s", t.name, h.Message)
		}
	}
	return ""
}
