package archive

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
)

// The PostgreSQL channels of the task queue. The payload is "namespace/queue" or "namespace/workflow".
const (
	channelTaskQueued = "chronicle_task_queued"
	channelTaskDone   = "chronicle_task_done"
)

// ListenTaskQueued calls fn when a task is put into a queue, until ctx ends. It logs with the logger of ctx. A notification can get lost, for
// example while the connection is down. So the callers must also poll.
func (s *Store) ListenTaskQueued(ctx context.Context, fn func(namespace, queue string)) error {
	return s.listen(ctx, channelTaskQueued, fn)
}

// ListenTaskDone calls fn when a task of a workflow has a result, until ctx ends. Notifications can get lost.
func (s *Store) ListenTaskDone(ctx context.Context, fn func(namespace, workflow string)) error {
	return s.listen(ctx, channelTaskDone, fn)
}

// listen holds one connection outside the pool for LISTEN. It connects again after an error, and returns only when
// ctx ends.
func (s *Store) listen(ctx context.Context, channel string, fn func(namespace, name string)) error {
	log := logr.FromContextOrDiscard(ctx)
	for wait := time.Second; ctx.Err() == nil; wait = min(2*wait, 30*time.Second) {
		err := s.listenOnce(ctx, channel, fn)
		if ctx.Err() != nil {
			break
		}
		log.Error(err, "The notifications stopped. The queue works with polls only until they come back", "channel", channel, "retry", wait)
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
	return nil
}

func (s *Store) listenOnce(ctx context.Context, channel string, fn func(namespace, name string)) error {
	pc, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	conn := pc.Hijack()
	defer func() { _ = conn.Close(context.Background()) }()
	if _, err := conn.Exec(ctx, "LISTEN "+channel); err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return fmt.Errorf("wait for a notification: %w", err)
		}
		if namespace, name, ok := strings.Cut(n.Payload, "/"); ok {
			fn(namespace, name)
		}
	}
}
