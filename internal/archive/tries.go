package archive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// Try is an earlier try of a run that was retried. Number is the retry count of the run during the try: 0 is the
// first try.
type Try struct {
	Number     int32
	Phase      string
	FinishedAt *time.Time
}

// Tries lists the earlier tries of a run, oldest first. uid is the UID of the run, because a new run can have the
// name of a deleted one.
func (s *Store) Tries(ctx context.Context, uid string) ([]Try, error) {
	rows, err := s.pool.Query(ctx, `SELECT try, phase, finished_at FROM workflow_tries WHERE uid = $1 ORDER BY try`, uid)
	if err != nil {
		return nil, fmt.Errorf("list the tries: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Try, error) {
		var t Try
		err := r.Scan(&t.Number, &t.Phase, &t.FinishedAt)
		return t, err
	})
}

// GetTry returns an earlier try of a run.
func (s *Store) GetTry(ctx context.Context, uid string, number int32) (*v1alpha1.Workflow, error) {
	var data []byte
	err := s.pool.QueryRow(ctx, `SELECT workflow FROM workflow_tries WHERE uid = $1 AND try = $2`, uid, number).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read the try: %w", err)
	}
	var wf v1alpha1.Workflow
	if err := json.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("decode the try: %w", err)
	}
	return &wf, nil
}
