package controller

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakePruner struct{ before chan time.Time }

func (f fakePruner) Prune(_ context.Context, before time.Time, _ int) (int64, error) {
	f.before <- before
	return 0, errors.New("the database does not answer")
}

func TestArchivePruner(t *testing.T) {
	f := fakePruner{before: make(chan time.Time, 4)}
	p := &ArchivePruner{Store: f, Retention: 24 * time.Hour, Every: 10 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- p.Start(ctx) }()
	// A failed prune does not stop the pruner: it tries again at the next tick.
	for range 2 {
		if got := time.Since(<-f.before); got < 24*time.Hour || got > 25*time.Hour {
			t.Fatalf("the limit is %v ago, want 24 h", got)
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Start = %v", err)
	}
}
