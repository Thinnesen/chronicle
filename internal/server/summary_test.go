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
		"pull":   {Type: v1alpha1.NodePod, Phase: v1alpha1.NodePending, Message: "ImagePullBackOff: no such image"},
		"slot":   {Type: v1alpha1.NodePod, Phase: v1alpha1.NodePending, Message: "waiting for a free slot (parallelism 2)"},
	}}}
	s := summary(wf)
	if s.NodesDone != 2 || s.NodesTotal != 6 || s.ApprovalsWaiting != 1 || s.WaitingReason != "ImagePullBackOff" {
		t.Errorf("done %d, total %d, approvals waiting %d, reason %q; want 2, 6, 1, ImagePullBackOff",
			s.NodesDone, s.NodesTotal, s.ApprovalsWaiting, s.WaitingReason)
	}
}

func TestSummaryStartAt(t *testing.T) {
	at := metav1.Now()
	wf := &v1alpha1.Workflow{Spec: v1alpha1.WorkflowSpec{StartAt: &at}}
	if s := summary(wf); !s.StartAt.AsTime().Equal(at.Time) {
		t.Errorf("start time of a waiting run = %v", s.StartAt)
	}
	wf.Status.StartedAt = &at
	if s := summary(wf); s.StartAt != nil {
		t.Errorf("a started run has the start time %v", s.StartAt)
	}
}

func TestWaitingReason(t *testing.T) {
	for msg, want := range map[string]string{
		"Unschedulable: 0/3 nodes are available":                      "Unschedulable",
		"waiting for the lock gpu":                                    "",
		"waiting for a free slot (parallelism 2)":                     "",
		`waiting for the lock gpu: ConfigMap "limits" does not exist`: "InvalidLockLimit",
	} {
		if got := waitingReason(msg); got != want {
			t.Errorf("waitingReason(%q) = %q, want %q", msg, got, want)
		}
	}
}
