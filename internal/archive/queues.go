package archive

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// QueueDepth is the backlog of one task queue: the pending tasks and the age of the oldest one.
type QueueDepth struct {
	Namespace, Queue string
	Pending          int64
	OldestSeconds    float64
}

// QueueDepths returns the backlog of the task queues with pending tasks, the largest first. Queue names are free text,
// so it returns at most limit queues. The rest add up in one row with the queue "other" and no namespace.
func (s *Store) QueueDepths(ctx context.Context, limit int) ([]QueueDepth, error) {
	rows, err := s.pool.Query(ctx, `
SELECT namespace, queue, count(*), EXTRACT(EPOCH FROM now() - min(created_at))::float8 FROM tasks
WHERE state = 'pending' GROUP BY namespace, queue ORDER BY count(*) DESC, namespace, queue`)
	if err != nil {
		return nil, fmt.Errorf("read the task queues: %w", err)
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (QueueDepth, error) {
		var d QueueDepth
		err := r.Scan(&d.Namespace, &d.Queue, &d.Pending, &d.OldestSeconds)
		return d, err
	})
	if err != nil || len(all) <= limit {
		return all, err
	}
	other := QueueDepth{Queue: "other"}
	for _, d := range all[limit:] {
		other.Pending += d.Pending
		other.OldestSeconds = max(other.OldestSeconds, d.OldestSeconds)
	}
	return append(all[:limit], other), nil
}
