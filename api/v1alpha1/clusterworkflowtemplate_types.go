package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=cwftmpl,categories=chronicle
// +kubebuilder:validation:XValidation:rule="has(self.spec.entrypoint) && has(self.spec.templates)",message="spec.entrypoint and spec.templates are required"

// ClusterWorkflowTemplate is a reusable workflow definition for all namespaces.
type ClusterWorkflowTemplate struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec is the workflow definition.
	// +required
	Spec WorkflowTemplateSpec `json:"spec"`
}

// +kubebuilder:object:root=true

// ClusterWorkflowTemplateList contains a list of ClusterWorkflowTemplate
type ClusterWorkflowTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ClusterWorkflowTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ClusterWorkflowTemplate{}, &ClusterWorkflowTemplateList{})
		return nil
	})
}
