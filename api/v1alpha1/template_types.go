package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Template is one unit of work. It has exactly one type: container, script, dag, steps, suspend or resource.
// +kubebuilder:validation:XValidation:rule="[has(self.container), has(self.script), has(self.dag), has(self.steps), has(self.suspend), has(self.resource)].filter(x, x).size() == 1",message="a template must have exactly one of container, script, dag, steps, suspend or resource"
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

	// retryStrategy runs the template again when it fails.
	// +optional
	RetryStrategy *RetryStrategy `json:"retryStrategy,omitempty"`

	// timeout fails a node of this template when it runs longer, for example "10m". With retries, it applies to each attempt.
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// container runs one container in a Pod.
	// +optional
	Container *ContainerTemplate `json:"container,omitempty"`

	// script runs a script in a Pod.
	// +optional
	Script *ScriptTemplate `json:"script,omitempty"`

	// dag runs tasks in the order of their dependencies.
	// +optional
	DAG *DAGTemplate `json:"dag,omitempty"`

	// suspend waits for an approval in spec.approvals of the Workflow, or for a duration.
	// The approval can give values to the output parameters. Without a value, an output parameter gets its default.
	// +optional
	Suspend *SuspendTemplate `json:"suspend,omitempty"`

	// resource creates, applies, patches, deletes or reads a Kubernetes object. It runs in a Pod with the
	// service account of the workflow, so it has only the permissions of that service account.
	// +optional
	Resource *ResourceTemplate `json:"resource,omitempty"`

	// steps runs groups of steps. The groups run one after the other. The steps in a group run in parallel.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=100
	// +kubebuilder:validation:items:MinItems=1
	// +kubebuilder:validation:items:MaxItems=100
	// +optional
	Steps [][]WorkflowStep `json:"steps,omitempty"`
}

// SuspendTemplate waits for an approval or for a duration.
type SuspendTemplate struct {
	// duration ends the wait with success, for example "1h". Without a duration, the node waits for an approval.
	// +optional
	Duration *metav1.Duration `json:"duration,omitempty"`
}

// ResourceAction is the operation of a resource template.
// +kubebuilder:validation:Enum=get;create;apply;patch;delete
type ResourceAction string

// The resource actions. apply is a server-side apply. patch is a JSON merge patch.
const (
	ResourceGet    ResourceAction = "get"
	ResourceCreate ResourceAction = "create"
	ResourceApply  ResourceAction = "apply"
	ResourcePatch  ResourceAction = "patch"
	ResourceDelete ResourceAction = "delete"
)

// ResourceTemplate is an operation on a Kubernetes object.
type ResourceTemplate struct {
	// action is the operation.
	// +required
	Action ResourceAction `json:"action"`

	// manifest is the object as YAML. Placeholders can be in string values only, never in keys.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=262144
	// +required
	Manifest string `json:"manifest"`

	// successCondition ends the node with success when the object matches, for example "status.succeeded > 0".
	// Conditions are comma-separated "path operator value" terms, and all terms must match.
	// +kubebuilder:validation:MaxLength=1024
	// +optional
	SuccessCondition string `json:"successCondition,omitempty"`

	// failureCondition fails the node when the object matches, for example "status.failed > 3".
	// +kubebuilder:validation:MaxLength=1024
	// +optional
	FailureCondition string `json:"failureCondition,omitempty"`

	// setOwnerReference makes the Workflow the owner of the object, so that the object is deleted with the Workflow.
	// +optional
	SetOwnerReference bool `json:"setOwnerReference,omitempty"`
}

// RetryPolicy tells which failures start a new attempt.
// +kubebuilder:validation:Enum=Always;OnFailure;OnError
type RetryPolicy string

// The retry policies. OnFailure retries a non-zero exit code. OnError retries a Pod or controller error.
const (
	RetryAlways    RetryPolicy = "Always"
	RetryOnFailure RetryPolicy = "OnFailure"
	RetryOnError   RetryPolicy = "OnError"
)

// RetryStrategy runs a template again when it fails.
type RetryStrategy struct {
	// limit is the number of retries after the first attempt.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	// +required
	Limit int32 `json:"limit"`

	// retryPolicy tells which failures start a new attempt. The default is OnFailure.
	// +optional
	RetryPolicy RetryPolicy `json:"retryPolicy,omitempty"`

	// backoff is the wait before each retry. Without a backoff, a retry starts at once.
	// +optional
	Backoff *Backoff `json:"backoff,omitempty"`
}

// Backoff is the wait before a retry: duration * factor^(retry-1), at most maxDuration.
type Backoff struct {
	// duration is the wait before the first retry, for example "10s".
	// +required
	Duration metav1.Duration `json:"duration"`

	// factor multiplies the wait for each retry. The default is 1.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	// +optional
	Factor *int32 `json:"factor,omitempty"`

	// maxDuration is the longest wait.
	// +optional
	MaxDuration *metav1.Duration `json:"maxDuration,omitempty"`
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

	Call `json:",inline"`

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

	Call `json:",inline"`
}

// Call is a call of a template from a DAG task or a step.
type Call struct {
	// template is the name of the template to run.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +required
	Template string `json:"template"`

	// arguments to give to the template.
	// +optional
	Arguments Arguments `json:"arguments,omitzero"`

	// when is a condition. When it is false, the node is Skipped. For example "{{tasks.a.outputs.result}} == heads".
	// +kubebuilder:validation:MaxLength=4096
	// +optional
	When string `json:"when,omitempty"`

	// withItems runs the template once for each item. "{{item}}" is the item, "{{item.key}}" a field of an object item.
	// +kubebuilder:validation:MaxItems=1000
	// +optional
	WithItems []apiextensionsv1.JSON `json:"withItems,omitempty"`

	// withParam runs the template once for each item of a JSON list, for example "{{tasks.a.outputs.result}}".
	// +kubebuilder:validation:MaxLength=4096
	// +optional
	WithParam string `json:"withParam,omitempty"`
}
