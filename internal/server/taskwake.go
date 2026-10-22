package server

import "sync"

// TaskWake wakes the long polls of a queue when a task arrives, so that a worker gets it at once and not with the
// next poll of the database.
type TaskWake struct {
	mu    sync.Mutex
	chans map[string]*waiters
}

// waiters is the channel of one queue and the number of polls that wait on it.
type waiters struct {
	c chan struct{}
	n int
}

// NewTaskWake returns a TaskWake without waiters.
func NewTaskWake() *TaskWake { return &TaskWake{chans: map[string]*waiters{}} }

// Notify wakes all polls that wait for the queue of the namespace.
func (w *TaskWake) Notify(namespace, queue string) {
	key := namespace + "/" + queue
	w.mu.Lock()
	defer w.mu.Unlock()
	if e, ok := w.chans[key]; ok {
		close(e.c)
		delete(w.chans, key)
	}
}

// wait returns a channel that closes with the next Notify for the queue, and a function to call when the poll stops
// waiting. Take it before the read of the queue, so that a task that arrives during the read is not missed. An entry
// lives only while polls wait on it: a worker chooses the queue name, so the map must not keep names.
func (w *TaskWake) wait(namespace, queue string) (<-chan struct{}, func()) {
	if w == nil {
		return nil, func() {}
	}
	key := namespace + "/" + queue
	w.mu.Lock()
	defer w.mu.Unlock()
	e, ok := w.chans[key]
	if !ok {
		e = &waiters{c: make(chan struct{})}
		w.chans[key] = e
	}
	e.n++
	return e.c, func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		// After a Notify, the key can hold the entry of newer polls. Only the own entry counts down.
		if e.n--; e.n == 0 && w.chans[key] == e {
			delete(w.chans, key)
		}
	}
}
