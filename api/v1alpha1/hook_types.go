package v1alpha1

// ExitHook is a template that runs after a task or a step.
type ExitHook struct {
	// template is the name of a template of this workflow.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +required
	Template string `json:"template"`

	// arguments to give to the template. They can use the outputs and the status of the task.
	// +optional
	Arguments Arguments `json:"arguments,omitzero"`
}
