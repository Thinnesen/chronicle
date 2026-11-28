package chronicle

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/gen/chronicle/v1/chroniclev1connect"
)

// taskStub gives its tasks one by one, records the results and cancels the tasks in cancel.
type taskStub struct {
	chroniclev1connect.UnimplementedTaskServiceHandler
	mu      sync.Mutex
	tasks   []*chroniclev1.Task
	results map[string]*chroniclev1.CompleteTaskRequest
	cancel  map[string]bool
	// refuse has, for each task, the codes that CompleteTask returns before it takes the result.
	refuse map[string][]connect.Code
	calls  map[string]int
}

type (
	pollReq  = connect.Request[chroniclev1.PollTaskRequest]
	pollResp = connect.Response[chroniclev1.PollTaskResponse]
	beatReq  = connect.Request[chroniclev1.HeartbeatTaskRequest]
	beatResp = connect.Response[chroniclev1.HeartbeatTaskResponse]
	doneReq  = connect.Request[chroniclev1.CompleteTaskRequest]
	doneResp = connect.Response[chroniclev1.CompleteTaskResponse]
)

func (s *taskStub) PollTask(_ context.Context, _ *pollReq) (*pollResp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.tasks) == 0 {
		return connect.NewResponse(&chroniclev1.PollTaskResponse{}), nil
	}
	t := s.tasks[0]
	s.tasks = s.tasks[1:]
	return connect.NewResponse(&chroniclev1.PollTaskResponse{Task: t}), nil
}

func (s *taskStub) HeartbeatTask(_ context.Context, r *beatReq) (*beatResp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return connect.NewResponse(&chroniclev1.HeartbeatTaskResponse{Cancelled: s.cancel[r.Msg.Id]}), nil
}

func (s *taskStub) CompleteTask(_ context.Context, r *doneReq) (*doneResp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := r.Msg.Id
	if s.calls != nil {
		s.calls[id]++
	}
	if codes := s.refuse[id]; len(codes) > 0 {
		s.refuse[id] = codes[1:]
		return nil, connect.NewError(codes[0], errors.New("refused"))
	}
	s.results[id] = r.Msg
	return connect.NewResponse(&chroniclev1.CompleteTaskResponse{}), nil
}

func (s *taskStub) result(id string) *chroniclev1.CompleteTaskRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.results[id]
}

// TestWorkerCompleteRetry checks that a transient error of CompleteTask is retried and a permanent error is not.
func TestWorkerCompleteRetry(t *testing.T) {
	stub := &taskStub{
		tasks: []*chroniclev1.Task{
			{Id: "flaky", Name: "greet", InputJson: "{}", Lease: "l"},
			{Id: "lost", Name: "greet", InputJson: "{}", Lease: "l"},
		},
		results: map[string]*chroniclev1.CompleteTaskRequest{},
		refuse: map[string][]connect.Code{
			"flaky": {connect.CodeUnavailable, connect.CodeResourceExhausted},
			"lost":  {connect.CodeFailedPrecondition, connect.CodeFailedPrecondition},
		},
		calls: map[string]int{},
	}
	mux := http.NewServeMux()
	mux.Handle(chroniclev1connect.NewTaskServiceHandler(stub))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client, err := NewClient(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	w := NewWorker(client, "team-a", WorkerOptions{Concurrency: 2, GracePeriod: 5 * time.Second})
	w.Register("greet", func(context.Context, TaskInfo, map[string]string) (any, error) { return "hi", nil })
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- w.Run(ctx) }()
	for start := time.Now(); stub.result("flaky") == nil && time.Since(start) < 5*time.Second; {
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if r := stub.results["flaky"]; r == nil || r.OutputJson != `"hi"` || stub.calls["flaky"] != 3 {
		t.Errorf("flaky: result %+v after %d calls, want the result after 3 calls", r, stub.calls["flaky"])
	}
	if stub.calls["lost"] != 1 {
		t.Errorf("lost: %d calls, want 1", stub.calls["lost"])
	}
}

// TestWorker runs four tasks: a result, a retryable error, a final error and a panic.
// A fifth task is cancelled through its heartbeat, so its context ends and no result goes out.
func TestWorker(t *testing.T) {
	task := func(id, name string) *chroniclev1.Task {
		return &chroniclev1.Task{Id: id, Name: name, InputJson: `{"who":"world"}`, Lease: "l-" + id,
			IdempotencyKey: "k", Traceparent: "tp"}
	}
	stub := &taskStub{
		tasks: []*chroniclev1.Task{
			task("ok", "greet"), task("retry", "flaky"), task("final", "broken"),
			task("panic", "panics"), task("cancel", "slow"),
		},
		results: map[string]*chroniclev1.CompleteTaskRequest{}, cancel: map[string]bool{"cancel": true},
	}
	mux := http.NewServeMux()
	mux.Handle(chroniclev1connect.NewTaskServiceHandler(stub))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client, err := NewClient(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}

	opts := WorkerOptions{Concurrency: 3, Heartbeat: 20 * time.Millisecond, GracePeriod: time.Second}
	w := NewWorker(client, "team-a", opts)
	w.Register("greet", func(_ context.Context, info TaskInfo, in map[string]string) (any, error) {
		return map[string]string{"greeting": "hello " + in["who"], "key": info.IdempotencyKey, "trace": info.Traceparent}, nil
	})
	w.Register("flaky", func(context.Context, TaskInfo, map[string]string) (any, error) {
		return nil, errors.New("the mail server timed out")
	})
	w.Register("broken", func(context.Context, TaskInfo, map[string]string) (any, error) {
		return nil, NonRetryable(errors.New("the address is not valid"))
	})
	w.Register("panics", func(context.Context, TaskInfo, map[string]string) (any, error) { panic("boom") })
	cancelled := make(chan struct{})
	w.Register("slow", func(ctx context.Context, _ TaskInfo, _ map[string]string) (any, error) {
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	})

	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- w.Run(ctx) }()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the cancelled task did not end")
	}
	stop()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	want := map[string]struct {
		output, failure string
		retryable       bool
	}{
		"ok":    {output: `{"greeting":"hello world","key":"k","trace":"tp"}`},
		"retry": {failure: "the mail server timed out", retryable: true},
		"final": {failure: "the address is not valid"},
		"panic": {failure: "panic in the task function: boom"},
	}
	if r := stub.result("cancel"); r != nil {
		t.Errorf("a cancelled task sent a result: %+v", r)
	}
	for id, w := range want {
		r := stub.result(id)
		if r == nil || r.OutputJson != w.output || r.Error != w.failure || r.Retryable != w.retryable || r.Lease != "l-"+id {
			t.Errorf("task %s: result %+v, want %+v", id, r, w)
		}
	}
}
