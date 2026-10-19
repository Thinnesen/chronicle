package chronicle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"connectrpc.com/connect"

	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

// TaskFunc runs one task. The input has the input parameters of the node. The result must encode as JSON:
// its fields become the output parameters with the same names. The context ends when the task is cancelled
// or when the worker stops after its grace period.
type TaskFunc func(ctx context.Context, task TaskInfo, input map[string]string) (any, error)

// TaskInfo describes one delivery of a task.
type TaskInfo struct {
	ID, Name, Workflow, NodeName string
	// IdempotencyKey is the same for all attempts of a node. Use it to make a repeated task harmless.
	IdempotencyKey string
}

// WorkerOptions configure a Worker.
type WorkerOptions struct {
	// Queue is the queue to poll. The default is "default".
	Queue string
	// Concurrency is the number of tasks that run at the same time. The default is 1.
	Concurrency int
	// Name identifies the worker in the task status. The default is the host name.
	Name string
	// Heartbeat is the time between two heartbeats. The default is 5 s.
	// Keep it below the heartbeatTimeout of the templates.
	Heartbeat time.Duration
	// GracePeriod is how long Run waits for running tasks after its context ends. The default is 30 s.
	GracePeriod time.Duration
}

// Worker takes tasks of one queue in one namespace and runs the registered functions.
type Worker struct {
	client    *Client
	namespace string
	opts      WorkerOptions
	funcs     map[string]TaskFunc
}

// NewWorker returns a worker. Register the task functions, then call Run.
func NewWorker(client *Client, namespace string, opts WorkerOptions) *Worker {
	if opts.Queue == "" {
		opts.Queue = "default"
	}
	opts.Concurrency = max(opts.Concurrency, 1)
	if opts.Name == "" {
		opts.Name, _ = os.Hostname()
	}
	if opts.Heartbeat <= 0 {
		opts.Heartbeat = 5 * time.Second
	}
	if opts.GracePeriod <= 0 {
		opts.GracePeriod = 30 * time.Second
	}
	return &Worker{client: client, namespace: namespace, opts: opts, funcs: map[string]TaskFunc{}}
}

// Register adds the function for a task type, the name of a task template.
func (w *Worker) Register(name string, fn TaskFunc) { w.funcs[name] = fn }

// nonRetryable marks an error that the retry strategy must not retry.
type nonRetryable struct{ error }

func (e nonRetryable) Unwrap() error { return e.error }

// NonRetryable marks an error as final: the node gets the phase Error, which the default retry policy does not retry.
func NonRetryable(err error) error { return nonRetryable{err} }

// Run polls for tasks until ctx ends. Then it stops polling and waits up to the grace period for the
// running tasks. It returns an error when it cannot poll, for example without the permission.
func (w *Worker) Run(ctx context.Context) error {
	if len(w.funcs) == 0 {
		return errors.New("register at least one task function")
	}
	names := make([]string, 0, len(w.funcs))
	for n := range w.funcs {
		names = append(names, n)
	}
	// work gets its own context, so that running tasks can finish after ctx ends.
	work, stopWork := context.WithCancel(context.WithoutCancel(ctx))
	defer stopWork()
	slots := make(chan struct{}, w.opts.Concurrency)
	var running sync.WaitGroup
	var pollErr error
	for ctx.Err() == nil {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			continue
		}
		task, err := w.poll(ctx, names)
		if err != nil || task == nil {
			<-slots
			if err != nil && ctx.Err() == nil {
				if code := connect.CodeOf(err); code == connect.CodePermissionDenied || code == connect.CodeUnauthenticated {
					pollErr = err
					break
				}
				time.Sleep(time.Second) // A short wait before the next poll after an error.
			}
			continue
		}
		running.Go(func() {
			defer func() { <-slots }()
			w.handle(work, task)
		})
	}
	finished := make(chan struct{})
	go func() { running.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(w.opts.GracePeriod):
		stopWork()
		<-finished
	}
	return pollErr
}

func (w *Worker) poll(ctx context.Context, names []string) (*chroniclev1.Task, error) {
	resp, err := w.client.Tasks.PollTask(ctx, connect.NewRequest(&chroniclev1.PollTaskRequest{
		Namespace: w.namespace, Queue: w.opts.Queue, Names: names, Worker: w.opts.Name,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.Task, nil
}

// handle runs one task with heartbeats and sends its result.
func (w *Worker) handle(ctx context.Context, t *chroniclev1.Task) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var lost atomic.Bool
	go w.heartbeat(ctx, cancel, &lost, t)
	output, failure, retryable := w.call(ctx, t)
	if lost.Load() {
		// The task was cancelled or the lease was lost: the server refuses a result.
		return
	}
	req := connect.NewRequest(&chroniclev1.CompleteTaskRequest{
		Namespace: w.namespace, Id: t.Id, Lease: t.Lease, OutputJson: output, Error: failure, Retryable: retryable,
	})
	// The result goes out also when the worker stops, so that the task does not wait for a timeout.
	_, _ = w.client.Tasks.CompleteTask(context.WithoutCancel(ctx), req)
}

// call runs the function. A panic becomes a final error.
func (w *Worker) call(ctx context.Context, t *chroniclev1.Task) (output, failure string, retryable bool) {
	defer func() {
		if r := recover(); r != nil {
			output, failure, retryable = "", fmt.Sprintf("panic in the task function: %v", r), false
		}
	}()
	var input map[string]string
	if err := json.Unmarshal([]byte(t.InputJson), &input); err != nil {
		return "", "the task input is not a JSON object: " + err.Error(), false
	}
	info := TaskInfo{ID: t.Id, Name: t.Name, Workflow: t.Workflow, NodeName: t.NodeName, IdempotencyKey: t.IdempotencyKey}
	fn, ok := w.funcs[t.Name]
	if !ok {
		return "", fmt.Sprintf("the worker has no function for the task %q", t.Name), false
	}
	result, err := fn(ctx, info, input)
	if err != nil {
		var final nonRetryable
		return "", err.Error(), !errors.As(err, &final)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", "the result does not encode as JSON: " + err.Error(), false
	}
	return string(data), "", false
}

// heartbeat sends heartbeats until ctx ends. It cancels the task when the server says so or when the lease is lost.
func (w *Worker) heartbeat(ctx context.Context, cancel context.CancelFunc, lost *atomic.Bool, t *chroniclev1.Task) {
	tick := time.NewTicker(w.opts.Heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		resp, err := w.client.Tasks.HeartbeatTask(ctx, connect.NewRequest(&chroniclev1.HeartbeatTaskRequest{
			Namespace: w.namespace, Id: t.Id, Lease: t.Lease,
		}))
		if connect.CodeOf(err) == connect.CodeFailedPrecondition || err == nil && resp.Msg.Cancelled {
			lost.Store(true)
			cancel()
			return
		}
	}
}
