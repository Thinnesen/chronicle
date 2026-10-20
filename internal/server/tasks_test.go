package server

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/gen/chronicle/v1/chroniclev1connect"
	"github.com/Thinnesen/chronicle/internal/archive"
	"github.com/Thinnesen/chronicle/internal/auth"
	"github.com/Thinnesen/chronicle/internal/engine"
)

// fakeTasks has one pending task and records the calls.
type fakeTasks struct {
	pending   *archive.Task
	completed string
}

func (f *fakeTasks) Poll(_ context.Context, namespace, _ string, names []string, _ string) (*archive.Task, string, error) {
	t := f.pending
	f.pending = nil
	return t, "lease-1", nil
}

func (f *fakeTasks) Heartbeat(_ context.Context, _, _, lease string) (bool, error) {
	if lease != "lease-1" {
		return false, archive.ErrLeaseLost
	}
	return false, nil
}

func (f *fakeTasks) ListTasks(context.Context, string, []string, int) ([]archive.Task, error) {
	return []archive.Task{{ID: "u/n1", Name: "greet", State: archive.TaskRunning}}, nil
}

func (f *fakeTasks) Complete(_ context.Context, _, id, lease, output, failure string, retryable bool) error {
	if lease != "lease-1" {
		return archive.ErrLeaseLost
	}
	f.completed = id + " " + output
	if failure != "" {
		f.completed = fmt.Sprintf("%s failed (retryable %t): %s", id, retryable, failure)
	}
	return nil
}

func TestTaskService(t *testing.T) {
	setup(t)
	ctx := context.Background()
	for _, o := range []client.Object{
		&rbacv1.Role{Name: "task-worker", Namespace: "team-a", Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{"chronicle.thinnesen.com"}, Resources: []string{"workflows/tasks"}, Verbs: []string{"update"},
		}}},
		&rbacv1.RoleBinding{Name: "worker", Namespace: "team-a",
			RoleRef:  rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: "task-worker"},
			Subjects: []rbacv1.Subject{{APIGroup: "rbac.authorization.k8s.io", Kind: "User", Name: "worker"}}},
	} {
		if err := admin.Create(ctx, o); client.IgnoreAlreadyExists(err) != nil {
			t.Fatal(err)
		}
	}
	store := &fakeTasks{pending: &archive.Task{ID: "u/n1", Name: "greet", Input: `{"who":"x"}`, IdempotencyKey: "u/wf.a"}}
	h, err := NewHandler(Options{Config: cfg, Scheme: scheme, Authenticators: []auth.Authenticator{headerAuth{}}, Tasks: store})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := chroniclev1connect.NewTaskServiceClient(srv.Client(), srv.URL)
	as := func(user string, req connect.AnyRequest) { req.Header().Set("X-Test-User", user) }

	poll := connect.NewRequest(&chroniclev1.PollTaskRequest{Namespace: "team-a", Names: []string{"greet"}, Worker: "w1"})
	as("worker", poll)
	got, err := c.PollTask(ctx, poll)
	if err != nil || got.Msg.Task.GetLease() != "lease-1" || got.Msg.Task.InputJson != `{"who":"x"}` || got.Msg.Task.IdempotencyKey != "u/wf.a" {
		t.Fatalf("poll: %v %+v", err, got)
	}
	beat := connect.NewRequest(&chroniclev1.HeartbeatTaskRequest{Namespace: "team-a", Id: "u/n1", Lease: "lease-1"})
	as("worker", beat)
	if _, err := c.HeartbeatTask(ctx, beat); err != nil {
		t.Fatal(err)
	}
	stale := connect.NewRequest(&chroniclev1.CompleteTaskRequest{Namespace: "team-a", Id: "u/n1", Lease: "old", OutputJson: `{}`})
	as("worker", stale)
	if _, err := c.CompleteTask(ctx, stale); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("a stale lease: %v", err)
	}
	// During a shutdown, a poll without a task returns at once instead of after the long poll.
	drain := make(chan struct{})
	close(drain)
	dh, err := NewHandler(Options{Config: cfg, Scheme: scheme, Authenticators: []auth.Authenticator{headerAuth{}}, Tasks: &fakeTasks{}, Drain: drain})
	if err != nil {
		t.Fatal(err)
	}
	dsrv := httptest.NewServer(dh)
	t.Cleanup(dsrv.Close)
	idle := connect.NewRequest(&chroniclev1.PollTaskRequest{Namespace: "team-a", Names: []string{"greet"}})
	as("worker", idle)
	start := time.Now()
	if got, err := chroniclev1connect.NewTaskServiceClient(dsrv.Client(), dsrv.URL).PollTask(ctx, idle); err != nil || got.Msg.Task != nil || time.Since(start) > 5*time.Second {
		t.Errorf("a poll during a shutdown: %v %+v after %s", err, got, time.Since(start))
	}

	done := connect.NewRequest(&chroniclev1.CompleteTaskRequest{Namespace: "team-a", Id: "u/n1", Lease: "lease-1", OutputJson: `{"ok":true}`})
	as("worker", done)
	if _, err := c.CompleteTask(ctx, done); err != nil || store.completed != `u/n1 {"ok":true}` {
		t.Fatalf("complete: %v %q", err, store.completed)
	}
	// A result that is too large fails the task, so its node does not wait forever. The worker gets the error too.
	big := connect.NewRequest(&chroniclev1.CompleteTaskRequest{Namespace: "team-a", Id: "u/n1", Lease: "lease-1",
		OutputJson: `"` + strings.Repeat("x", engine.MaxTaskResult) + `"`})
	as("worker", big)
	if _, err := c.CompleteTask(ctx, big); connect.CodeOf(err) != connect.CodeInvalidArgument ||
		store.completed != "u/n1 failed (retryable false): the result of the task is larger than 64 KiB: return less, or save large data as an artifact" {
		t.Fatalf("a large result: %v %q", err, store.completed)
	}

	denied := connect.NewRequest(&chroniclev1.PollTaskRequest{Namespace: "team-a", Names: []string{"greet"}})
	as("bob", denied)
	if _, err := c.PollTask(ctx, denied); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("bob: %v", err)
	}
	list := connect.NewRequest(&chroniclev1.ListTasksRequest{Namespace: "team-a"})
	as("alice", list)
	if got, err := c.ListTasks(ctx, list); err != nil || len(got.Msg.Tasks) != 1 || got.Msg.Tasks[0].State != "running" {
		t.Errorf("list: %v %+v", err, got)
	}
	empty := connect.NewRequest(&chroniclev1.PollTaskRequest{Namespace: "team-a"})
	as("worker", empty)
	if _, err := c.PollTask(ctx, empty); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("no names: %v", err)
	}
}
