package engine

import (
	"fmt"
	"hash/fnv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/expr"
)

// MaxChildDepth limits the nesting of child runs, so that a template that starts itself does not run forever.
const MaxChildDepth = 10

// childPollInterval is the wait before the engine asks again for a child run that the cache does not have yet.
const childPollInterval = 2 * time.Second

// ChildResult is the observed state of a child run.
type ChildResult struct {
	Phase      v1alpha1.WorkflowPhase
	Message    string
	Outputs    *v1alpha1.Outputs
	FinishedAt *metav1.Time
	// RestartGeneration is the retry that the child run was asked for, and Restarts the retry that it applied.
	// RestartRefused is the reason when it refused the retry.
	RestartGeneration, Restarts int32
	RestartRefused              string
}

// CreateWorkflow asks the controller to create the child run of a node. The create must be idempotent.
type CreateWorkflow struct {
	Name, NodeID, NodeName string
	TemplateRef            v1alpha1.WorkflowTemplateRef
	// Parameters are the resolved arguments of the child run.
	Parameters []v1alpha1.Parameter
}

func (CreateWorkflow) isAction() {}

// ShutdownWorkflow asks the controller to stop or terminate a child run.
type ShutdownWorkflow struct {
	Name string
	Mode v1alpha1.ShutdownMode
}

func (ShutdownWorkflow) isAction() {}

// RetryWorkflow asks the controller to retry the failed nodes of a child run, with this retry generation.
type RetryWorkflow struct {
	Name       string
	Generation int32
}

func (RetryWorkflow) isAction() {}

// childName returns the name of the child run of a node. It is stable for the node, and new for each retry.
func (e *engine) childName(nodeID string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(e.in.UID + "/" + e.runID(nodeID)))
	prefix := e.in.Name
	if len(prefix) > 240 {
		prefix = prefix[:240]
	}
	return fmt.Sprintf("%s-%08x", prefix, h.Sum32())
}

// runChild creates the child run of a node and mirrors the observed child run into the node.
func (e *engine) runChild(c call, t *v1alpha1.Template, n v1alpha1.NodeStatus) v1alpha1.NodeStatus {
	child, observed := e.in.Children[n.ChildWorkflow]
	switch {
	case observed && n.Phase == v1alpha1.NodePending && n.ChildWorkflow != e.childName(n.ID):
		// A retry of the parent kept the node of a failed child run, so the child run retries its failed nodes.
		e.retryChild(t, &n, child)
	case observed:
		e.applyChild(t, &n, child)
	case n.Phase == v1alpha1.NodePending && e.stopping:
	case n.Phase == v1alpha1.NodePending && e.in.Depth >= MaxChildDepth:
		e.setPhase(&n, v1alpha1.NodeError, fmt.Sprintf("the child runs are nested too deeply (more than %d)", MaxChildDepth))
	case n.Phase == v1alpha1.NodePending:
		ref, params, err := e.childCall(c, t, n)
		if err != nil {
			e.setPhase(&n, v1alpha1.NodeError, err.Error())
			break
		}
		n.Message = "starting the child run " + n.ChildWorkflow
		e.actions = append(e.actions, CreateWorkflow{Name: n.ChildWorkflow, NodeID: n.ID, NodeName: c.nodeName,
			TemplateRef: ref, Parameters: params})
		e.reached(e.in.Now.Add(childPollInterval))
	default:
		e.setPhase(&n, v1alpha1.NodeError, "the child run "+n.ChildWorkflow+" was deleted")
	}
	return e.save(n)
}

// retryChild asks a kept child run for the retry of this try, and waits until the child run has applied it.
func (e *engine) retryChild(t *v1alpha1.Template, n *v1alpha1.NodeStatus, child ChildResult) {
	want := e.in.Restarts
	switch {
	case child.RestartGeneration < want:
		if n.StartedAt == nil {
			n.StartedAt = &e.in.Now
		}
		n.Message = "retrying the child run " + n.ChildWorkflow
		e.actions = append(e.actions, RetryWorkflow{Name: n.ChildWorkflow, Generation: want})
		e.reached(e.in.Now.Add(childPollInterval))
	case child.Restarts < want:
		e.reached(e.in.Now.Add(childPollInterval))
	case child.RestartRefused != "":
		e.setPhase(n, v1alpha1.NodeError, fmt.Sprintf("the child run %s refused the retry: %s", n.ChildWorkflow, child.RestartRefused))
	default:
		e.applyChild(t, n, child)
	}
}

// childCall resolves the placeholders in the template name and in the arguments of the child run.
func (e *engine) childCall(c call, t *v1alpha1.Template, n v1alpha1.NodeStatus) (v1alpha1.WorkflowTemplateRef, []v1alpha1.Parameter, error) {
	resolve := e.nodeScope(c, n)
	ref := t.ChildWorkflow.WorkflowTemplateRef
	name, err := expr.Substitute(ref.Name, resolve)
	if err != nil {
		return ref, nil, err
	}
	ref.Name = name
	params := make([]v1alpha1.Parameter, 0, len(t.ChildWorkflow.Arguments.Parameters))
	for _, p := range t.ChildWorkflow.Arguments.Parameters {
		if p.Value == nil {
			return ref, nil, fmt.Errorf("the argument %q of the child run has no value", p.Name)
		}
		v, err := expr.Substitute(*p.Value, resolve)
		if err != nil {
			return ref, nil, err
		}
		params = append(params, v1alpha1.Parameter{Name: p.Name, Value: &v})
	}
	return ref, params, nil
}

// applyChild sets the phase of a node from its child run, and copies the outputs of a child run that succeeded.
func (e *engine) applyChild(t *v1alpha1.Template, n *v1alpha1.NodeStatus, child ChildResult) {
	switch child.Phase {
	case v1alpha1.WorkflowSucceeded:
		n.FinishedAt = child.FinishedAt
		out, err := childOutputs(t, child.Outputs)
		if err != nil {
			e.setPhase(n, v1alpha1.NodeFailed, err.Error())
			return
		}
		n.Outputs = out
		e.setPhase(n, v1alpha1.NodeSucceeded, "")
	case v1alpha1.WorkflowFailed, v1alpha1.WorkflowError:
		n.FinishedAt = child.FinishedAt
		e.setPhase(n, v1alpha1.NodePhase(child.Phase), fmt.Sprintf("the child run %s %s: %s", n.ChildWorkflow, child.Phase, child.Message))
	case v1alpha1.WorkflowRunning, v1alpha1.WorkflowSuspended:
		e.setPhase(n, v1alpha1.NodeRunning, "")
	default:
		e.setPhase(n, v1alpha1.NodePending, child.Message)
	}
}

// childOutputs takes the output parameters of a template from the global outputs of its child run.
func childOutputs(t *v1alpha1.Template, child *v1alpha1.Outputs) (*v1alpha1.Outputs, error) {
	values := map[string]string{}
	if child != nil {
		for _, p := range child.Parameters {
			if p.Value != nil {
				values[p.Name] = *p.Value
			}
		}
	}
	out := &v1alpha1.Outputs{}
	for _, p := range t.Outputs.Parameters {
		v, ok := values[p.Name]
		if !ok {
			if p.Default == nil {
				return nil, fmt.Errorf("the child run has no output parameter %q", p.Name)
			}
			v = *p.Default
		}
		out.Parameters = append(out.Parameters, v1alpha1.Parameter{Name: p.Name, Value: &v})
	}
	return out, nil
}

// shutdownMode is the shutdown of a child run when the engine stops its node.
func (e *engine) shutdownMode() v1alpha1.ShutdownMode {
	if e.in.Shutdown == v1alpha1.ShutdownTerminate {
		return v1alpha1.ShutdownTerminate
	}
	return v1alpha1.ShutdownStop
}
