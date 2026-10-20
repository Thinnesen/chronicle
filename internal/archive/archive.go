// Package archive keeps finished runs in PostgreSQL, so that their history stays after the Workflow is deleted.
package archive

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

//go:embed migrations/*.sql
var migrations embed.FS

// ErrNotFound means that the archive has no such run.
var ErrNotFound = errors.New("the run is not in the archive")

// Store is the archive in PostgreSQL.
type Store struct {
	pool *pgxpool.Pool
}

// Open connects to PostgreSQL and applies the migrations that are missing.
func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to the archive: %w", err)
	}
	s := &Store{pool: pool}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the connections.
func (s *Store) Close() { s.pool.Close() }

// migrate applies each migration file once, in name order, in one transaction with a lock.
func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("start the migration: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	stmts := []string{
		`SELECT pg_advisory_xact_lock(4711)`,
		`CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`,
	}
	for _, q := range stmts {
		if _, err := tx.Exec(ctx, q); err != nil {
			return fmt.Errorf("prepare the migration: %w", err)
		}
	}
	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	slices.Sort(files)
	for _, f := range files {
		var done bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, f).Scan(&done); err != nil {
			return fmt.Errorf("read the migrations: %w", err)
		}
		if done {
			continue
		}
		sql, _ := migrations.ReadFile(f)
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("apply %s: %w", f, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, f); err != nil {
			return fmt.Errorf("record %s: %w", f, err)
		}
	}
	return tx.Commit(ctx)
}

// Save writes a finished run. A second save of the same run (after a retry) replaces it. Before that, the archive
// keeps the earlier try in workflow_tries, because a retry resets its failed nodes.
func (s *Store) Save(ctx context.Context, wf *v1alpha1.Workflow, nodesDone, nodesTotal int) error {
	obj := wf.DeepCopy()
	obj.ManagedFields = nil
	data, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("encode the run: %w", err)
	}
	labels := jsonObject(wf.Labels)
	template := ""
	if ref := wf.Spec.WorkflowTemplateRef; ref != nil {
		template = ref.Name
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("save the run: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `
INSERT INTO workflow_tries (uid, try, phase, finished_at, workflow)
SELECT uid, COALESCE((workflow->'status'->>'restarts')::integer, 0), phase, finished_at, workflow FROM workflow_runs
WHERE uid = $1 AND COALESCE((workflow->'status'->>'restarts')::integer, 0) < $2
ON CONFLICT (uid, try) DO NOTHING`, string(wf.UID), wf.Status.Restarts)
	if err != nil {
		return fmt.Errorf("keep the earlier try: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO workflow_runs (uid, namespace, name, phase, message, template, labels, created_at, started_at, finished_at, nodes_done, nodes_total, workflow)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (uid) DO UPDATE SET phase = $4, message = $5, labels = $7, started_at = $9, finished_at = $10,
    nodes_done = $11, nodes_total = $12, workflow = $13`,
		string(wf.UID), wf.Namespace, wf.Name, string(wf.Status.Phase), wf.Status.Message, template, labels,
		wf.CreationTimestamp.Time, timeOrNil(wf.Status.StartedAt), timeOrNil(wf.Status.FinishedAt), nodesDone, nodesTotal, data)
	if err != nil {
		return fmt.Errorf("save the run: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("save the run: %w", err)
	}
	return nil
}

// Row is the short form of an archived run.
type Row struct {
	UID, Namespace, Name, Phase, Message, Template string
	Labels                                         map[string]string
	CreatedAt                                      time.Time
	StartedAt, FinishedAt                          *time.Time
	NodesDone, NodesTotal                          int32
}

// Query selects archived runs of one namespace, newest first.
type Query struct {
	Namespace string
	// Labels must all be on the run.
	Labels map[string]string
	Phases []string
	Limit  int
	// After starts the list after this run, for the next page.
	After *Position
}

// Position is a place in the order of a list: newest first, then by name.
type Position struct {
	CreatedAt time.Time
	Name      string
}

// List returns the runs that match the query.
func (s *Store) List(ctx context.Context, q Query) ([]Row, error) {
	args := []any{q.Namespace, jsonObject(q.Labels)}
	where := `namespace = $1 AND labels @> $2`
	if len(q.Phases) > 0 {
		args = append(args, q.Phases)
		where += ` AND phase = ANY($` + strconv.Itoa(len(args)) + `)`
	}
	if q.After != nil {
		args = append(args, q.After.CreatedAt, q.After.Name)
		t, n := strconv.Itoa(len(args)-1), strconv.Itoa(len(args))
		where += ` AND (created_at < $` + t + ` OR created_at = $` + t + ` AND name > $` + n + `)`
	}
	args = append(args, q.Limit)
	rows, err := s.pool.Query(ctx, `
SELECT uid, namespace, name, phase, message, template, labels, created_at, started_at, finished_at, nodes_done, nodes_total
FROM workflow_runs WHERE `+where+` ORDER BY created_at DESC, name LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("list the archived runs: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Row, error) {
		var row Row
		var uid [16]byte
		err := r.Scan(&uid, &row.Namespace, &row.Name, &row.Phase, &row.Message, &row.Template, &row.Labels,
			&row.CreatedAt, &row.StartedAt, &row.FinishedAt, &row.NodesDone, &row.NodesTotal)
		row.UID = formatUUID(uid)
		return row, err
	})
}

// Get returns the newest archived run with the name in the namespace.
func (s *Store) Get(ctx context.Context, namespace, name string) (*v1alpha1.Workflow, error) {
	var data []byte
	err := s.pool.QueryRow(ctx, `SELECT workflow FROM workflow_runs WHERE namespace = $1 AND name = $2 ORDER BY created_at DESC LIMIT 1`,
		namespace, name).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read the archived run: %w", err)
	}
	var wf v1alpha1.Workflow
	if err := json.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("decode the archived run: %w", err)
	}
	return &wf, nil
}

// jsonObject encodes labels as a JSON object. No labels is {}, never null.
func jsonObject(m map[string]string) []byte {
	if m == nil {
		m = map[string]string{}
	}
	b, _ := json.Marshal(m)
	return b
}

func timeOrNil(t *metav1.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Time
}

func formatUUID(b [16]byte) string {
	h := fmt.Sprintf("%x", b)
	return strings.Join([]string{h[0:8], h[8:12], h[12:16], h[16:20], h[20:32]}, "-")
}

// OpenFile opens the archive with the DSN in a file, for example a mounted Secret.
func OpenFile(ctx context.Context, path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read the archive DSN: %w", err)
	}
	return Open(ctx, strings.TrimSpace(string(data)))
}
