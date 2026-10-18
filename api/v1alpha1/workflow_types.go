package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// WorkflowSpec is the spec of one run. It has an inline definition or a reference to a template.
type WorkflowSpec struct {
	WorkflowTemplateSpec `json:",inline"`

	// workflowTemplateRef refers to a WorkflowTemplate or a ClusterWorkflowTemplate.
	// +optional
	WorkflowTemplateRef *WorkflowTemplateRef `json:"workflowTemplateRef,omitempty"`
}

// WorkflowTemplateRef refers to a WorkflowTemplate or a ClusterWorkflowTemplate.
type WorkflowTemplateRef struct {
	// name of the template.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +required
	Name string `json:"name"`

	// clusterScope is true for a ClusterWorkflowTemplate.
	// +optional
	ClusterScope bool `json:"clusterScope,omitempty"`
}

// WorkflowPhase is the phase of a workflow.
// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed;Error;Suspended
type WorkflowPhase string

// The workflow phases.
const (
	WorkflowPending   WorkflowPhase = "Pending"
	WorkflowRunning   WorkflowPhase = "Running"
	WorkflowSucceeded WorkflowPhase = "Succeeded"
	WorkflowFailed    WorkflowPhase = "Failed"
	WorkflowError     WorkflowPhase = "Error"
	WorkflowSuspended WorkflowPhase = "Suspended"
)

// WorkflowStatus is the observed state of a workflow.
type WorkflowStatus struct {
	// phase of the workflow.
	// +optional
	Phase WorkflowPhase `json:"phase,omitempty"`

	// startedAt is the time when the workflow started.
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// finishedAt is the time when the workflow finished.
	// +optional
	FinishedAt *metav1.Time `json:"finishedAt,omitempty"`

	// message tells why the workflow is in this phase.
	// +optional
	Message string `json:"message,omitempty"`

	// nodes is a map of node ID to node status.
	// +optional
	Nodes map[string]NodeStatus `json:"nodes,omitempty"`

	// conditions of the workflow.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=wf,categories=chronicle
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Started",type=date,JSONPath=`.status.startedAt`
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=`.status.message`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="has(self.spec.workflowTemplateRef) ? !has(self.spec.templates) : (has(self.spec.entrypoint) && has(self.spec.templates))",message="give spec.entrypoint and spec.templates, or spec.workflowTemplateRef without spec.templates"

// Workflow is one run of a workflow definition.
type Workflow struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Workflow
	// +required
	Spec WorkflowSpec `json:"spec"`

	// status defines the observed state of Workflow
	// +optional
	Status WorkflowStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// WorkflowList contains a list of Workflow
type WorkflowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Workflow `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Workflow{}, &WorkflowList{})
		return nil
	})
}
