package server

import "testing"

func TestTaskWake(t *testing.T) {
	w := NewTaskWake()
	mail, doneMail := w.wait("team-a", "mail")
	other, doneOther := w.wait("team-b", "mail")
	w.Notify("team-a", "mail")
	select {
	case <-mail:
	default:
		t.Fatal("the poll of the queue did not wake")
	}
	select {
	case <-other:
		t.Fatal("the poll of another namespace woke")
	default:
	}
	doneMail()
	doneOther()
	// A worker can poll any queue name. An entry must live only while a poll waits, so the map stays small.
	for range 3 {
		_, done := w.wait("team-a", "made-up")
		done()
	}
	if len(w.chans) != 0 {
		t.Errorf("%d entries after all polls ended, want 0", len(w.chans))
	}
}
