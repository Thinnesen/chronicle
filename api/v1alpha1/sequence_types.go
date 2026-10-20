package v1alpha1

import "k8s.io/apimachinery/pkg/util/intstr"

// Sequence is a range of numbers for withSequence. Give count, or end. The numbers can be placeholders.
// +kubebuilder:validation:XValidation:rule="has(self.count) != has(self.end)",message="give count or end"
type Sequence struct {
	// count is the number of items. The items are start, start+1 and so on.
	// +optional
	Count *intstr.IntOrString `json:"count,omitempty"`

	// start is the first number. The default is 0.
	// +optional
	Start *intstr.IntOrString `json:"start,omitempty"`

	// end is the last number. It is in the range.
	// +optional
	End *intstr.IntOrString `json:"end,omitempty"`

	// format makes an item text from the number, with one integer verb, for example "day-%02d". The default is "%d".
	// +kubebuilder:validation:MaxLength=256
	// +optional
	Format string `json:"format,omitempty"`
}

// FanOut tells if a call runs its template once for each item.
func (c *Call) FanOut() bool {
	return len(c.WithItems) > 0 || c.WithParam != "" || c.WithSequence != nil
}
