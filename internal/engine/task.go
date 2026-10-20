package engine

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// taskPollInterval is the wait between two checks of a task that has not finished. The controller reads
// the task queue only when the engine runs, so the engine asks to run again.
const taskPollInterval = 2 * time.Second

// MaxTaskResult is the largest task result that a node keeps in its status.
const MaxTaskResult = 64 << 10

// TaskResult is the observed state of the task of a node in the task queue.
type TaskResult struct {
	// Phase is Pending (no worker has it), Running, Succeeded, Failed or Error.
	Phase   v1alpha1.NodePhase
	Message string
	// Output is the JSON result of a succeeded task.
	Output     string
	StartedAt  *metav1.Time
	FinishedAt *metav1.Time
	// HeartbeatAt is the time of the last heartbeat of the worker.
	HeartbeatAt *metav1.Time
}

// EnqueueTask asks the controller to put the task of a node into the queue. The enqueue must be idempotent.
type EnqueueTask struct {
	TaskID   string
	NodeID   string
	NodeName string
	// IdempotencyKey is the same for all attempts of a node, so that a worker can detect a repeated task.
	IdempotencyKey string
	Name, Queue    string
	// Input is a JSON object with the input parameters of the node.
	Input string
}

func (EnqueueTask) isAction() {}

// CancelTask asks the controller to cancel a task. A worker that sends a heartbeat learns it, and its result is refused.
type CancelTask struct {
	TaskID string
	Reason string
}

func (CancelTask) isAction() {}

// runTask enqueues the task of a node and mirrors the observed task into the node.
func (e *engine) runTask(c call, t *v1alpha1.Template, n v1alpha1.NodeStatus) v1alpha1.NodeStatus {
	if !e.in.TasksEnabled {
		e.setPhase(&n, v1alpha1.NodeError, "task templates need the archive database")
		return e.save(n)
	}
	task, observed := e.in.Tasks[n.TaskID]
	switch {
	case !observed && n.Phase == v1alpha1.NodePending && e.stopping:
	case !observed && n.Phase == v1alpha1.NodePending && e.waitForLock(t, &n):
	case !observed && n.Phase == v1alpha1.NodePending:
		n.Message = ""
		e.actions = append(e.actions, EnqueueTask{
			TaskID: n.TaskID, NodeID: n.ID, NodeName: c.nodeName, IdempotencyKey: e.in.UID + "/" + retryKey(c.nodeName),
			Name: t.Task.Name, Queue: queueOf(t.Task), Input: taskInput(n.Inputs),
		})
		e.reached(e.in.Now.Add(taskPollInterval))
	case !observed:
		e.setPhase(&n, v1alpha1.NodeError, "the task is not in the queue")
	case IsTerminal(task.Phase):
		e.finishTask(t, &n, task)
	default:
		e.watchTask(t, &n, task)
	}
	return e.save(n)
}

// defaultHeartbeatTimeout fails a task whose worker went silent, when the template sets no heartbeatTimeout. The SDK
// workers send a heartbeat each 5 s, so 1 minute is 12 lost heartbeats.
const defaultHeartbeatTimeout = time.Minute

// watchTask checks the timeouts of a task that has not finished.
func (e *engine) watchTask(t *v1alpha1.Template, n *v1alpha1.NodeStatus, task TaskResult) {
	spec := t.Task
	if task.Phase == v1alpha1.NodePending {
		if d := spec.ScheduleToStartTimeout; d != nil && e.reached(n.StartedAt.Add(d.Duration)) {
			e.failTask(n, fmt.Sprintf("no worker took the task in %s", d.Duration))
			return
		}
		e.setPhase(n, v1alpha1.NodePending, "waiting for a worker of the queue "+queueOf(spec))
	} else {
		last := task.StartedAt
		if task.HeartbeatAt != nil {
			last = task.HeartbeatAt
		}
		d := defaultHeartbeatTimeout
		if spec.HeartbeatTimeout != nil {
			d = spec.HeartbeatTimeout.Duration
		}
		if last != nil && e.reached(last.Add(d)) {
			e.failTask(n, fmt.Sprintf("the worker sent no heartbeat for %s", d))
			return
		}
		e.setPhase(n, v1alpha1.NodeRunning, "")
	}
	e.reached(e.in.Now.Add(taskPollInterval))
}

func (e *engine) failTask(n *v1alpha1.NodeStatus, msg string) {
	e.actions = append(e.actions, CancelTask{TaskID: n.TaskID, Reason: msg})
	e.setPhase(n, v1alpha1.NodeFailed, msg)
}

// finishTask copies the result of a finished task into the node. The fields of a result object become
// the output parameters with the same names.
func (e *engine) finishTask(t *v1alpha1.Template, n *v1alpha1.NodeStatus, task TaskResult) {
	n.FinishedAt = task.FinishedAt
	if task.Phase != v1alpha1.NodeSucceeded {
		e.setPhase(n, task.Phase, task.Message)
		return
	}
	if len(task.Output) > MaxTaskResult {
		e.setPhase(n, v1alpha1.NodeFailed, fmt.Sprintf("the task result is larger than %d KiB", MaxTaskResult>>10))
		return
	}
	out, err := taskOutputs(t, task.Output)
	if err != nil {
		e.setPhase(n, v1alpha1.NodeFailed, err.Error())
		return
	}
	n.Outputs = out
	e.setPhase(n, v1alpha1.NodeSucceeded, "")
}

func taskOutputs(t *v1alpha1.Template, output string) (*v1alpha1.Outputs, error) {
	var value any
	if err := json.Unmarshal([]byte(output), &value); err != nil {
		return nil, fmt.Errorf("the task result is not JSON: %w", err)
	}
	result := output
	if s, ok := value.(string); ok {
		result = s
	}
	out := &v1alpha1.Outputs{Result: &result}
	fields, _ := value.(map[string]any)
	for _, p := range t.Outputs.Parameters {
		v, ok := fields[p.Name]
		if !ok {
			if p.Default == nil {
				return nil, fmt.Errorf("the task result has no field %q for the output parameter", p.Name)
			}
			v = *p.Default
		}
		s, isString := v.(string)
		if !isString {
			b, _ := json.Marshal(v)
			s = string(b)
		}
		out.Parameters = append(out.Parameters, v1alpha1.Parameter{Name: p.Name, Value: &s})
	}
	return out, nil
}

// taskInput returns the input parameters of a node as a JSON object.
func taskInput(inputs *v1alpha1.Inputs) string {
	m := map[string]string{}
	if inputs != nil {
		for _, p := range inputs.Parameters {
			m[p.Name] = *p.Value
		}
	}
	b, _ := json.Marshal(m) // Marshal sorts the keys, so the input is deterministic.
	return string(b)
}

func queueOf(t *v1alpha1.TaskTemplate) string {
	if t.Queue == "" {
		return "default"
	}
	return t.Queue
}

// retryKey removes the attempt suffix "(n)" of a node name, so that all attempts share a key.
// An item suffix such as "(0:x)" stays, because each item is a different task.
func retryKey(nodeName string) string {
	open := strings.LastIndexByte(nodeName, '(')
	if open < 0 || !strings.HasSuffix(nodeName, ")") {
		return nodeName
	}
	if _, err := strconv.Atoi(nodeName[open+1 : len(nodeName)-1]); err != nil {
		return nodeName
	}
	return nodeName[:open]
}
