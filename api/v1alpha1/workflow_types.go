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

	// suspend pauses the workflow: no new node starts. Running Pods go on.
	// +optional
	Suspend bool `json:"suspend,omitempty"`

	// shutdown stops the workflow. Stop fails the running nodes and runs the exit handler.
	// Terminate fails the running nodes and does not run the exit handler.
	// +optional
	Shutdown ShutdownMode `json:"shutdown,omitempty"`

	// restart asks for a retry of a finished run. Raise generation to retry again.
	// +optional
	Restart *Restart `json:"restart,omitempty"`

	// approvals are the decisions for suspend nodes.
	// +listType=map
	// +listMapKey=node
	// +kubebuilder:validation:MaxItems=500
	// +optional
	Approvals []Approval `json:"approvals,omitempty"`
}

// Restart is a request to retry a finished run.
type Restart struct {
	// generation counts the retries. The controller retries when it is higher than status.restarts.
	// +kubebuilder:validation:Minimum=1
	// +required
	Generation int32 `json:"generation"`

	// fromNode is the ID or the name of the node to retry from. Empty retries the nodes that failed.
	// +kubebuilder:validation:MaxLength=1024
	// +optional
	FromNode string `json:"fromNode,omitempty"`
}

// ShutdownMode is a way to stop a workflow.
// +kubebuilder:validation:Enum=Stop;Terminate
type ShutdownMode string

// The shutdown modes.
const (
	ShutdownStop      ShutdownMode = "Stop"
	ShutdownTerminate ShutdownMode = "Terminate"
)

// Decision is the decision of an approval.
// +kubebuilder:validation:Enum=Approve;Reject
type Decision string

// The decisions.
const (
	DecisionApprove Decision = "Approve"
	DecisionReject  Decision = "Reject"
)

// Approval is a decision for a suspend node.
type Approval struct {
	// node is the ID or the name of the suspend node.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1024
	// +required
	Node string `json:"node"`

	// decision: Approve ends the node with success. Reject fails the node.
	// +required
	Decision Decision `json:"decision"`

	// parameters give values to the output parameters of the suspend template.
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=64
	// +optional
	Parameters []Parameter `json:"parameters,omitempty"`

	// message is a comment for the run history.
	// +kubebuilder:validation:MaxLength=1024
	// +optional
	Message string `json:"message,omitempty"`

	// user is the user who decided. The chronicle API sets it. The admission webhook refuses a new approval whose
	// user is not the user of the request, so a recorded user is always true. An approval without a user is allowed.
	// +kubebuilder:validation:MaxLength=512
	// +optional
	User string `json:"user,omitempty"`
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

	// restarts is the number of retries that the controller made.
	// +optional
	Restarts int32 `json:"restarts,omitempty"`

	// storedSpec is the resolved spec of a workflow that uses workflowTemplateRef or templateRef.
	// The controller resolves the references once, so later template changes do not change the run.
	// +optional
	StoredSpec *WorkflowTemplateSpec `json:"storedSpec,omitempty"`

	// nodes is a map of node ID to node status.
	// +optional
	Nodes map[string]NodeStatus `json:"nodes,omitempty"`

	// outputs are the workflow outputs: the output parameters with a globalName, from the steps that finished.
	// +optional
	Outputs *Outputs `json:"outputs,omitempty"`

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
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.spec.shutdown) || (has(self.spec.shutdown) && (self.spec.shutdown == oldSelf.spec.shutdown || self.spec.shutdown == 'Terminate'))",message="a shutdown cannot be removed, and only Stop can change to Terminate"

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
