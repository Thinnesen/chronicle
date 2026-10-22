package server

import "testing"

func TestTaskWake(t *testing.T) {
	w := NewTaskWake()
	mail, other := w.wait("team-a", "mail"), w.wait("team-b", "mail")
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
	if next := w.wait("team-a", "mail"); next == mail {
		t.Error("a new wait got the closed channel")
	}
}
