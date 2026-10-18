package controller

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/engine"
	"github.com/Thinnesen/chronicle/internal/executor"
)

// podResult reads the state of a workflow Pod for the engine.
func podResult(pod *corev1.Pod) engine.PodResult {
	main := containerStatus(pod.Status.ContainerStatuses, mainContainer)
	if t := main.State.Terminated; t != nil {
		return terminated(t)
	}
	if init := containerStatus(pod.Status.InitContainerStatuses, "init"); init.State.Terminated != nil && init.State.Terminated.ExitCode != 0 {
		return engine.PodResult{Phase: v1alpha1.NodeError, Message: "the executor did not install: " + init.State.Terminated.Reason}
	}
	switch pod.Status.Phase {
	case corev1.PodFailed, corev1.PodSucceeded:
		return engine.PodResult{Phase: v1alpha1.NodeError, Message: podMessage(pod)}
	case corev1.PodRunning:
		return engine.PodResult{Phase: v1alpha1.NodeRunning}
	}
	r := engine.PodResult{Phase: v1alpha1.NodePending}
	if w := main.State.Waiting; w != nil && w.Reason != "ContainerCreating" {
		r.Message = w.Reason + ": " + w.Message
	}
	return r
}

func containerStatus(list []corev1.ContainerStatus, name string) corev1.ContainerStatus {
	for _, s := range list {
		if s.Name == name {
			return s
		}
	}
	return corev1.ContainerStatus{}
}

func terminated(t *corev1.ContainerStateTerminated) engine.PodResult {
	r := engine.PodResult{Phase: v1alpha1.NodeSucceeded, FinishedAt: &t.FinishedAt}
	var msg executor.Message
	if json.Unmarshal([]byte(t.Message), &msg) == nil {
		r.Outputs = outputs(msg)
	}
	if t.ExitCode == 0 {
		return r
	}
	r.Phase = v1alpha1.NodeFailed
	switch {
	case msg.Error != "":
		r.Message = msg.Error
	case t.Reason != "" && t.Reason != "Error":
		r.Message = fmt.Sprintf("%s (exit code %d)", t.Reason, t.ExitCode)
	default:
		r.Message = fmt.Sprintf("exit code %d", t.ExitCode)
	}
	return r
}

func outputs(msg executor.Message) *v1alpha1.Outputs {
	if msg.Result == nil && len(msg.Parameters) == 0 {
		return nil
	}
	o := &v1alpha1.Outputs{Result: msg.Result}
	for _, name := range slices.Sorted(maps.Keys(msg.Parameters)) {
		v := msg.Parameters[name]
		o.Parameters = append(o.Parameters, v1alpha1.Parameter{Name: name, Value: &v})
	}
	return o
}

func podMessage(pod *corev1.Pod) string {
	if pod.Status.Message != "" {
		return pod.Status.Reason + ": " + pod.Status.Message
	}
	if pod.Status.Reason != "" {
		return pod.Status.Reason
	}
	return "the Pod ended without a result"
}
