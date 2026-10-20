package controller

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/artifact"
	"github.com/Thinnesen/chronicle/internal/engine"
	"github.com/Thinnesen/chronicle/internal/executor"
)

// podResult reads the state of a workflow Pod for the engine.
func podResult(pod *corev1.Pod) engine.PodResult {
	main := containerStatus(pod.Status.ContainerStatuses, mainContainer)
	if t := main.State.Terminated; t != nil {
		return terminated(t, pod.Annotations[AnnotationArtifactPrefix])
	}
	executorInit := containerStatus(pod.Status.InitContainerStatuses, "init")
	if t := executorInit.State.Terminated; t != nil && t.ExitCode != 0 {
		// The init container also downloads the input artifacts, and reports a failure in its message.
		var msg executor.Message
		if json.Unmarshal([]byte(t.Message), &msg) == nil && msg.Error != "" {
			return engine.PodResult{Phase: v1alpha1.NodeError, Message: msg.Error}
		}
		return engine.PodResult{Phase: v1alpha1.NodeError, Message: "the executor did not install: " + t.Reason}
	}
	switch pod.Status.Phase {
	case corev1.PodFailed, corev1.PodSucceeded:
		return engine.PodResult{Phase: v1alpha1.NodeError, Message: podMessage(pod)}
	case corev1.PodRunning:
		return engine.PodResult{Phase: v1alpha1.NodeRunning}
	}
	return engine.PodResult{Phase: v1alpha1.NodePending, Message: pendingMessage(pod, executorInit, main)}
}

// pendingMessage tells why a Pod does not start. Either no node can take it, or a container waits, for example for its
// image.
func pendingMessage(pod *corev1.Pod, executorInit, main corev1.ContainerStatus) string {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionFalse && c.Reason != "" {
			return reasonMessage(c.Reason, c.Message)
		}
	}
	for _, s := range []corev1.ContainerStatus{executorInit, main} {
		// ContainerCreating and PodInitializing are normal steps of a start, not news for the user.
		if w := s.State.Waiting; w != nil && w.Reason != "ContainerCreating" && w.Reason != "PodInitializing" {
			return reasonMessage(w.Reason, w.Message)
		}
	}
	return ""
}

func containerStatus(list []corev1.ContainerStatus, name string) corev1.ContainerStatus {
	for _, s := range list {
		if s.Name == name {
			return s
		}
	}
	return corev1.ContainerStatus{}
}

// terminated reads the result of the main container. prefix is the key prefix of its artifacts.
func terminated(t *corev1.ContainerStateTerminated, prefix string) engine.PodResult {
	r := engine.PodResult{Phase: v1alpha1.NodeSucceeded, FinishedAt: &t.FinishedAt}
	var msg executor.Message
	if json.Unmarshal([]byte(t.Message), &msg) == nil {
		r.Outputs = outputs(msg, prefix)
		if msg.Log && prefix != "" {
			r.LogKey = artifact.LogKey(prefix)
		}
	}
	if r.Outputs == nil {
		r.Outputs = &v1alpha1.Outputs{}
	}
	r.Outputs.ExitCode = &t.ExitCode
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

func outputs(msg executor.Message, prefix string) *v1alpha1.Outputs {
	if msg.Result == nil && len(msg.Parameters) == 0 && len(msg.Artifacts) == 0 {
		return nil
	}
	o := &v1alpha1.Outputs{Result: msg.Result}
	if prefix != "" {
		for _, name := range msg.Artifacts {
			o.Artifacts = append(o.Artifacts, v1alpha1.Artifact{Name: name, Key: artifact.ArtifactKey(prefix, name)})
		}
	}
	for _, name := range slices.Sorted(maps.Keys(msg.Parameters)) {
		v := msg.Parameters[name]
		o.Parameters = append(o.Parameters, v1alpha1.Parameter{Name: name, Value: &v})
	}
	return o
}

func podMessage(pod *corev1.Pod) string {
	if m := reasonMessage(pod.Status.Reason, pod.Status.Message); m != "" {
		return m
	}
	return "the Pod ended without a result"
}

// reasonMessage joins a Kubernetes reason and message, and leaves out a part that is empty.
func reasonMessage(reason, message string) string {
	switch {
	case reason == "":
		return message
	case message == "":
		return reason
	}
	return reason + ": " + message
}
