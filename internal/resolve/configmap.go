package resolve

import (
	"context"
	"fmt"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// ConfigMapGetter reads one key of a ConfigMap in the namespace of the workflow. found is false for a missing key.
// It refuses a ConfigMap without the label that shares its values with workflows.
type ConfigMapGetter interface {
	ConfigMapValue(ctx context.Context, name, key string) (value string, found bool, err error)
}

// UsesConfigMaps tells if a workflow parameter or an input parameter reads a ConfigMap.
func UsesConfigMaps(spec *v1alpha1.WorkflowTemplateSpec) bool {
	found := false
	eachConfigMapParam(spec, func(*v1alpha1.Parameter, bool) { found = true })
	return found
}

// ConfigMapValues reads each ConfigMap value into the spec, once at the start of a run. So all steps see the same
// value. A workflow parameter gets the value, but a given value wins. An input parameter gets it as its default, so a
// caller can still give a value. A missing key keeps the default. Without a default, a missing key is an error.
func ConfigMapValues(ctx context.Context, g ConfigMapGetter, spec *v1alpha1.WorkflowTemplateSpec) error {
	var err error
	eachConfigMapParam(spec, func(p *v1alpha1.Parameter, workflow bool) {
		if err != nil {
			return
		}
		ref := p.ValueFrom.ConfigMapKeyRef
		p.ValueFrom = nil
		if workflow && p.Value != nil {
			return
		}
		v, found, gerr := g.ConfigMapValue(ctx, ref.Name, ref.Key)
		switch {
		case gerr != nil:
			err = fmt.Errorf("parameter %q: %w", p.Name, gerr)
		case !found && p.Default == nil:
			err = fmt.Errorf("parameter %q: ConfigMap %q has no key %q: %w", p.Name, ref.Name, ref.Key, ErrInvalid)
		case found && workflow:
			p.Value = &v
		case found:
			p.Default = &v
		}
	})
	return err
}

// eachConfigMapParam calls f for each workflow parameter (workflow true) and input parameter that reads a ConfigMap.
func eachConfigMapParam(spec *v1alpha1.WorkflowTemplateSpec, f func(p *v1alpha1.Parameter, workflow bool)) {
	reads := func(p *v1alpha1.Parameter) bool { return p.ValueFrom != nil && p.ValueFrom.ConfigMapKeyRef != nil }
	for i := range spec.Arguments.Parameters {
		if p := &spec.Arguments.Parameters[i]; reads(p) {
			f(p, true)
		}
	}
	for i := range spec.Templates {
		for j := range spec.Templates[i].Inputs.Parameters {
			if p := &spec.Templates[i].Inputs.Parameters[j]; reads(p) {
				f(p, false)
			}
		}
	}
}
