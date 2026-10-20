package archive

import (
	"context"
	"testing"
)

func TestQueueDepths(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `INSERT INTO tasks (id, namespace, workflow_uid, workflow, node_id, node_name, idempotency_key, queue, name, input, state, created_at)
		VALUES ('a', 'team-a', gen_random_uuid(), 'w', 'n', 'n', 'k', 'mail', 'x', '{}', 'pending', now() - interval '90 seconds'),
		       ('b', 'team-a', gen_random_uuid(), 'w', 'n', 'n', 'k', 'mail', 'x', '{}', 'pending', now()),
		       ('c', 'team-a', gen_random_uuid(), 'w', 'n', 'n', 'k', 'mail', 'x', '{}', 'running', now()),
		       ('d', 'team-b', gen_random_uuid(), 'w', 'n', 'n', 'k', 'gpu', 'x', '{}', 'pending', now())`); err != nil {
		t.Fatal(err)
	}
	got, err := s.QueueDepths(ctx, 10)
	if err != nil || len(got) != 2 {
		t.Fatalf("QueueDepths = %+v, %v", got, err)
	}
	mail := got[0]
	if mail.Namespace != "team-a" || mail.Queue != "mail" || mail.Pending != 2 || mail.OldestSeconds < 89 {
		t.Errorf("mail = %+v", mail)
	}
	// With a limit, the rest of the queues add up in one row with the queue "other".
	got, err = s.QueueDepths(ctx, 1)
	if err != nil || len(got) != 2 || got[1].Queue != "other" || got[1].Namespace != "" || got[1].Pending != 1 {
		t.Fatalf("QueueDepths(1) = %+v, %v", got, err)
	}
}
