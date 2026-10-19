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

	// valueFrom tells where an output parameter gets its value.
	// +optional
	ValueFrom *ValueFrom `json:"valueFrom,omitempty"`
}

// ValueFrom is the source of an output parameter value.
type ValueFrom struct {
	// path of a file in the main container. Its contents become the value.
	// +kubebuilder:validation:MaxLength=4096
	// +optional
	Path string `json:"path,omitempty"`

	// jsonPath reads the value from the object of a resource template, for example "{.metadata.name}".
	// +kubebuilder:validation:MaxLength=1024
	// +optional
	JSONPath string `json:"jsonPath,omitempty"`
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

	// key of the object in the artifact repository. Only the status sets it.
	// +kubebuilder:validation:MaxLength=1024
	// +optional
	Key string `json:"key,omitempty"`
}
