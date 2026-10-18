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

	// podGC deletes the Pods of finished nodes. Without it, the Pods stay until the Workflow is deleted.
	// +optional
	PodGC *PodGC `json:"podGC,omitempty"`

	// ttlStrategy deletes the Workflow some time after it finished.
	// +optional
	TTLStrategy *TTLStrategy `json:"ttlStrategy,omitempty"`

	// serviceAccountName is the service account of the workflow Pods.
	// +kubebuilder:validation:MaxLength=253
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
}

// PodGCStrategy tells when to delete Pods.
// +kubebuilder:validation:Enum=OnPodCompletion;OnPodSuccess;OnWorkflowCompletion;OnWorkflowSuccess
type PodGCStrategy string

// The Pod GC strategies.
const (
	PodGCOnPodCompletion      PodGCStrategy = "OnPodCompletion"
	PodGCOnPodSuccess         PodGCStrategy = "OnPodSuccess"
	PodGCOnWorkflowCompletion PodGCStrategy = "OnWorkflowCompletion"
	PodGCOnWorkflowSuccess    PodGCStrategy = "OnWorkflowSuccess"
)

// PodGC deletes the Pods of finished nodes.
type PodGC struct {
	// strategy tells when to delete Pods.
	// +required
	Strategy PodGCStrategy `json:"strategy"`

	// deleteDelayDuration is the wait before a Pod is deleted, for example "10m".
	// +optional
	DeleteDelayDuration *metav1.Duration `json:"deleteDelayDuration,omitempty"`
}

// TTLStrategy deletes a finished Workflow. The most specific field wins.
type TTLStrategy struct {
	// secondsAfterCompletion applies to all finished workflows.
	// +kubebuilder:validation:Minimum=0
	// +optional
	SecondsAfterCompletion *int32 `json:"secondsAfterCompletion,omitempty"`

	// secondsAfterSuccess applies to workflows that succeeded.
	// +kubebuilder:validation:Minimum=0
	// +optional
	SecondsAfterSuccess *int32 `json:"secondsAfterSuccess,omitempty"`

	// secondsAfterFailure applies to workflows that failed or had an error.
	// +kubebuilder:validation:Minimum=0
	// +optional
	SecondsAfterFailure *int32 `json:"secondsAfterFailure,omitempty"`
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
