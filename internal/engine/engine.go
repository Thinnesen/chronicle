// Package engine is the pure workflow engine. It has no I/O and no clock.
// It takes the spec, the status and the observed Pods, and it returns the new status and the actions to apply.
package engine

import (
	"errors"
	"fmt"
	"time"

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
	// CreatedAt is the creation time of the Workflow. ScheduledTime is the schedule time of a cron run, or empty.
	CreatedAt     metav1.Time
	ScheduledTime string
	// Labels and Annotations are the metadata of the Workflow, for {{workflow.labels.<key>}} and
	// {{workflow.annotations.<key>}}.
	Labels, Annotations map[string]string
	// Spec is the resolved workflow definition.
	Spec *v1alpha1.WorkflowTemplateSpec
	// Status is the status from the last run of the engine.
	Status v1alpha1.WorkflowStatus
	// Pods are the observed Pods, by Pod name.
	Pods map[string]PodResult
	// Tasks are the observed tasks in the task queue, by task ID. TasksEnabled is false without a task queue.
	Tasks        map[string]TaskResult
	TasksEnabled bool
	// Restarts is the number of retries of the workflow. New Pods get it in their name, so that a Pod
	// of an earlier try can never stand for a new node.
	Restarts int32
	// Suspend, Shutdown and Approvals are the run controls from the Workflow spec.
	Suspend   bool
	Shutdown  v1alpha1.ShutdownMode
	Approvals []v1alpha1.Approval
	// Now is the current time.
	Now metav1.Time
}

// PodResult is the observed state of the Pod of a node.
type PodResult struct {
	Phase      v1alpha1.NodePhase
	Message    string
	FinishedAt *metav1.Time
	Outputs    *v1alpha1.Outputs
	// LogKey is the key of the archived log.
	LogKey string
}

// Result is the new status and the actions that the controller must apply.
type Result struct {
	Status  v1alpha1.WorkflowStatus
	Actions []Action
	// RequeueAfter is the time until the engine must run again for a timer (backoff, timeout, deadline). Zero means no timer.
	RequeueAfter time.Duration
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
	// Template is the container or script template with all placeholders replaced.
	Template *v1alpha1.Template
	// Inputs are the resolved input parameters.
	Inputs *v1alpha1.Inputs
}

func (CreatePod) isAction() {}

// DeletePod asks the controller to delete the Pod of a stopped node.
type DeletePod struct {
	PodName string
}

func (DeletePod) isAction() {}

// Run runs one step of the engine. The same input always gives the same result.
func Run(in Input) Result {
	status := *in.Status.DeepCopy()
	if status.Nodes == nil {
		status.Nodes = map[string]v1alpha1.NodeStatus{}
	}
	e := newEngine(&in, status.Nodes)
	if !isFinished(status.Phase) {
		e.step(&status)
		status.Outputs = e.workflowOutputs()
	}
	e.actions = append(e.actions, e.gc(&status)...)
	return Result{Status: status, Actions: e.actions, RequeueAfter: e.requeueAfter()}
}

// step runs the workflow once and sets its phase.
func (e *engine) step(status *v1alpha1.WorkflowStatus) {
	now := e.in.Now
	if status.StartedAt == nil {
		status.StartedAt = &now
	}
	if errs := validate.Spec(e.in.Spec, true); len(errs) > 0 {
		finish(status, v1alpha1.WorkflowError, "invalid spec: "+errs.ToAggregate().Error(), now)
		return
	}
	e.startedAt = status.StartedAt.Time
	phase, msg := e.runWorkflow()
	switch {
	case phase == v1alpha1.NodeRunning && e.in.Suspend:
		status.Phase = v1alpha1.WorkflowSuspended
	case phase == v1alpha1.NodeRunning:
		status.Phase = v1alpha1.WorkflowRunning
	default:
		finish(status, workflowPhase(phase), msg, now)
	}
}

// runWorkflow runs the entrypoint, the deadline and the exit handler. It returns Running until all are done.
func (e *engine) runWorkflow() (v1alpha1.NodePhase, string) {
	root, ok := e.nodes[e.in.Name]
	if !ok && e.in.Suspend {
		return v1alpha1.NodeRunning, ""
	}
	root = e.execute(call{template: e.in.Spec.Entrypoint, nodeName: e.in.Name, args: e.workflowArgs()}, 0)
	if !isTerminal(root.Phase) {
		if reason := e.stopReason(); reason != "" {
			e.stop(root.ID, reason)
			root = e.nodes[root.ID]
		}
	}
	if !isTerminal(root.Phase) {
		return v1alpha1.NodeRunning, ""
	}
	phase, msg := outcomePhase(root.Phase), root.Message
	exitName := e.in.Name + ".onExit"
	if e.in.Spec.OnExit == "" || e.in.Shutdown == v1alpha1.ShutdownTerminate {
		e.stop(e.nodeID(exitName), "the workflow was terminated")
		return phase, msg
	}
	if _, started := e.nodes[e.nodeID(exitName)]; !started && e.in.Suspend {
		return v1alpha1.NodeRunning, ""
	}
	e.status = string(workflowPhase(phase))
	exit := e.execute(call{template: e.in.Spec.OnExit, nodeName: exitName, args: e.workflowArgs()}, 0)
	if !isTerminal(exit.Phase) {
		return v1alpha1.NodeRunning, ""
	}
	if p := outcomePhase(exit.Phase); worse(phase, p) != phase {
		return p, "the exit handler failed: " + exit.Message
	}
	return phase, msg
}

// stopReason returns why the entrypoint must stop now, or "".
func (e *engine) stopReason() string {
	switch e.in.Shutdown {
	case v1alpha1.ShutdownStop:
		return "the workflow was stopped"
	case v1alpha1.ShutdownTerminate:
		return "the workflow was terminated"
	}
	if d := e.in.Spec.ActiveDeadlineSeconds; d != nil && e.reached(e.startedAt.Add(time.Duration(*d)*time.Second)) {
		return "the workflow deadline was exceeded"
	}
	return ""
}

// outcomePhase maps a finished node phase to Succeeded, Failed or Error.
func outcomePhase(p v1alpha1.NodePhase) v1alpha1.NodePhase {
	if isFailure(p) {
		return p
	}
	return v1alpha1.NodeSucceeded
}

func workflowPhase(p v1alpha1.NodePhase) v1alpha1.WorkflowPhase {
	switch p {
	case v1alpha1.NodeFailed:
		return v1alpha1.WorkflowFailed
	case v1alpha1.NodeError:
		return v1alpha1.WorkflowError
	}
	return v1alpha1.WorkflowSucceeded
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
	// args are the resolved argument values.
	args map[string]string
	// artifacts are the keys of the argument artifacts.
	artifacts map[string]string
	// attempt is true for one attempt of a retry node, and retry is its number, from 0.
	attempt bool
	retry   int
}

type engine struct {
	in        *Input
	nodes     map[string]v1alpha1.NodeStatus
	templates map[string]*v1alpha1.Template
	actions   []Action
	// status is "{{workflow.status}}": Running, then the outcome of the entrypoint for the exit handler.
	status string
	// wakeAt is the earliest timer, or nil.
	wakeAt *time.Time
	// startedAt is the start time of the workflow.
	startedAt time.Time
}

func newEngine(in *Input, nodes map[string]v1alpha1.NodeStatus) *engine {
	e := &engine{in: in, nodes: nodes, templates: map[string]*v1alpha1.Template{}, status: string(v1alpha1.WorkflowRunning)}
	for i := range in.Spec.Templates {
		e.templates[in.Spec.Templates[i].Name] = &in.Spec.Templates[i]
	}
	return e
}

// workflowArgs gives the workflow parameters to the entrypoint template.
func (e *engine) workflowArgs() map[string]string {
	args := map[string]string{}
	for _, p := range e.in.Spec.Arguments.Parameters {
		if v := paramValue(p); v != nil {
			args[p.Name] = *v
		}
	}
	return args
}

// execute starts or continues the node of a call and returns the node.
func (e *engine) execute(c call, depth int) v1alpha1.NodeStatus {
	id := e.nodeID(c.nodeName)
	n, exists := e.nodes[id]
	if exists && isTerminal(n.Phase) {
		return n
	}
	t := e.templates[c.template]
	if !exists {
		var ok bool
		if n, ok = e.startNode(c, t, depth); !ok {
			return n
		}
	}
	if t.RetryStrategy != nil && n.Type == v1alpha1.NodeRetry {
		return e.runRetry(c, t, n, depth)
	}
	n = e.runBody(c, t, n, depth)
	// The timeout is checked after the body, so that a Pod that finished in time counts.
	if t.Timeout != nil && !isTerminal(n.Phase) && e.reached(n.StartedAt.Add(t.Timeout.Duration)) {
		e.stop(n.ID, fmt.Sprintf("the timeout of %s was exceeded", t.Timeout.Duration))
		n = e.nodes[n.ID]
	}
	return n
}

// runBody runs a node of a DAG, steps, container or script template.
func (e *engine) runBody(c call, t *v1alpha1.Template, n v1alpha1.NodeStatus, depth int) v1alpha1.NodeStatus {
	switch {
	case t.Suspend != nil:
		return e.runSuspend(t, n)
	case t.DAG != nil:
		return e.runDAG(c, t, n, depth)
	case t.Steps != nil:
		return e.runSteps(c, t, n, depth)
	case t.Task != nil:
		return e.runTask(c, t, n)
	default:
		return e.runPod(c, t, n)
	}
}

// startNode creates the node of a call. It returns false when the node failed at once.
func (e *engine) startNode(c call, t *v1alpha1.Template, depth int) (v1alpha1.NodeStatus, bool) {
	n := e.newNode(c, nodeType(t))
	if t.RetryStrategy != nil && !c.attempt {
		n.Type = v1alpha1.NodeRetry
	}
	inputs, err := e.resolveInputs(t, c)
	n.StartedAt = &e.in.Now
	switch {
	case depth > maxDepth:
		err = errors.New("the templates are nested too deeply")
	case err == nil && n.Type == v1alpha1.NodePod:
		n.Phase = v1alpha1.NodePending
		n.PodName = e.runID(n.ID)
	case err == nil && n.Type == v1alpha1.NodeTask:
		n.Phase = v1alpha1.NodePending
		n.TaskID = e.in.UID + "/" + e.runID(n.ID)
	case err == nil:
		e.setPhase(&n, v1alpha1.NodeRunning, "")
	}
	n.Inputs = inputs
	if err != nil {
		e.setPhase(&n, v1alpha1.NodeError, err.Error())
	}
	return e.save(n), err == nil
}

// runID returns the name of the Pod or the task of a node. After a retry of the workflow it has a suffix,
// so that a Pod or a task of an earlier try can never stand for a new node.
func (e *engine) runID(nodeID string) string {
	if e.in.Restarts > 0 {
		return fmt.Sprintf("%s-r%d", nodeID, e.in.Restarts)
	}
	return nodeID
}

func nodeType(t *v1alpha1.Template) v1alpha1.NodeType {
	switch {
	case t.Suspend != nil:
		return v1alpha1.NodeSuspend
	case t.DAG != nil:
		return v1alpha1.NodeDAG
	case t.Steps != nil:
		return v1alpha1.NodeSteps
	case t.Task != nil:
		return v1alpha1.NodeTask
	}
	return v1alpha1.NodePod
}
