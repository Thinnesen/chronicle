package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestProgress(t *testing.T) {
	end := &metav1.Time{}
	s := WorkflowStatus{Nodes: map[string]NodeStatus{
		"root":    {Type: NodeDAG, FinishedAt: end},
		"pod":     {Type: NodePod, FinishedAt: end},
		"gate":    {Type: NodeSuspend},
		"task":    {Type: NodeTask, FinishedAt: end},
		"retry":   {Type: NodeRetry, FinishedAt: end},
		"pending": {Type: NodeTask},
	}}
	if done, total := s.Progress(); done != 2 || total != 4 {
		t.Errorf("Progress() = %d, %d; want 2, 4", done, total)
	}
}
