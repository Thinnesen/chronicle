package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

func TestPodResult(t *testing.T) {
	main := func(s corev1.ContainerState) []corev1.ContainerStatus {
		return []corev1.ContainerStatus{{Name: mainContainer, State: s}}
	}
	tests := []struct {
		name   string
		status corev1.PodStatus
		phase  v1alpha1.NodePhase
		msg    string
	}{
		{"pending", corev1.PodStatus{Phase: corev1.PodPending}, v1alpha1.NodePending, ""},
		{"image pull", corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: main(corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "no such image"}})},
			v1alpha1.NodePending, "ImagePullBackOff: no such image"},
		{"initializing", corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: main(corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{Reason: "PodInitializing"}})},
			v1alpha1.NodePending, ""},
		{"waiting without a message", corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: main(corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{Reason: "CreateContainerConfigError"}})},
			v1alpha1.NodePending, "CreateContainerConfigError"},
		{"unschedulable", corev1.PodStatus{Phase: corev1.PodPending, Conditions: []corev1.PodCondition{{Type: corev1.PodScheduled,
			Status: corev1.ConditionFalse, Reason: "Unschedulable", Message: "0/3 nodes have enough memory"}}},
			v1alpha1.NodePending, "Unschedulable: 0/3 nodes have enough memory"},
		{"executor image pull", corev1.PodStatus{Phase: corev1.PodPending, InitContainerStatuses: []corev1.ContainerStatus{{Name: "init",
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ErrImagePull", Message: "denied"}}}}},
			v1alpha1.NodePending, "ErrImagePull: denied"},
		{"running", corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: main(corev1.ContainerState{Running: &corev1.ContainerStateRunning{}})},
			v1alpha1.NodeRunning, ""},
		{"out of memory", corev1.PodStatus{Phase: corev1.PodFailed, ContainerStatuses: main(corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{ExitCode: 137, Reason: "OOMKilled"}})},
			v1alpha1.NodeFailed, "OOMKilled (exit code 137)"},
		{"executor error", corev1.PodStatus{Phase: corev1.PodFailed, ContainerStatuses: main(corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Reason: "Error", Message: `{"error":"output parameter \"p\": missing"}`}})},
			v1alpha1.NodeFailed, `output parameter "p": missing`},
		{"evicted", corev1.PodStatus{Phase: corev1.PodFailed, Reason: "Evicted", Message: "low memory"}, v1alpha1.NodeError, "Evicted: low memory"},
		{"init failed", corev1.PodStatus{Phase: corev1.PodFailed, InitContainerStatuses: []corev1.ContainerStatus{{Name: "init", State: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Reason: "Error"}}}}}, v1alpha1.NodeError, "the executor did not install: Error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := podResult(&corev1.Pod{Status: tt.status})
			if r.Phase != tt.phase || r.Message != tt.msg {
				t.Errorf("got %s %q, want %s %q", r.Phase, r.Message, tt.phase, tt.msg)
			}
			if m := containerStatus(tt.status.ContainerStatuses, mainContainer).State.Terminated; m != nil &&
				(r.Outputs == nil || r.Outputs.ExitCode == nil || *r.Outputs.ExitCode != m.ExitCode) {
				t.Errorf("outputs %+v, want exit code %d", r.Outputs, m.ExitCode)
			}
		})
	}
}
