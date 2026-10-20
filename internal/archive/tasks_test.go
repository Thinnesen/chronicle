package archive

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func newTask(i int) NewTask {
	return NewTask{
		ID: fmt.Sprintf("u1/n%d", i), Namespace: "team-a", WorkflowUID: "11111111-0000-0000-0000-000000000001", Workflow: "wf",
		NodeID: fmt.Sprintf("n%d", i), NodeName: fmt.Sprintf("wf.t%d", i), IdempotencyKey: fmt.Sprintf("u1/wf.t%d", i),
		Queue: "default", Name: "greet", Input: `{"who":"world"}`,
	}
}

// TestTaskQueue covers the delivery rules. Each task has one worker. A lease covers heartbeats and results. A cancelled
// task takes no result.
func TestTaskQueue(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	for i := range 3 {
		if err := s.Enqueue(ctx, newTask(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Enqueue(ctx, newTask(0)); err != nil {
		t.Fatalf("a second enqueue must do nothing: %v", err)
	}

	// Four workers poll at the same time: each of the three tasks goes to exactly one of them.
	type taken struct{ id, lease string }
	var mu sync.Mutex
	var got []taken
	var wg sync.WaitGroup
	for w := range 4 {
		wg.Go(func() {
			task, lease, err := s.Poll(ctx, "team-a", "default", []string{"greet"}, fmt.Sprintf("w%d", w))
			if err != nil {
				t.Error(err)
			}
			if task != nil {
				mu.Lock()
				got = append(got, taken{task.ID, lease})
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if len(got) != 3 || got[0].id == got[1].id || got[1].id == got[2].id || got[0].id == got[2].id {
		t.Fatalf("taken = %+v", got)
	}
	if task, _, _ := s.Poll(ctx, "team-b", "default", []string{"greet"}, "w9"); task != nil {
		t.Fatalf("a worker of another namespace got %s", task.ID)
	}

	a, b, c := got[0], got[1], got[2]
	if err := s.Complete(ctx, "team-a", a.id, "wrong", `{}`, "", false); !errors.Is(err, ErrLeaseLost) {
		t.Errorf("a result with the wrong lease: %v", err)
	}
	if cancelled, err := s.Heartbeat(ctx, "team-a", a.id, a.lease); err != nil || cancelled {
		t.Errorf("heartbeat: %v %v", cancelled, err)
	}
	if err := s.Complete(ctx, "team-a", a.id, a.lease, `{"greeting":"hi"}`, "", false); err != nil {
		t.Fatal(err)
	}
	if err := s.Complete(ctx, "team-a", a.id, a.lease, `{}`, "", false); !errors.Is(err, ErrLeaseLost) {
		t.Errorf("a second result: %v", err)
	}

	if err := s.Cancel(ctx, b.id, "the workflow was stopped"); err != nil {
		t.Fatal(err)
	}
	if cancelled, err := s.Heartbeat(ctx, "team-a", b.id, b.lease); err != nil || !cancelled {
		t.Errorf("heartbeat after cancel: %v %v", cancelled, err)
	}
	if err := s.Complete(ctx, "team-a", b.id, b.lease, `{}`, "", false); !errors.Is(err, ErrLeaseLost) {
		t.Errorf("a result after cancel: %v", err)
	}
	if err := s.Complete(ctx, "team-a", c.id, c.lease, "", "timeout of the mail server", true); err != nil {
		t.Fatal(err)
	}

	checkList(t, s, []string{TaskFailed, TaskCancelled}, 2)
	checkList(t, s, nil, 3) // The API gives nil for "all states".
	tasks, err := s.ForWorkflow(ctx, "11111111-0000-0000-0000-000000000001")
	if err != nil || len(tasks) != 3 {
		t.Fatalf("tasks: %v %d", err, len(tasks))
	}
	states := map[string]string{}
	for _, task := range tasks {
		states[task.ID] = task.State + " " + task.Output
	}
	want := map[string]string{a.id: TaskSucceeded + ` {"greeting": "hi"}`, b.id: TaskCancelled + " ", c.id: TaskFailed + " "}
	for id, w := range want {
		if states[id] != w {
			t.Errorf("task %s = %q, want %q", id, states[id], w)
		}
	}
}

func checkList(t *testing.T, s *Store, states []string, want int) {
	t.Helper()
	if tasks, err := s.ListTasks(context.Background(), "team-a", states, 10); err != nil || len(tasks) != want {
		t.Errorf("list the tasks in %v: %v, %d tasks, want %d", states, err, len(tasks), want)
	}
}
