package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// ConcurrencyPolicy tells what to do when a run is due and an earlier run is still active.
// +kubebuilder:validation:Enum=Allow;Forbid;Replace
type ConcurrencyPolicy string

// The concurrency policies. Replace terminates the active runs and starts a new run.
const (
	ConcurrencyAllow   ConcurrencyPolicy = "Allow"
	ConcurrencyForbid  ConcurrencyPolicy = "Forbid"
	ConcurrencyReplace ConcurrencyPolicy = "Replace"
)

// CronWorkflowSpec creates Workflows on a schedule.
type CronWorkflowSpec struct {
	// schedules are cron expressions with five fields, for example "0 3 * * *". A run starts at each time of each schedule.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	// +kubebuilder:validation:items:MaxLength=256
	// +required
	Schedules []string `json:"schedules"`

	// timezone of the schedules, for example "Europe/Berlin". The default is UTC.
	// +kubebuilder:validation:MaxLength=64
	// +optional
	Timezone string `json:"timezone,omitempty"`

	// concurrencyPolicy tells what to do when an earlier run is still active. The default is Allow.
	// +optional
	ConcurrencyPolicy ConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`

	// startingDeadlineSeconds skips a run that could not start in this time after its schedule.
	// +kubebuilder:validation:Minimum=0
	// +optional
	StartingDeadlineSeconds *int64 `json:"startingDeadlineSeconds,omitempty"`

	// successfulJobsHistoryLimit is the number of succeeded runs to keep. The default is 3.
	// +kubebuilder:validation:Minimum=0
	// +optional
	SuccessfulJobsHistoryLimit *int32 `json:"successfulJobsHistoryLimit,omitempty"`

	// failedJobsHistoryLimit is the number of failed runs to keep. The default is 1.
	// +kubebuilder:validation:Minimum=0
	// +optional
	FailedJobsHistoryLimit *int32 `json:"failedJobsHistoryLimit,omitempty"`

	// suspend stops new runs. Active runs go on.
	// +optional
	Suspend bool `json:"suspend,omitempty"`

	// workflowMetadata are labels and annotations for the created Workflows.
	// +optional
	WorkflowMetadata WorkflowMetadata `json:"workflowMetadata,omitzero"`

	// workflowSpec is the spec of the created Workflows.
	// +required
	WorkflowSpec WorkflowSpec `json:"workflowSpec"`
}

// WorkflowMetadata are labels and annotations for created Workflows.
type WorkflowMetadata struct {
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// CronWorkflowStatus is the observed state of a CronWorkflow.
type CronWorkflowStatus struct {
	// lastScheduledTime is the schedule time of the last run that was started or skipped.
	// +optional
	LastScheduledTime *metav1.Time `json:"lastScheduledTime,omitempty"`

	// nextScheduledTime is the next schedule time.
	// +optional
	NextScheduledTime *metav1.Time `json:"nextScheduledTime,omitempty"`

	// active are the runs that did not finish.
	// +optional
	Active []corev1.ObjectReference `json:"active,omitempty"`

	// conditions of the CronWorkflow.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=cwf;cronwf,categories=chronicle
// +kubebuilder:printcolumn:name="Schedules",type=string,JSONPath=`.spec.schedules`
// +kubebuilder:printcolumn:name="Suspend",type=boolean,JSONPath=`.spec.suspend`
// +kubebuilder:printcolumn:name="Last",type=date,JSONPath=`.status.lastScheduledTime`
// +kubebuilder:printcolumn:name="Next",type=date,JSONPath=`.status.nextScheduledTime`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="has(self.spec.workflowSpec.workflowTemplateRef) ? !has(self.spec.workflowSpec.templates) : (has(self.spec.workflowSpec.entrypoint) && has(self.spec.workflowSpec.templates))",message="give spec.workflowSpec.entrypoint and templates, or workflowTemplateRef without templates"

// CronWorkflow creates Workflows on a schedule.
type CronWorkflow struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of CronWorkflow
	// +required
	Spec CronWorkflowSpec `json:"spec"`

	// status defines the observed state of CronWorkflow
	// +optional
	Status CronWorkflowStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// CronWorkflowList contains a list of CronWorkflow
type CronWorkflowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []CronWorkflow `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &CronWorkflow{}, &CronWorkflowList{})
		return nil
	})
}
