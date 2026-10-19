package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NodeType is the type of a node in the workflow graph.
// +kubebuilder:validation:Enum=Pod;DAG;Steps;StepGroup;TaskGroup;Retry;Suspend;Task
type NodeType string

// The node types.
const (
	NodePod       NodeType = "Pod"
	NodeDAG       NodeType = "DAG"
	NodeSteps     NodeType = "Steps"
	NodeStepGroup NodeType = "StepGroup"
	// NodeTaskGroup is a task or step with withItems or withParam. Its children are the items.
	NodeTaskGroup NodeType = "TaskGroup"
	// NodeRetry is a template with a retry strategy. Its children are the attempts.
	NodeRetry NodeType = "Retry"
	// NodeSuspend waits for an approval or a duration.
	NodeSuspend NodeType = "Suspend"
	// NodeTask waits for a task worker.
	NodeTask NodeType = "Task"
)

// NodePhase is the phase of a node.
// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed;Error;Skipped;Omitted
type NodePhase string

// The node phases.
const (
	NodePending   NodePhase = "Pending"
	NodeRunning   NodePhase = "Running"
	NodeSucceeded NodePhase = "Succeeded"
	NodeFailed    NodePhase = "Failed"
	NodeError     NodePhase = "Error"
	NodeSkipped   NodePhase = "Skipped"
	NodeOmitted   NodePhase = "Omitted"
)

// NodeStatus is the status of one node in the workflow graph.
type NodeStatus struct {
	// id of the node. It is the key in the nodes map.
	// +required
	ID string `json:"id"`

	// name is the full path of the node, for example "wf.main.a".
	// +required
	Name string `json:"name"`

	// displayName is the short name of the node, for example "a".
	// +optional
	DisplayName string `json:"displayName,omitempty"`

	// type of the node.
	// +required
	Type NodeType `json:"type"`

	// templateName is the name of the template that the node runs.
	// +optional
	TemplateName string `json:"templateName,omitempty"`

	// phase of the node.
	// +optional
	Phase NodePhase `json:"phase,omitempty"`

	// startedAt is the time when the node started.
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// finishedAt is the time when the node finished.
	// +optional
	FinishedAt *metav1.Time `json:"finishedAt,omitempty"`

	// message tells why the node is in this phase.
	// +optional
	Message string `json:"message,omitempty"`

	// children are the IDs of the child nodes.
	// +optional
	Children []string `json:"children,omitempty"`

	// podName is the name of the Pod of a Pod node.
	// +optional
	PodName string `json:"podName,omitempty"`

	// taskID is the ID of the task of a Task node in the task queue.
	// +kubebuilder:validation:MaxLength=512
	// +optional
	TaskID string `json:"taskID,omitempty"`

	// logKey is the key of the archived log of the main container in the artifact repository.
	// +kubebuilder:validation:MaxLength=1024
	// +optional
	LogKey string `json:"logKey,omitempty"`

	// inputs are the resolved input parameters and artifacts of the node.
	// +optional
	Inputs *Inputs `json:"inputs,omitempty"`

	// outputs of the node.
	// +optional
	Outputs *Outputs `json:"outputs,omitempty"`
}
