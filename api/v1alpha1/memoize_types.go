package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// Memoize is the cache of a template: the outputs of a succeeded node, by key.
type Memoize struct {
	// key names the result, with placeholders, for example "report-{{inputs.parameters.day}}". Nodes with the same
	// key in the namespace share the result.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1024
	// +required
	Key string `json:"key"`

	// maxAge is the oldest result that a node reuses, for example "24h". Without it, a result stays valid.
	// +optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern=`^(0|([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+)$`
	MaxAge *metav1.Duration `json:"maxAge,omitempty"`
}

// NodeCache is the cache of a node.
type NodeCache struct {
	// key is the resolved cache key.
	Key string `json:"key"`
	// hit is true when the node reused the result of an earlier node instead of running.
	// +optional
	Hit bool `json:"hit,omitempty"`
	// workflow is the run whose node saved the result that this node reused.
	// +optional
	Workflow string `json:"workflow,omitempty"`
}
