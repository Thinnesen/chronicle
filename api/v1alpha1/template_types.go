package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
)

// Template is one unit of work. It has exactly one type: container, script, dag or steps.
// +kubebuilder:validation:XValidation:rule="[has(self.container), has(self.script), has(self.dag), has(self.steps)].filter(x, x).size() == 1",message="a template must have exactly one of container, script, dag or steps"
type Template struct {
	// name of the template. It is unique in the workflow.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9][-a-zA-Z0-9_]*$`
	// +kubebuilder:validation:MaxLength=128
	// +required
	Name string `json:"name"`

	// inputs are the parameters that the template accepts.
	// +optional
	Inputs Inputs `json:"inputs,omitzero"`

	// outputs are the parameters that the template produces.
	// +optional
	Outputs Outputs `json:"outputs,omitzero"`

	// container runs one container in a Pod.
	// +optional
	Container *ContainerTemplate `json:"container,omitempty"`

	// script runs a script in a Pod.
	// +optional
	Script *ScriptTemplate `json:"script,omitempty"`

	// dag runs tasks in the order of their dependencies.
	// +optional
	DAG *DAGTemplate `json:"dag,omitempty"`

	// steps runs groups of steps. The groups run one after the other. The steps in a group run in parallel.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=100
	// +kubebuilder:validation:items:MinItems=1
	// +kubebuilder:validation:items:MaxItems=100
	// +optional
	Steps [][]WorkflowStep `json:"steps,omitempty"`
}

// ContainerTemplate is the container of a Pod. The controller sets a restricted security context.
type ContainerTemplate struct {
	// image of the container.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1024
	// +required
	Image string `json:"image"`

	// command is the entrypoint. It is not run in a shell.
	// +kubebuilder:validation:MaxItems=256
	// +optional
	Command []string `json:"command,omitempty"`

	// args are the arguments to the entrypoint.
	// +kubebuilder:validation:MaxItems=256
	// +optional
	Args []string `json:"args,omitempty"`

	// env is the list of environment variables.
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=256
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// envFrom is the list of sources of environment variables.
	// +kubebuilder:validation:MaxItems=32
	// +optional
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`

	// workingDir is the working directory.
	// +kubebuilder:validation:MaxLength=4096
	// +optional
	WorkingDir string `json:"workingDir,omitempty"`

	// resources are the compute resources of the container.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitzero"`

	// imagePullPolicy is the pull policy of the image.
	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`
}

// ScriptTemplate runs source code in a container.
// The source gets parameters through environment variables, never through text substitution.
type ScriptTemplate struct {
	ContainerTemplate `json:",inline"`

	// source is the script. The controller writes it to a file and runs the command with the file as the last argument.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=262144
	// +required
	Source string `json:"source"`
}

// DAGTemplate runs tasks in the order of their dependencies.
type DAGTemplate struct {
	// tasks of the DAG.
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=200
	// +required
	Tasks []DAGTask `json:"tasks"`

	// failFast stops the start of new tasks after a task fails. The default is true.
	// +optional
	FailFast *bool `json:"failFast,omitempty"`
}

// DAGTask is one node of a DAG.
// +kubebuilder:validation:XValidation:rule="!(has(self.dependencies) && has(self.depends))",message="use dependencies or depends, not both"
type DAGTask struct {
	// name of the task. It is unique in the DAG.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9][-a-zA-Z0-9_]*$`
	// +kubebuilder:validation:MaxLength=128
	// +required
	Name string `json:"name"`

	// template is the name of the template to run.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +required
	Template string `json:"template"`

	// arguments to give to the template.
	// +optional
	Arguments Arguments `json:"arguments,omitzero"`

	// dependencies are the names of the tasks that must succeed before this task starts.
	// +listType=set
	// +kubebuilder:validation:MaxItems=200
	// +kubebuilder:validation:items:MaxLength=128
	// +optional
	Dependencies []string `json:"dependencies,omitempty"`

	// depends is a boolean expression of task results, for example "a.Succeeded || b.Failed".
	// +kubebuilder:validation:MaxLength=4096
	// +optional
	Depends string `json:"depends,omitempty"`
}

// WorkflowStep is one step in a group of parallel steps.
type WorkflowStep struct {
	// name of the step. It is unique in the steps template.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9][-a-zA-Z0-9_]*$`
	// +kubebuilder:validation:MaxLength=128
	// +required
	Name string `json:"name"`

	// template is the name of the template to run.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +required
	Template string `json:"template"`

	// arguments to give to the template.
	// +optional
	Arguments Arguments `json:"arguments,omitzero"`
}
