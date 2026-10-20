package cli

import (
	"testing"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

func TestWatchChanges(t *testing.T) {
	pending := func(msg string) []v1alpha1.NodeStatus {
		return []v1alpha1.NodeStatus{{ID: "a", Name: "a", Phase: v1alpha1.NodePending, Message: msg}}
	}
	w := newChangeTracker()
	polls := []struct {
		nodes []v1alpha1.NodeStatus
		want  int
	}{
		{pending(""), 1},
		{pending("ErrImagePull: no such host"), 1},
		{pending("ImagePullBackOff: back-off"), 1},
		{pending("ErrImagePull: no such host"), 0}, // A reason that the user has seen once is not news.
		{[]v1alpha1.NodeStatus{{ID: "a", Name: "a", Phase: v1alpha1.NodeRunning}}, 1},
	}
	for i, p := range polls {
		if got := w.changes(p.nodes); len(got) != p.want {
			t.Errorf("poll %d: %d changes, want %d", i, len(got), p.want)
		}
	}
}
