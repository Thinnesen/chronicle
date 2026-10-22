package server

import "sync"

// TaskWake wakes the long polls of a queue when a task arrives, so that a worker gets it at once and not with the
// next poll of the database.
type TaskWake struct {
	mu    sync.Mutex
	chans map[string]chan struct{}
}

// NewTaskWake returns a TaskWake without waiters.
func NewTaskWake() *TaskWake { return &TaskWake{chans: map[string]chan struct{}{}} }

// Notify wakes all polls that wait for the queue of the namespace.
func (w *TaskWake) Notify(namespace, queue string) {
	key := namespace + "/" + queue
	w.mu.Lock()
	defer w.mu.Unlock()
	if c, ok := w.chans[key]; ok {
		close(c)
		delete(w.chans, key)
	}
}

// wait returns a channel that closes with the next Notify for the queue. Take it before the read of the queue, so
// that a task that arrives during the read is not missed.
func (w *TaskWake) wait(namespace, queue string) <-chan struct{} {
	if w == nil {
		return nil
	}
	key := namespace + "/" + queue
	w.mu.Lock()
	defer w.mu.Unlock()
	c, ok := w.chans[key]
	if !ok {
		c = make(chan struct{})
		w.chans[key] = c
	}
	return c
}
