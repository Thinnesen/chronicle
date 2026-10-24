package archive

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// The states of a task in the queue.
const (
	TaskPending   = "pending"
	TaskRunning   = "running"
	TaskSucceeded = "succeeded"
	TaskFailed    = "failed"
	TaskError     = "error"
	TaskCancelled = "cancelled"
)

// ErrLeaseLost means that the worker no longer has the task: it timed out, was cancelled or finished.
var ErrLeaseLost = errors.New("the task is no longer yours: it timed out, was cancelled or has a result")

// NewTask is a task to put into the queue.
type NewTask struct {
	ID, Namespace, WorkflowUID, Workflow string
	NodeID, NodeName, IdempotencyKey     string
	Queue, Name                          string
	// Input is a JSON object.
	Input string
	// Traceparent is the W3C trace context of the node, or empty without traces.
	Traceparent string
}

// Task is a task in the queue.
type Task struct {
	NewTask
	State, Message, Worker string
	// Output is the JSON result, or empty.
	Output                             string
	CreatedAt                          time.Time
	StartedAt, HeartbeatAt, FinishedAt *time.Time
}

const taskColumns = `id, namespace, workflow_uid::text, workflow, node_id, node_name, idempotency_key, queue, name, input::text, traceparent,
	state, message, worker, coalesce(output::text, ''), created_at, started_at, heartbeat_at, finished_at`

func scanTask(row pgx.Row) (Task, error) {
	var t Task
	err := row.Scan(&t.ID, &t.Namespace, &t.WorkflowUID, &t.Workflow, &t.NodeID, &t.NodeName, &t.IdempotencyKey,
		&t.Queue, &t.Name, &t.Input, &t.Traceparent, &t.State, &t.Message, &t.Worker, &t.Output, &t.CreatedAt,
		&t.StartedAt, &t.HeartbeatAt, &t.FinishedAt)
	return t, err
}

// Enqueue puts a task into the queue. A second enqueue of the same ID does nothing.
func (s *Store) Enqueue(ctx context.Context, t NewTask) error {
	// The notification wakes the long polls of the queue. PostgreSQL sends it at the commit.
	_, err := s.pool.Exec(ctx, `WITH added AS (
		INSERT INTO tasks (id, namespace, workflow_uid, workflow, node_id, node_name, idempotency_key, queue, name, input, traceparent)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11) ON CONFLICT (id) DO NOTHING RETURNING namespace, queue)
		SELECT pg_notify('`+channelTaskQueued+`', namespace || '/' || queue) FROM added`,
		t.ID, t.Namespace, t.WorkflowUID, t.Workflow, t.NodeID, t.NodeName, t.IdempotencyKey, t.Queue, t.Name, t.Input, t.Traceparent)
	if err != nil {
		return fmt.Errorf("enqueue the task %s: %w", t.ID, err)
	}
	return nil
}

// Cancel cancels a task that has no result yet. Its worker learns it with the next heartbeat.
func (s *Store) Cancel(ctx context.Context, id, reason string) error {
	_, err := s.pool.Exec(ctx, `UPDATE tasks SET state = $2, message = $3, finished_at = now(), lease = ''
		WHERE id = $1 AND state IN ($4, $5)`, id, TaskCancelled, reason, TaskPending, TaskRunning)
	if err != nil {
		return fmt.Errorf("cancel the task %s: %w", id, err)
	}
	return nil
}

// ForWorkflow returns the tasks of a run.
func (s *Store) ForWorkflow(ctx context.Context, workflowUID string) ([]Task, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+taskColumns+` FROM tasks WHERE workflow_uid = $1`, workflowUID)
	if err != nil {
		return nil, fmt.Errorf("read the tasks: %w", err)
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("read the tasks: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// OpenRun is a run that has open tasks.
type OpenRun struct {
	Namespace, Workflow, WorkflowUID string
}

// OpenRuns returns each run that has a pending or a running task, one time.
func (s *Store) OpenRuns(ctx context.Context) ([]OpenRun, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT namespace, workflow, workflow_uid::text FROM tasks WHERE state IN ($1, $2)`,
		TaskPending, TaskRunning)
	if err != nil {
		return nil, fmt.Errorf("read the runs with open tasks: %w", err)
	}
	defer rows.Close()
	var out []OpenRun
	for rows.Next() {
		var r OpenRun
		if err := rows.Scan(&r.Namespace, &r.Workflow, &r.WorkflowUID); err != nil {
			return nil, fmt.Errorf("read the runs with open tasks: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CancelRun cancels all tasks of a run that have no result yet. The workers learn it with the next heartbeat.
func (s *Store) CancelRun(ctx context.Context, workflowUID, reason string) error {
	_, err := s.pool.Exec(ctx, `UPDATE tasks SET state = $2, message = $3, finished_at = now(), lease = ''
		WHERE workflow_uid = $1 AND state IN ($4, $5)`, workflowUID, TaskCancelled, reason, TaskPending, TaskRunning)
	if err != nil {
		return fmt.Errorf("cancel the tasks of the run %s: %w", workflowUID, err)
	}
	return nil
}

// Poll gives the oldest pending task of the queue with one of the names to a worker. It returns a nil task
// when there is none. The lease is the secret that the worker must give for heartbeats and the result.
func (s *Store) Poll(ctx context.Context, namespace, queue string, names []string, worker string) (*Task, string, error) {
	lease := randomLease()
	row := s.pool.QueryRow(ctx, `UPDATE tasks SET state = $5, lease = $6, worker = $4, started_at = now(), heartbeat_at = now()
		WHERE id = (SELECT id FROM tasks WHERE state = $7 AND namespace = $1 AND queue = $2 AND name = ANY($3)
			ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1)
		RETURNING `+taskColumns, namespace, queue, names, worker, TaskRunning, lease, TaskPending)
	t, err := scanTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("take a task: %w", err)
	}
	return &t, lease, nil
}

// Heartbeat records that the worker still runs the task. It returns true when the task was cancelled.
func (s *Store) Heartbeat(ctx context.Context, namespace, id, lease string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE tasks SET heartbeat_at = now() WHERE id = $1 AND namespace = $2 AND state = $3 AND lease = $4`,
		id, namespace, TaskRunning, lease)
	if err != nil {
		return false, fmt.Errorf("record the heartbeat: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return false, nil
	}
	var state string
	err = s.pool.QueryRow(ctx, `SELECT state FROM tasks WHERE id = $1 AND namespace = $2`, id, namespace).Scan(&state)
	if err == nil && state == TaskCancelled {
		return true, nil
	}
	return false, ErrLeaseLost
}

// Complete stores the result of a task. A worker without the current lease gets ErrLeaseLost.
// With an error message, the task fails; retryable false makes it an Error, which a retry policy can treat apart.
func (s *Store) Complete(ctx context.Context, namespace, id, lease, output, failure string, retryable bool) error {
	state, out := TaskSucceeded, &output
	switch {
	case failure != "" && retryable:
		state, out = TaskFailed, nil
	case failure != "":
		state, out = TaskError, nil
	case !json.Valid([]byte(output)):
		return fmt.Errorf("the result is not JSON")
	}
	// The notification wakes the controller, so that the run goes on at once.
	var stored int
	err := s.pool.QueryRow(ctx, `WITH done AS (
		UPDATE tasks SET state = $5, output = $6::jsonb, message = $7, finished_at = now(), lease = ''
		WHERE id = $1 AND namespace = $2 AND state = $3 AND lease = $4 RETURNING namespace, workflow)
		SELECT count(pg_notify('`+channelTaskDone+`', namespace || '/' || workflow)) FROM done`,
		id, namespace, TaskRunning, lease, state, out, failure).Scan(&stored)
	if err != nil {
		return fmt.Errorf("store the result: %w", err)
	}
	if stored == 0 {
		return ErrLeaseLost
	}
	return nil
}

func randomLease() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ListTasks returns the newest tasks of a namespace, optionally only with the given states.
func (s *Store) ListTasks(ctx context.Context, namespace string, states []string, limit int) ([]Task, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+taskColumns+` FROM tasks
		WHERE namespace = $1 AND (COALESCE(cardinality($2::text[]), 0) = 0 OR state = ANY($2)) ORDER BY created_at DESC LIMIT $3`,
		namespace, states, limit)
	if err != nil {
		return nil, fmt.Errorf("list the tasks: %w", err)
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("list the tasks: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
