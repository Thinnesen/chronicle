package archive

import (
	"context"
	"fmt"
	"time"
)

// pruneQueries delete one batch of old rows each: finished runs (their tries go with them), finished tasks and step
// cache entries. $1 is the time limit and $2 the batch size. Small batches keep each lock short.
var pruneQueries = []string{
	`DELETE FROM workflow_runs WHERE ctid IN (SELECT ctid FROM workflow_runs WHERE finished_at < $1 LIMIT $2)`,
	`DELETE FROM tasks WHERE ctid IN (SELECT ctid FROM tasks WHERE state NOT IN ('pending', 'running') AND finished_at < $1 LIMIT $2)`,
	`DELETE FROM step_cache WHERE ctid IN (SELECT ctid FROM step_cache WHERE saved_at < $1 LIMIT $2)`,
}

// Prune deletes the runs, the finished tasks and the step cache entries that are older than before. It returns the
// number of deleted rows.
func (s *Store) Prune(ctx context.Context, before time.Time, batch int) (int64, error) {
	var total int64
	for _, q := range pruneQueries {
		for {
			tag, err := s.pool.Exec(ctx, q, before, batch)
			if err != nil {
				return total, fmt.Errorf("prune the archive: %w", err)
			}
			total += tag.RowsAffected()
			if tag.RowsAffected() < int64(batch) {
				break
			}
		}
	}
	return total, nil
}
