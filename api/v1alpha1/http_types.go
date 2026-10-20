package v1alpha1

import corev1 "k8s.io/api/core/v1"

// HTTPMethod is the method of an HTTP request.
// +kubebuilder:validation:Enum=GET;POST;PUT;PATCH;DELETE;HEAD
type HTTPMethod string

// HTTPTemplate sends one HTTP request from a Pod in the namespace of the workflow. A status from 200 to 299 ends
// the node with success. The response body becomes outputs.result.
type HTTPTemplate struct {
	// url of the request. It can have placeholders.
	// +kubebuilder:validation:Pattern=`^(https?://|\{\{)`
	// +kubebuilder:validation:MaxLength=2048
	// +required
	URL string `json:"url"`

	// method of the request. The default is GET.
	// +optional
	Method HTTPMethod `json:"method,omitempty"`

	// headers of the request. A value can come from a Secret in the namespace.
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	// +optional
	Headers []HTTPHeader `json:"headers,omitempty"`

	// body of the request. It can have placeholders.
	// +kubebuilder:validation:MaxLength=65536
	// +optional
	Body string `json:"body,omitempty"`
}

// HTTPHeader is one header of a request: a value, or a value from a Secret.
// +kubebuilder:validation:XValidation:rule="has(self.value) != has(self.valueFrom)",message="a header needs value or valueFrom"
type HTTPHeader struct {
	// name of the header.
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9!#$%&'*+.^_|~-]+$`
	// +kubebuilder:validation:MaxLength=256
	// +required
	Name string `json:"name"`

	// value of the header. It can have placeholders.
	// +kubebuilder:validation:MaxLength=8192
	// +optional
	Value *string `json:"value,omitempty"`

	// valueFrom reads the value from a Secret. The Pod reads the Secret; the controller does not.
	// +optional
	ValueFrom *HTTPHeaderSource `json:"valueFrom,omitempty"`
}

// HTTPHeaderSource is the source of a header value.
type HTTPHeaderSource struct {
	// secretKeyRef is a key of a Secret in the namespace of the workflow.
	// +required
	SecretKeyRef corev1.SecretKeySelector `json:"secretKeyRef"`
}
