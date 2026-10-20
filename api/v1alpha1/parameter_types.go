package v1alpha1

// Parameter is a named string value. Inputs, outputs and arguments use parameters.
type Parameter struct {
	// name of the parameter.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9_][-a-zA-Z0-9_]*$`
	// +kubebuilder:validation:MaxLength=128
	// +required
	Name string `json:"name"`

	// value of the parameter. For an input, a value cannot be changed by the caller.
	// +kubebuilder:validation:MaxLength=262144
	// +optional
	Value *string `json:"value,omitempty"`

	// default is the value when the caller does not give a value.
	// +kubebuilder:validation:MaxLength=262144
	// +optional
	Default *string `json:"default,omitempty"`

	// enum is the list of permitted values.
	// +kubebuilder:validation:MaxItems=256
	// +kubebuilder:validation:items:MaxLength=1024
	// +optional
	Enum []string `json:"enum,omitempty"`

	// description of the parameter, for people and for the UI.
	// +kubebuilder:validation:MaxLength=1024
	// +optional
	Description string `json:"description,omitempty"`

	// valueFrom tells where an output parameter gets its value, or where a workflow or input parameter reads it.
	// +optional
	ValueFrom *ValueFrom `json:"valueFrom,omitempty"`

	// globalName makes an output parameter a workflow output with this name: "{{workflow.outputs.parameters.<name>}}"
	// in later steps and in the exit handler, and status.outputs of the run.
	// +kubebuilder:validation:MaxLength=128
	// +optional
	GlobalName string `json:"globalName,omitempty"`
}

// ValueFrom is the source of a parameter value.
type ValueFrom struct {
	// path of a file in the main container. Its contents become the value.
	// +kubebuilder:validation:MaxLength=4096
	// +optional
	Path string `json:"path,omitempty"`

	// jsonPath reads the value from the object of a resource template, for example "{.metadata.name}".
	// +kubebuilder:validation:MaxLength=1024
	// +optional
	JSONPath string `json:"jsonPath,omitempty"`

	// configMapKeyRef reads a workflow parameter or an input parameter from a ConfigMap in the namespace when the run
	// starts. The ConfigMap needs the label chronicle.thinnesen.com/parameters: "true". A missing key uses the default.
	// +optional
	ConfigMapKeyRef *ConfigMapKeyRef `json:"configMapKeyRef,omitempty"`
}

// ConfigMapKeyRef is one key of a ConfigMap.
type ConfigMapKeyRef struct {
	// name of the ConfigMap.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +required
	Name string `json:"name"`

	// key in the data of the ConfigMap.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +required
	Key string `json:"key"`
}

// Arguments are the values that a caller gives to a workflow or a template.
type Arguments struct {
	// parameters to give.
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=256
	// +optional
	Parameters []Parameter `json:"parameters,omitempty"`

	// artifacts to give.
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	// +optional
	Artifacts []Artifact `json:"artifacts,omitempty"`
}

// Inputs are the values that a template accepts.
type Inputs struct {
	// parameters that the template accepts.
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=256
	// +optional
	Parameters []Parameter `json:"parameters,omitempty"`

	// artifacts that the template accepts.
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	// +optional
	Artifacts []Artifact `json:"artifacts,omitempty"`
}

// Outputs are the values that a template produces.
type Outputs struct {
	// parameters that the template produces.
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=256
	// +optional
	Parameters []Parameter `json:"parameters,omitempty"`

	// artifacts that the template produces.
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	// +optional
	Artifacts []Artifact `json:"artifacts,omitempty"`

	// result is the standard output of a container or script node. Only the status sets it.
	// +optional
	Result *string `json:"result,omitempty"`

	// exitCode is the exit code of the main container of a container or script node. Only the status sets it.
	// +optional
	ExitCode *int32 `json:"exitCode,omitempty"`
}

// Artifact is a file or a directory in the artifact repository of the namespace.
type Artifact struct {
	// name of the artifact.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9_][-a-zA-Z0-9_]*$`
	// +kubebuilder:validation:MaxLength=128
	// +required
	Name string `json:"name"`

	// path in the main container. The executor puts an input artifact there and saves an output artifact from there.
	// +kubebuilder:validation:Pattern=`^/`
	// +kubebuilder:validation:MaxLength=4096
	// +optional
	Path string `json:"path,omitempty"`

	// from is the artifact that an argument gives, as one placeholder, for example "{{tasks.make.outputs.artifacts.data}}".
	// +kubebuilder:validation:MaxLength=1024
	// +optional
	From string `json:"from,omitempty"`

	// optional makes a missing output file or a missing argument not an error.
	// +optional
	Optional bool `json:"optional,omitempty"`

	// globalName makes an output artifact a workflow output with this name: "{{workflow.outputs.artifacts.<name>}}"
	// in the from of a later argument, and status.outputs of the run.
	// +kubebuilder:validation:MaxLength=128
	// +optional
	GlobalName string `json:"globalName,omitempty"`

	// raw is the content of an input artifact, as text in the spec.
	// +optional
	Raw *RawArtifact `json:"raw,omitempty"`

	// http is a file that the step downloads before it starts. The executor saves the file as it is.
	// +optional
	HTTP *HTTPArtifact `json:"http,omitempty"`

	// mode is the file mode of a raw or http input artifact, for example 0755 for a program. The default is 0644.
	// It has no effect on an artifact from the repository.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=511
	// +optional
	Mode *int32 `json:"mode,omitempty"`

	// key of the object in the artifact repository. Only the status sets it.
	// +kubebuilder:validation:MaxLength=1024
	// +optional
	Key string `json:"key,omitempty"`
}

// RawArtifact is the content of a small input artifact.
type RawArtifact struct {
	// data is the content of the file.
	// +kubebuilder:validation:MaxLength=65536
	// +required
	Data string `json:"data"`
}

// HTTPArtifact is a file at a URL.
type HTTPArtifact struct {
	// url of the file. The step downloads it from its own Pod, with a GET request.
	// +kubebuilder:validation:Pattern=`^https?://`
	// +kubebuilder:validation:MaxLength=2048
	// +required
	URL string `json:"url"`
}
