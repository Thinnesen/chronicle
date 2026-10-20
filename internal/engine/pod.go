package engine

import (
	"fmt"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/expr"
	"github.com/Thinnesen/chronicle/internal/manifest"
)

// runPod creates the Pod of a node or mirrors the observed Pod into the node.
func (e *engine) runPod(c call, t *v1alpha1.Template, n v1alpha1.NodeStatus) v1alpha1.NodeStatus {
	pod, observed := e.in.Pods[n.PodName]
	switch {
	case observed:
		wasDone := IsTerminal(n.Phase)
		e.applyPod(&n, pod)
		e.saveToCache(c, n, wasDone)
		if pod.Stuck && !wasDone && IsTerminal(n.Phase) {
			e.actions = append(e.actions, DeletePod{PodName: n.PodName})
		}
	case n.Phase == v1alpha1.NodePending && e.stopping:
	case n.Phase == v1alpha1.NodePending:
		if e.fromCache(c, t, &n) {
			break
		}
		if limit, full := e.noSlot(n); full {
			n.Message = fmt.Sprintf("%s (parallelism %d)", waitingForSlot, limit)
			break
		}
		if e.waitForLock(t, &n) {
			break
		}
		resolved, err := substituteTemplate(t, e.nodeScope(c, n))
		if err != nil {
			e.setPhase(&n, v1alpha1.NodeError, err.Error())
			break
		}
		n.Message = ""
		e.actions = append(e.actions, CreatePod{NodeID: n.ID, NodeName: c.nodeName, PodName: n.PodName, Template: resolved, Inputs: n.Inputs})
	default:
		e.setPhase(&n, v1alpha1.NodeError, "the Pod was deleted")
	}
	return e.save(n)
}

func (e *engine) applyPod(n *v1alpha1.NodeStatus, pod PodResult) {
	if pod.LogKey != "" {
		n.LogKey = pod.LogKey
	}
	if IsTerminal(pod.Phase) {
		n.Outputs = pod.Outputs
		n.FinishedAt = pod.FinishedAt
	}
	if pod.Phase == n.Phase && !IsTerminal(n.Phase) {
		n.Message = pod.Message
	}
	e.setPhase(n, pod.Phase, pod.Message)
}

// substituteTemplate returns a copy of a container, script, resource or http template with the placeholders replaced.
// A resource manifest becomes JSON.
func substituteTemplate(t *v1alpha1.Template, resolve expr.Resolver) (*v1alpha1.Template, error) {
	out := t.DeepCopy()
	if out.Resource != nil {
		m, err := manifest.Substitute(out.Resource.Manifest, resolve)
		out.Resource.Manifest = m
		return out, err
	}
	if h := out.HTTP; h != nil {
		fields := []*string{&h.URL, &h.Body}
		for i := range h.Headers {
			if h.Headers[i].Value != nil {
				fields = append(fields, h.Headers[i].Value)
			}
		}
		return out, substituteAll(fields, resolve)
	}
	ct := out.Container
	if out.Script != nil {
		ct = &out.Script.ContainerTemplate
	}
	fields := make([]*string, 0, 2+len(ct.Command)+len(ct.Args)+len(ct.Env))
	fields = append(fields, &ct.Image, &ct.WorkingDir)
	for i := range ct.Command {
		fields = append(fields, &ct.Command[i])
	}
	for i := range ct.Args {
		fields = append(fields, &ct.Args[i])
	}
	for i := range ct.Env {
		fields = append(fields, &ct.Env[i].Value)
	}
	if err := substituteAll(fields, resolve); err != nil {
		return nil, err
	}
	return out, nil
}

// substituteAll replaces the placeholders in each field.
func substituteAll(fields []*string, resolve expr.Resolver) error {
	for _, f := range fields {
		v, err := expr.Substitute(*f, resolve)
		if err != nil {
			return err
		}
		*f = v
	}
	return nil
}
