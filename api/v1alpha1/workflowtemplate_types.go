package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// WorkflowTemplateSpec is a workflow definition. WorkflowTemplate, ClusterWorkflowTemplate and Workflow use it.
type WorkflowTemplateSpec struct {
	// entrypoint is the name of the template that the workflow starts with.
	// +kubebuilder:validation:MaxLength=128
	// +optional
	Entrypoint string `json:"entrypoint,omitempty"`

	// arguments are the workflow parameters.
	// +optional
	Arguments Arguments `json:"arguments,omitzero"`

	// templates of the workflow.
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=200
	// +optional
	Templates []Template `json:"templates,omitempty"`

	// onExit is the name of a template that runs after the workflow, also when it failed. It can use "{{workflow.status}}".
	// +kubebuilder:validation:MaxLength=128
	// +optional
	OnExit string `json:"onExit,omitempty"`

	// activeDeadlineSeconds fails the workflow when it runs longer. The exit handler still runs.
	// +kubebuilder:validation:Minimum=1
	// +optional
	ActiveDeadlineSeconds *int64 `json:"activeDeadlineSeconds,omitempty"`

	// serviceAccountName is the service account of the workflow Pods.
	// +kubebuilder:validation:MaxLength=253
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=wftmpl,categories=chronicle
// +kubebuilder:validation:XValidation:rule="has(self.spec.entrypoint) && has(self.spec.templates)",message="spec.entrypoint and spec.templates are required"

// WorkflowTemplate is a reusable workflow definition in a namespace.
type WorkflowTemplate struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec is the workflow definition.
	// +required
	Spec WorkflowTemplateSpec `json:"spec"`
}

// +kubebuilder:object:root=true

// WorkflowTemplateList contains a list of WorkflowTemplate
type WorkflowTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []WorkflowTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &WorkflowTemplate{}, &WorkflowTemplateList{})
		return nil
	})
}
