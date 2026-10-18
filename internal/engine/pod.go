package engine

import (
	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/expr"
)

// runPod creates the Pod of a node or mirrors the observed Pod into the node.
func (e *engine) runPod(c call, t *v1alpha1.Template, n v1alpha1.NodeStatus) v1alpha1.NodeStatus {
	pod, observed := e.in.Pods[n.ID]
	switch {
	case observed:
		e.applyPod(&n, pod)
	case n.Phase == v1alpha1.NodePending:
		resolved, err := substituteTemplate(t, e.inputScope(n.Inputs))
		if err != nil {
			e.setPhase(&n, v1alpha1.NodeError, err.Error())
			break
		}
		e.actions = append(e.actions, CreatePod{NodeID: n.ID, NodeName: c.nodeName, PodName: n.PodName, Template: resolved, Inputs: n.Inputs})
	default:
		e.setPhase(&n, v1alpha1.NodeError, "the Pod was deleted")
	}
	return e.save(n)
}

func (e *engine) applyPod(n *v1alpha1.NodeStatus, pod PodResult) {
	if isTerminal(pod.Phase) {
		n.Outputs = pod.Outputs
		n.FinishedAt = pod.FinishedAt
	}
	if pod.Phase == n.Phase && !isTerminal(n.Phase) {
		n.Message = pod.Message
	}
	e.setPhase(n, pod.Phase, pod.Message)
}

// substituteTemplate returns a copy of a container or script template with the placeholders replaced.
func substituteTemplate(t *v1alpha1.Template, resolve expr.Resolver) (*v1alpha1.Template, error) {
	out := t.DeepCopy()
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
	for _, f := range fields {
		v, err := expr.Substitute(*f, resolve)
		if err != nil {
			return nil, err
		}
		*f = v
	}
	return out, nil
}
