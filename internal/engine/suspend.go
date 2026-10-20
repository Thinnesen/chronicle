package engine

import (
	"errors"
	"fmt"
	"slices"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// runSuspend ends a suspend node when an approval for it exists or when its duration is over.
func (e *engine) runSuspend(t *v1alpha1.Template, n v1alpha1.NodeStatus) v1alpha1.NodeStatus {
	if a := e.approval(n); a != nil {
		if a.Decision == v1alpha1.DecisionReject {
			e.setPhase(&n, v1alpha1.NodeFailed, joinMessage(decided("rejected", a), a.Message))
			return e.save(n)
		}
		if err := checkEnums(t, a.Parameters); err != nil {
			// The API refuses such a value. This catches an approval written in another way, for example with kubectl.
			e.setPhase(&n, v1alpha1.NodeFailed, err.Error())
			return e.save(n)
		}
		n.Outputs = suspendOutputs(t, a.Parameters)
		e.setPhase(&n, v1alpha1.NodeSucceeded, joinMessage(decided("approved", a), a.Message))
		return e.save(n)
	}
	if d := t.Suspend.Duration; d != nil {
		if e.reached(n.StartedAt.Add(d.Duration)) {
			n.Outputs = suspendOutputs(t, nil)
			e.setPhase(&n, v1alpha1.NodeSucceeded, "")
			return e.save(n)
		}
		n.Message = fmt.Sprintf("waiting for %s or an approval", d.Duration)
		return e.save(n)
	}
	n.Message = "waiting for an approval"
	return e.save(n)
}

func (e *engine) approval(n v1alpha1.NodeStatus) *v1alpha1.Approval {
	for i, a := range e.in.Approvals {
		if a.Node == n.ID || a.Node == n.Name {
			return &e.in.Approvals[i]
		}
	}
	return nil
}

// suspendOutputs gives each output parameter the approval value, the value or the default.
func suspendOutputs(t *v1alpha1.Template, given []v1alpha1.Parameter) *v1alpha1.Outputs {
	if len(t.Outputs.Parameters) == 0 {
		return nil
	}
	out := &v1alpha1.Outputs{}
	for _, p := range t.Outputs.Parameters {
		v := paramValue(p)
		for _, g := range given {
			if g.Name == p.Name && g.Value != nil {
				v = g.Value
			}
		}
		if v != nil {
			out.Parameters = append(out.Parameters, v1alpha1.Parameter{Name: p.Name, Value: v})
		}
	}
	return out
}

// ErrNotWaiting means that an approval names a node that is not a suspend node that waits.
var ErrNotWaiting = errors.New("the run has no suspend node with this name or ID that waits for an approval")

// CheckApproval returns ErrNotWaiting when the approved node does not wait. It returns an error when the approval gives
// a value that the template of the node does not allow. The API checks this before it saves the approval.
func CheckApproval(wf *v1alpha1.Workflow, a v1alpha1.Approval) error {
	spec := &wf.Spec.WorkflowTemplateSpec
	if wf.Status.StoredSpec != nil {
		spec = wf.Status.StoredSpec
	}
	for _, n := range wf.Status.Nodes {
		if n.Type != v1alpha1.NodeSuspend || n.Phase != v1alpha1.NodeRunning || (n.ID != a.Node && n.Name != a.Node) {
			continue
		}
		for i := range spec.Templates {
			if spec.Templates[i].Name == n.TemplateName {
				return checkEnums(&spec.Templates[i], a.Parameters)
			}
		}
		// Without the template, the values cannot be checked, so the approval fails closed.
		return fmt.Errorf("the run has no template %q for the node %s", n.TemplateName, n.Name)
	}
	return ErrNotWaiting
}

// checkEnums returns an error when an approval value is not one of the allowed values of its output parameter.
func checkEnums(t *v1alpha1.Template, given []v1alpha1.Parameter) error {
	for _, p := range t.Outputs.Parameters {
		for _, g := range given {
			if g.Name == p.Name && g.Value != nil && len(p.Enum) > 0 && !slices.Contains(p.Enum, *g.Value) {
				return fmt.Errorf("the approval value %q of %q is not one of %q", *g.Value, p.Name, p.Enum)
			}
		}
	}
	return nil
}

// decided names the decision and, when the approval has one, the user who decided.
func decided(verb string, a *v1alpha1.Approval) string {
	if a.User == "" {
		return verb
	}
	return verb + " by " + a.User
}

func joinMessage(prefix, msg string) string {
	if msg == "" {
		return prefix
	}
	return prefix + ": " + msg
}
