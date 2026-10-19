package server

import (
	"context"
	"errors"
	"sync"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"
	authorizationv1 "k8s.io/api/authorization/v1"

	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/internal/archive"
	"github.com/Thinnesen/chronicle/internal/auth"
	"github.com/Thinnesen/chronicle/internal/engine"
)

// TaskStore is the task queue that the workers use. The archive database implements it.
type TaskStore interface {
	Poll(ctx context.Context, namespace, queue string, names []string, worker string) (*archive.Task, string, error)
	Heartbeat(ctx context.Context, namespace, id, lease string) (bool, error)
	Complete(ctx context.Context, namespace, id, lease, output, failure string, retryable bool) error
	ListTasks(ctx context.Context, namespace string, states []string, limit int) ([]archive.Task, error)
}

const (
	// longPoll is the longest wait of PollTask, and pollEvery the wait between two reads of the queue.
	longPoll  = 30 * time.Second
	pollEvery = time.Second
	// workerCacheTTL is how long the server keeps an access decision for a worker.
	workerCacheTTL = 30 * time.Second
	maxTaskNames   = 100
	maxListedTasks = 200
)

// taskService implements the TaskService API.
type taskService struct {
	workflows *workflowService
	store     TaskStore

	mu      sync.Mutex
	allowed map[string]time.Time // identity and namespace, to the end of the decision
}

func (s *taskService) PollTask(ctx context.Context, req *connect.Request[chroniclev1.PollTaskRequest]) (*connect.Response[chroniclev1.PollTaskResponse], error) {
	m := req.Msg
	if len(m.Names) == 0 || len(m.Names) > maxTaskNames {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("give between 1 and 100 task names"))
	}
	if err := s.authorize(ctx, m.Namespace); err != nil {
		return nil, err
	}
	queue := m.Queue
	if queue == "" {
		queue = "default"
	}
	ctx, cancel := context.WithTimeout(ctx, longPoll)
	defer cancel()
	for {
		task, lease, err := s.store.Poll(ctx, m.Namespace, queue, m.Names, m.Worker)
		if err != nil && ctx.Err() == nil {
			return nil, connect.NewError(connect.CodeUnavailable, err)
		}
		if task != nil {
			return connect.NewResponse(&chroniclev1.PollTaskResponse{Task: &chroniclev1.Task{
				Id: task.ID, Name: task.Name, InputJson: task.Input, IdempotencyKey: task.IdempotencyKey,
				Workflow: task.Workflow, NodeName: task.NodeName, Lease: lease,
			}}), nil
		}
		select {
		case <-ctx.Done():
			return connect.NewResponse(&chroniclev1.PollTaskResponse{}), nil
		case <-time.After(pollEvery):
		}
	}
}

func (s *taskService) HeartbeatTask(ctx context.Context, req *connect.Request[chroniclev1.HeartbeatTaskRequest]) (*connect.Response[chroniclev1.HeartbeatTaskResponse], error) {
	m := req.Msg
	if err := s.authorize(ctx, m.Namespace); err != nil {
		return nil, err
	}
	cancelled, err := s.store.Heartbeat(ctx, m.Namespace, m.Id, m.Lease)
	if err != nil {
		return nil, taskError(err)
	}
	return connect.NewResponse(&chroniclev1.HeartbeatTaskResponse{Cancelled: cancelled}), nil
}

func (s *taskService) CompleteTask(ctx context.Context, req *connect.Request[chroniclev1.CompleteTaskRequest]) (*connect.Response[chroniclev1.CompleteTaskResponse], error) {
	m := req.Msg
	if err := s.authorize(ctx, m.Namespace); err != nil {
		return nil, err
	}
	if len(m.OutputJson) > engine.MaxTaskResult || len(m.Error) > engine.MaxTaskResult {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("the result is larger than 64 KiB"))
	}
	if err := s.store.Complete(ctx, m.Namespace, m.Id, m.Lease, m.OutputJson, m.Error, m.Retryable); err != nil {
		return nil, taskError(err)
	}
	return connect.NewResponse(&chroniclev1.CompleteTaskResponse{}), nil
}

func (s *taskService) ListTasks(ctx context.Context, req *connect.Request[chroniclev1.ListTasksRequest]) (*connect.Response[chroniclev1.ListTasksResponse], error) {
	m := req.Msg
	c, err := s.workflows.client(ctx, m.Namespace)
	if err != nil {
		return nil, err
	}
	if !allowed(ctx, c, m.Namespace, "list") {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("you cannot list runs in this namespace"))
	}
	limit := int(m.Limit)
	if limit <= 0 || limit > maxListedTasks {
		limit = maxListedTasks
	}
	tasks, err := s.store.ListTasks(ctx, m.Namespace, m.States, limit)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, err)
	}
	out := make([]*chroniclev1.TaskSummary, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, &chroniclev1.TaskSummary{
			Id: t.ID, Name: t.Name, Queue: t.Queue, State: t.State, Workflow: t.Workflow, NodeName: t.NodeName,
			Worker: t.Worker, Message: t.Message, CreatedAt: timestamppb.New(t.CreatedAt),
			StartedAt: optionalTime(t.StartedAt), FinishedAt: optionalTime(t.FinishedAt),
		})
	}
	return connect.NewResponse(&chroniclev1.ListTasksResponse{Tasks: out}), nil
}

func optionalTime(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func taskError(err error) error {
	if errors.Is(err, archive.ErrLeaseLost) {
		return connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewError(connect.CodeUnavailable, err)
}

// authorize checks, as the caller, the permission "update workflows/tasks" in the namespace.
func (s *taskService) authorize(ctx context.Context, namespace string) error {
	id := auth.IdentityFrom(ctx)
	if id == nil {
		return connect.NewError(connect.CodeUnauthenticated, errors.New("log in first"))
	}
	key := cacheKey(id) + "\x00" + namespace
	s.mu.Lock()
	until, ok := s.allowed[key]
	s.mu.Unlock()
	if ok && time.Now().Before(until) {
		return nil
	}
	c, err := s.workflows.client(ctx, namespace)
	if err != nil {
		return err
	}
	review := &authorizationv1.SelfSubjectAccessReview{Spec: authorizationv1.SelfSubjectAccessReviewSpec{
		ResourceAttributes: &authorizationv1.ResourceAttributes{
			Namespace: namespace, Verb: "update", Group: "chronicle.thinnesen.com", Resource: "workflows", Subresource: "tasks",
		},
	}}
	if err := c.Create(ctx, review); err != nil {
		return connect.NewError(connect.CodeUnavailable, err)
	}
	if !review.Status.Allowed {
		return connect.NewError(connect.CodePermissionDenied, errors.New("you cannot run tasks in this namespace: you need update on workflows/tasks"))
	}
	s.mu.Lock()
	now := time.Now()
	for k, end := range s.allowed {
		if now.After(end) {
			delete(s.allowed, k)
		}
	}
	s.allowed[key] = now.Add(workerCacheTTL)
	s.mu.Unlock()
	return nil
}
