package controller

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/Thinnesen/chronicle/internal/archive"
)

type fakeQueues struct {
	depths []archive.QueueDepth
	err    error
}

func (f fakeQueues) QueueDepths(context.Context, int) ([]archive.QueueDepth, error) {
	return f.depths, f.err
}

func TestQueueCollector(t *testing.T) {
	c := NewQueueCollector(fakeQueues{depths: []archive.QueueDepth{{Namespace: "team-a", Queue: "mail", Pending: 3, OldestSeconds: 42}}})
	want := `
# HELP chronicle_task_queue_oldest_pending_seconds The age of the oldest pending task of a task queue.
# TYPE chronicle_task_queue_oldest_pending_seconds gauge
chronicle_task_queue_oldest_pending_seconds{queue="mail",workflow_namespace="team-a"} 42
# HELP chronicle_task_queue_pending The number of pending tasks of a task queue.
# TYPE chronicle_task_queue_pending gauge
chronicle_task_queue_pending{queue="mail",workflow_namespace="team-a"} 3
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(want)); err != nil {
		t.Error(err)
	}
	// A database that does not answer gives no queue metrics, never old values. The other metrics stay.
	if n := testutil.CollectAndCount(NewQueueCollector(fakeQueues{err: errors.New("down")})); n != 0 {
		t.Errorf("%d metrics from a database that does not answer", n)
	}
}
