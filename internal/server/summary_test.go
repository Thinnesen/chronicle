package server

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

func TestSummaryCounts(t *testing.T) {
	wf := &v1alpha1.Workflow{Status: v1alpha1.WorkflowStatus{Phase: v1alpha1.WorkflowRunning, Nodes: map[string]v1alpha1.NodeStatus{
		"build":  {Type: v1alpha1.NodePod, Phase: v1alpha1.NodeSucceeded, FinishedAt: &metav1.Time{}},
		"gate":   {Type: v1alpha1.NodeSuspend, Phase: v1alpha1.NodeRunning},
		"mail":   {Type: v1alpha1.NodeTask, Phase: v1alpha1.NodeRunning},
		"passed": {Type: v1alpha1.NodeSuspend, Phase: v1alpha1.NodeSucceeded, FinishedAt: &metav1.Time{}},
	}}}
	s := summary(wf)
	if s.NodesDone != 2 || s.NodesTotal != 4 || s.ApprovalsWaiting != 1 {
		t.Errorf("done %d, total %d, approvals waiting %d; want 2, 4, 1", s.NodesDone, s.NodesTotal, s.ApprovalsWaiting)
	}
}
