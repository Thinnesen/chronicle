package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TaskTemplate calls a function that a task worker of an SDK runs. The worker gets the input parameters
// as a JSON object. Its result becomes outputs.result, and the fields of a result object become the
// output parameters with the same names. The template timeout is the start-to-close timeout.
type TaskTemplate struct {
	// name of the task type that a worker registers.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9][-a-zA-Z0-9_.]*$`
	// +kubebuilder:validation:MaxLength=128
	// +required
	Name string `json:"name"`

	// queue that the workers poll. The default is "default".
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9][-a-zA-Z0-9_.]*$`
	// +kubebuilder:validation:MaxLength=63
	// +optional
	Queue string `json:"queue,omitempty"`

	// heartbeatTimeout fails the attempt when the worker sends no heartbeat for this time. The default is 1 minute.
	// +optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern=`^(0|([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+)$`
	HeartbeatTimeout *metav1.Duration `json:"heartbeatTimeout,omitempty"`

	// scheduleToStartTimeout fails the attempt when no worker takes the task in this time.
	// +optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern=`^(0|([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+)$`
	ScheduleToStartTimeout *metav1.Duration `json:"scheduleToStartTimeout,omitempty"`
}
