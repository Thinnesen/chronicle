package archive

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

func TestTries(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	wf := run("44444444-4444-4444-4444-444444444444", "retried", v1alpha1.WorkflowFailed, time.Now(), nil)
	wf.Status.Message = "first try failed"
	if err := s.Save(ctx, wf, 0, 1); err != nil {
		t.Fatal(err)
	}
	// The same try saved twice is still one try.
	if err := s.Save(ctx, wf, 0, 1); err != nil {
		t.Fatal(err)
	}
	wf.Status.Restarts, wf.Status.Phase, wf.Status.Message = 1, v1alpha1.WorkflowSucceeded, ""
	if err := s.Save(ctx, wf, 1, 1); err != nil {
		t.Fatal(err)
	}
	tries, err := s.Tries(ctx, string(wf.UID))
	if err != nil || len(tries) != 1 || tries[0].Number != 0 || tries[0].Phase != "Failed" {
		t.Fatalf("tries: %v %+v", err, tries)
	}
	old, err := s.GetTry(ctx, string(wf.UID), 0)
	if err != nil || old.Status.Message != "first try failed" || old.Status.Phase != v1alpha1.WorkflowFailed {
		t.Fatalf("try 0: %v %+v", err, old)
	}
	if _, err := s.GetTry(ctx, string(wf.UID), 5); !errors.Is(err, ErrNotFound) {
		t.Fatalf("try 5: %v, want ErrNotFound", err)
	}
	if cur, err := s.Get(ctx, "team-a", "retried"); err != nil || cur.Status.Phase != v1alpha1.WorkflowSucceeded {
		t.Fatalf("current: %v %+v", err, cur)
	}
}
