package controller

import (
	"context"
	"time"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// pruneBatch is the number of rows that one delete removes.
const pruneBatch = 500

// Pruner deletes archive rows that are older than a time.
type Pruner interface {
	Prune(ctx context.Context, before time.Time, batch int) (int64, error)
}

// ArchivePruner deletes the runs, the finished tasks and the step cache entries that are older than Retention from the
// archive. It runs at the start and then after each Every. Only the leader runs it.
type ArchivePruner struct {
	Store     Pruner
	Retention time.Duration
	Every     time.Duration
}

// Start implements manager.Runnable. A failed prune is logged; the next tick tries again.
func (p *ArchivePruner) Start(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("archive-pruner")
	tick := time.NewTicker(p.Every)
	defer tick.Stop()
	for {
		n, err := p.Store.Prune(ctx, time.Now().Add(-p.Retention), pruneBatch)
		if err != nil {
			log.Error(err, "Cannot prune the archive")
		} else if n > 0 {
			log.Info("Pruned the archive", "rows", n)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}
