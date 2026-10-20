package archive

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

func TestPrune(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	now := time.Now()
	old := run("55555555-5555-5555-5555-555555555555", "old", v1alpha1.WorkflowFailed, now.Add(-48*time.Hour), nil)
	fresh := run("66666666-6666-6666-6666-666666666666", "fresh", v1alpha1.WorkflowSucceeded, now, nil)
	for _, wf := range []*v1alpha1.Workflow{old, fresh} {
		if err := s.Save(ctx, wf, 1, 1); err != nil {
			t.Fatal(err)
		}
	}
	old.Status.Restarts = 1 // a retried run: its first try is in workflow_tries
	if err := s.Save(ctx, old, 1, 1); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`UPDATE workflow_runs SET finished_at = now() - interval '48 hours' WHERE name = 'old'`,
		`UPDATE workflow_runs SET finished_at = now() WHERE name = 'fresh'`,
		`INSERT INTO tasks (id, namespace, workflow_uid, workflow, node_id, node_name, idempotency_key, queue, name, input, state, finished_at)
		 VALUES ('done', 'team-a', '55555555-5555-5555-5555-555555555555', 'old', 'n', 'n', 'k', 'q', 'x', '{}', 'succeeded', now() - interval '48 hours'),
		        ('open', 'team-a', '55555555-5555-5555-5555-555555555555', 'old', 'n', 'n', 'k', 'q', 'x', '{}', 'pending', NULL)`,
		`INSERT INTO step_cache (namespace, key, outputs, workflow, node, saved_at) VALUES ('team-a', 'stale', '{}', 'old', 'n', now() - interval '48 hours'),
		        ('team-a', 'new', '{}', 'fresh', 'n', now())`,
	} {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.Prune(ctx, now.Add(-24*time.Hour), 1)
	if err != nil || n != 3 {
		t.Fatalf("Prune = %d, %v; want 3 rows (one run, one task, one cache entry)", n, err)
	}
	var left []string
	rows, _ := s.pool.Query(ctx, `SELECT name FROM workflow_runs UNION ALL SELECT id FROM tasks UNION ALL SELECT key FROM step_cache
		UNION ALL SELECT 'try-'||try FROM workflow_tries ORDER BY 1`)
	for rows.Next() {
		var x string
		_ = rows.Scan(&x)
		left = append(left, x)
	}
	if want := "fresh new open"; strings.Join(left, " ") != want {
		t.Errorf("left %q, want %q", strings.Join(left, " "), want)
	}
}
