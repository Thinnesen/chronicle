package engine

import (
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

const taskSpec = `
entrypoint: main
templates:
- name: main
  dag:
    tasks:
    - {name: a, template: t, arguments: {parameters: [{name: who, value: world}]}}
    - {name: b, template: c, depends: a, arguments: {parameters: [{name: x, value: "{{tasks.a.outputs.parameters.greeting}}"}]}}
- name: t
  inputs: {parameters: [{name: who}]}
  outputs: {parameters: [{name: greeting}]}
  retryStrategy: {limit: 1}
  task: {name: greet, heartbeatTimeout: 10s, scheduleToStartTimeout: 1m}
- name: c
  inputs: {parameters: [{name: x}]}
  container: {image: x, command: [echo, "{{inputs.parameters.x}}"]}
`

func taskInputFor(t *testing.T, spec string) Input {
	t.Helper()
	var s v1alpha1.WorkflowTemplateSpec
	if err := yaml.UnmarshalStrict([]byte(spec), &s); err != nil {
		t.Fatal(err)
	}
	return Input{Name: wfName, Namespace: "ns", UID: "uid", Spec: &s, Pods: map[string]PodResult{},
		Tasks: map[string]TaskResult{}, TasksEnabled: true, Now: at(0)}
}

// next runs the engine and keeps the status. It returns the actions of one type.
func next[T Action](in *Input, step int) ([]T, Result) {
	in.Now = at(step)
	r := Run(*in)
	in.Status = r.Status
	var out []T
	for _, a := range r.Actions {
		if x, ok := a.(T); ok {
			out = append(out, x)
		}
	}
	return out, r
}

// TestTaskRetryAfterHeartbeatTimeout covers at-least-once delivery: a silent worker fails the attempt,
// the retry gets a new task with the same idempotency key, and its result flows to the next task.
func TestTaskRetryAfterHeartbeatTimeout(t *testing.T) {
	in := taskInputFor(t, taskSpec)
	first, r := next[EnqueueTask](&in, 0)
	if len(first) != 1 || first[0].Input != `{"who":"world"}` || first[0].Name != "greet" || first[0].Queue != "default" ||
		first[0].IdempotencyKey != "uid/wf.a" || r.RequeueAfter == 0 {
		t.Fatalf("enqueue = %+v, requeue %s", first, r.RequeueAfter)
	}
	start := at(1)
	in.Tasks[first[0].TaskID] = TaskResult{Phase: v1alpha1.NodeRunning, StartedAt: &start}
	if _, r := next[EnqueueTask](&in, 5); r.Status.Phase != v1alpha1.WorkflowRunning {
		t.Fatalf("phase = %s", r.Status.Phase)
	}
	var retry []EnqueueTask
	var cancels []CancelTask
	for step := 12; step < 16 && len(retry) == 0; step++ {
		c, _ := next[CancelTask](&in, step)
		cancels = append(cancels, c...)
		retry, _ = next[EnqueueTask](&in, step)
	}
	if len(cancels) != 1 || cancels[0].TaskID != first[0].TaskID || !strings.Contains(cancels[0].Reason, "no heartbeat") {
		t.Fatalf("cancels = %+v", cancels)
	}
	if len(retry) != 1 || retry[0].TaskID == first[0].TaskID || retry[0].IdempotencyKey != first[0].IdempotencyKey {
		t.Fatalf("retry = %+v", retry)
	}
	done := at(20)
	in.Tasks[retry[0].TaskID] = TaskResult{Phase: v1alpha1.NodeSucceeded, Output: `{"greeting":"hello world"}`, FinishedAt: &done}
	pods, _ := next[CreatePod](&in, 21)
	if len(pods) != 1 || strings.Join(pods[0].Template.Container.Command, " ") != "echo hello world" {
		t.Fatalf("pods = %+v", pods)
	}
}

func TestTaskOutcomes(t *testing.T) {
	const single = "entrypoint: t\ntemplates:\n- {name: t, outputs: {parameters: [{name: p}]}, task: {name: x, scheduleToStartTimeout: 30s}}"
	tests := []struct {
		name    string
		enabled bool
		task    *TaskResult
		step    int
		phase   v1alpha1.NodePhase
		msg     string
	}{
		{name: "no task queue", phase: v1alpha1.NodeError, msg: "need the archive database"},
		{name: "no worker in time", enabled: true, task: &TaskResult{Phase: v1alpha1.NodePending}, step: 31,
			phase: v1alpha1.NodeFailed, msg: "no worker took the task in 30s"},
		{name: "result without the output field", enabled: true, task: &TaskResult{Phase: v1alpha1.NodeSucceeded, Output: `"plain"`},
			step: 1, phase: v1alpha1.NodeFailed, msg: `no field "p"`},
		{name: "worker error", enabled: true, task: &TaskResult{Phase: v1alpha1.NodeError, Message: "panic in the worker"},
			step: 1, phase: v1alpha1.NodeError, msg: "panic in the worker"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := taskInputFor(t, single)
			in.TasksEnabled = tt.enabled
			enq, _ := next[EnqueueTask](&in, 0)
			if tt.task != nil {
				in.Tasks[enq[0].TaskID] = *tt.task
				next[EnqueueTask](&in, tt.step)
			}
			n := nodeByName(in.Status, wfName)
			if n.Phase != tt.phase || !strings.Contains(n.Message, tt.msg) {
				t.Errorf("node = %s %q", n.Phase, n.Message)
			}
		})
	}
}

func TestTaskStopCancels(t *testing.T) {
	in := taskInputFor(t, "entrypoint: t\ntemplates:\n- {name: t, task: {name: x}}")
	enq, _ := next[EnqueueTask](&in, 0)
	start := metav1.NewTime(at(0).Add(time.Second))
	in.Tasks[enq[0].TaskID] = TaskResult{Phase: v1alpha1.NodeRunning, StartedAt: &start}
	in.Shutdown = v1alpha1.ShutdownTerminate
	cancels, r := next[CancelTask](&in, 2)
	if len(cancels) != 1 || cancels[0].TaskID != enq[0].TaskID || r.Status.Phase != v1alpha1.WorkflowFailed {
		t.Errorf("cancels %+v, phase %s", cancels, r.Status.Phase)
	}
}

func TestRetryKey(t *testing.T) {
	for in, want := range map[string]string{"wf.a(1)": "wf.a", "wf.a(0:x)": "wf.a(0:x)", "wf.a": "wf.a", "wf.a(2:x)(3)": "wf.a(2:x)"} {
		if got := retryKey(in); got != want {
			t.Errorf("retryKey(%q) = %q, want %q", in, got, want)
		}
	}
}
