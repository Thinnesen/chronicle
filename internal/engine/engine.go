// Package engine is the pure workflow engine. It has no I/O and no clock.
// It takes the spec, the status and the observed Pods, and it returns the new status and the actions to apply.
package engine

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/validate"
)

// maxDepth limits the nesting of DAG and steps templates, so that a recursive template cannot grow without end.
const maxDepth = 50

// Input is everything that the engine reads.
type Input struct {
	// Name, Namespace and UID identify the Workflow.
	Name, Namespace, UID string
	// Spec is the resolved workflow definition.
	Spec *v1alpha1.WorkflowTemplateSpec
	// Status is the status from the last run of the engine.
	Status v1alpha1.WorkflowStatus
	// Pods are the observed Pods, by node ID.
	Pods map[string]PodResult
	// Now is the current time.
	Now metav1.Time
}

// PodResult is the observed state of the Pod of a node.
type PodResult struct {
	Phase      v1alpha1.NodePhase
	Message    string
	StartedAt  *metav1.Time
	FinishedAt *metav1.Time
	Outputs    *v1alpha1.Outputs
}

// Result is the new status and the actions that the controller must apply.
type Result struct {
	Status  v1alpha1.WorkflowStatus
	Actions []Action
}

// Action is a change that the controller applies to the cluster.
type Action interface {
	isAction()
}

// CreatePod asks the controller to create the Pod of a node. The create must be idempotent.
type CreatePod struct {
	NodeID   string
	NodeName string
	PodName  string
	Template *v1alpha1.Template
	// Arguments are the parameters given to the template.
	Arguments v1alpha1.Arguments
}

func (CreatePod) isAction() {}

// Run runs one step of the engine. The same input always gives the same result.
func Run(in Input) Result {
	status := *in.Status.DeepCopy()
	if isFinished(status.Phase) {
		return Result{Status: status}
	}
	if status.StartedAt == nil {
		status.StartedAt = &in.Now
	}
	if errs := validate.Spec(in.Spec, true); len(errs) > 0 {
		finish(&status, v1alpha1.WorkflowError, "invalid spec: "+errs.ToAggregate().Error(), in.Now)
		return Result{Status: status}
	}
	if status.Nodes == nil {
		status.Nodes = map[string]v1alpha1.NodeStatus{}
	}
	e := newEngine(&in, status.Nodes)
	root := e.execute(call{template: in.Spec.Entrypoint, nodeName: in.Name, args: in.Spec.Arguments}, 0)
	switch root.Phase {
	case v1alpha1.NodeSucceeded, v1alpha1.NodeSkipped, v1alpha1.NodeOmitted:
		finish(&status, v1alpha1.WorkflowSucceeded, "", in.Now)
	case v1alpha1.NodeFailed:
		finish(&status, v1alpha1.WorkflowFailed, root.Message, in.Now)
	case v1alpha1.NodeError:
		finish(&status, v1alpha1.WorkflowError, root.Message, in.Now)
	default:
		status.Phase = v1alpha1.WorkflowRunning
	}
	return Result{Status: status, Actions: e.actions}
}

func isFinished(p v1alpha1.WorkflowPhase) bool {
	return p == v1alpha1.WorkflowSucceeded || p == v1alpha1.WorkflowFailed || p == v1alpha1.WorkflowError
}

func finish(status *v1alpha1.WorkflowStatus, phase v1alpha1.WorkflowPhase, msg string, now metav1.Time) {
	status.Phase = phase
	status.Message = msg
	status.FinishedAt = &now
}

// call is one call of a template.
type call struct {
	template string
	nodeName string
	args     v1alpha1.Arguments
}

type engine struct {
	in        *Input
	nodes     map[string]v1alpha1.NodeStatus
	templates map[string]*v1alpha1.Template
	actions   []Action
}

func newEngine(in *Input, nodes map[string]v1alpha1.NodeStatus) *engine {
	e := &engine{in: in, nodes: nodes, templates: map[string]*v1alpha1.Template{}}
	for i := range in.Spec.Templates {
		e.templates[in.Spec.Templates[i].Name] = &in.Spec.Templates[i]
	}
	return e
}

// execute starts or continues the node of a call and returns the node.
func (e *engine) execute(c call, depth int) v1alpha1.NodeStatus {
	id := e.nodeID(c.nodeName)
	if n, ok := e.nodes[id]; ok && isTerminal(n.Phase) {
		return n
	}
	t := e.templates[c.template]
	switch {
	case depth > maxDepth:
		n := e.newNode(c, v1alpha1.NodePod)
		e.setPhase(&n, v1alpha1.NodeError, "the templates are nested too deeply")
		return e.save(n)
	case t.DAG != nil:
		return e.runDAG(c, t.DAG, depth)
	case t.Steps != nil:
		return e.runSteps(c, t.Steps, depth)
	default:
		return e.runPod(c, t)
	}
}

// runPod creates the Pod of a node or mirrors the observed Pod into the node.
func (e *engine) runPod(c call, t *v1alpha1.Template) v1alpha1.NodeStatus {
	id := e.nodeID(c.nodeName)
	n, ok := e.nodes[id]
	if !ok {
		n = e.newNode(c, v1alpha1.NodePod)
		n.Phase = v1alpha1.NodePending
		n.PodName = id
	}
	pod, observed := e.in.Pods[id]
	switch {
	case observed:
		e.applyPod(&n, pod)
	case n.Phase == v1alpha1.NodePending:
		e.actions = append(e.actions, CreatePod{NodeID: id, NodeName: c.nodeName, PodName: n.PodName, Template: t, Arguments: c.args})
	default:
		e.setPhase(&n, v1alpha1.NodeError, "the Pod was deleted")
	}
	return e.save(n)
}

func (e *engine) applyPod(n *v1alpha1.NodeStatus, pod PodResult) {
	if pod.StartedAt != nil {
		n.StartedAt = pod.StartedAt
	}
	if pod.Phase == v1alpha1.NodeRunning && n.StartedAt == nil {
		n.StartedAt = &e.in.Now
	}
	if isTerminal(pod.Phase) {
		n.Outputs = pod.Outputs
		n.FinishedAt = pod.FinishedAt
	}
	e.setPhase(n, pod.Phase, pod.Message)
}
