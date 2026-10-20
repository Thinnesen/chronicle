package v1alpha1

// The labels and annotations that the controller puts on a child run.
const (
	// LabelParentUID is the UID of the parent run.
	LabelParentUID = "chronicle.thinnesen.com/parent-uid"
	// AnnotationParent is the name of the parent run, and AnnotationParentNode the name of its node.
	AnnotationParent     = "chronicle.thinnesen.com/parent"
	AnnotationParentNode = "chronicle.thinnesen.com/parent-node"
	// AnnotationDepth is the number of parent runs above the run.
	AnnotationDepth = "chronicle.thinnesen.com/depth"
)

// ChildWorkflowTemplate starts a run of a WorkflowTemplate or a ClusterWorkflowTemplate in the same namespace and waits
// for it. The node gets the phase and the output parameters of the child run. The parent run owns the child run: a stop
// of the parent stops the child, and a delete of the parent deletes the child.
type ChildWorkflowTemplate struct {
	// workflowTemplateRef names the template of the child run. The name can have placeholders.
	// +required
	WorkflowTemplateRef WorkflowTemplateRef `json:"workflowTemplateRef"`

	// arguments are the parameters of the child run. Their values can have placeholders. Artifacts are not supported.
	// +optional
	Arguments Arguments `json:"arguments,omitzero"`
}
