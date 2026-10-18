package engine

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/expr"
)

// globals returns the workflow placeholders: workflow.name, workflow.parameters.x and others.
func (e *engine) globals() map[string]string {
	g := map[string]string{
		"workflow.name": e.in.Name, "workflow.namespace": e.in.Namespace, "workflow.uid": e.in.UID,
		"workflow.status": e.status,
	}
	for _, p := range e.in.Spec.Arguments.Parameters {
		if v := paramValue(p); v != nil {
			g["workflow.parameters."+p.Name] = *v
		}
	}
	return g
}

func paramValue(p v1alpha1.Parameter) *string {
	if p.Value != nil {
		return p.Value
	}
	return p.Default
}

// inputScope returns a resolver for the globals and the input parameters of a node.
func (e *engine) inputScope(inputs *v1alpha1.Inputs) expr.Resolver {
	vals := e.globals()
	if inputs != nil {
		for _, p := range inputs.Parameters {
			if p.Value != nil {
				vals["inputs.parameters."+p.Name] = *p.Value
			}
		}
	}
	return func(name string) (string, bool) {
		v, ok := vals[name]
		return v, ok
	}
}

// resolveInputs gives each input parameter of a template its value: the fixed value, the argument or the default.
// A fixed value or a default can use the workflow placeholders. An argument is used as it is.
func (e *engine) resolveInputs(t *v1alpha1.Template, args map[string]string) (*v1alpha1.Inputs, error) {
	if len(t.Inputs.Parameters) == 0 {
		return nil, nil
	}
	out := &v1alpha1.Inputs{}
	for _, in := range t.Inputs.Parameters {
		v, fromTemplate := in.Value, true
		if v == nil {
			if a, ok := args[in.Name]; ok {
				v, fromTemplate = &a, false
			} else {
				v = in.Default
			}
		}
		if v == nil {
			return nil, fmt.Errorf("parameter %q has no value", in.Name)
		}
		if fromTemplate {
			s, err := expr.Substitute(*v, e.inputScope(nil))
			if err != nil {
				return nil, fmt.Errorf("parameter %q: %w", in.Name, err)
			}
			v = &s
		}
		if len(in.Enum) > 0 && !slices.Contains(in.Enum, *v) {
			return nil, fmt.Errorf("parameter %q: value %q is not one of %q", in.Name, *v, in.Enum)
		}
		out.Parameters = append(out.Parameters, v1alpha1.Parameter{Name: in.Name, Value: v})
	}
	return out, nil
}

// resolveArgs substitutes the placeholders in the argument values.
func resolveArgs(args v1alpha1.Arguments, resolve expr.Resolver) (map[string]string, error) {
	out := map[string]string{}
	for _, a := range args.Parameters {
		if a.Value == nil {
			continue
		}
		v, err := expr.Substitute(*a.Value, resolve)
		if err != nil {
			return nil, fmt.Errorf("argument %q: %w", a.Name, err)
		}
		out[a.Name] = v
	}
	return out, nil
}

// nodeOutput returns an output of a node: "outputs.result" or "outputs.parameters.x".
// For a task group, it returns a JSON list with the output of each item.
func (e *engine) nodeOutput(n v1alpha1.NodeStatus, key string) (string, bool) {
	if n.Type != v1alpha1.NodeTaskGroup {
		return outputOf(n.Outputs, key)
	}
	list := []string{}
	for _, id := range n.Children {
		c := e.nodes[id]
		if c.Phase == v1alpha1.NodeSkipped {
			continue
		}
		v, ok := outputOf(c.Outputs, key)
		if !ok {
			return "", false
		}
		list = append(list, v)
	}
	b, _ := json.Marshal(list)
	return string(b), true
}

func outputOf(o *v1alpha1.Outputs, key string) (string, bool) {
	if o == nil {
		return "", false
	}
	if key == "outputs.result" {
		if o.Result == nil {
			return "", false
		}
		return *o.Result, true
	}
	name, ok := strings.CutPrefix(key, "outputs.parameters.")
	if !ok {
		return "", false
	}
	for _, p := range o.Parameters {
		if p.Name == name && p.Value != nil {
			return *p.Value, true
		}
	}
	return "", false
}

// itemValue returns "{{item}}" or "{{item.key}}" for an item. Text values have no quotes. Other values are JSON.
func itemValue(item *apiextensionsv1.JSON, name string) (string, bool) {
	raw := item.Raw
	if key, ok := strings.CutPrefix(name, "item."); ok {
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil || obj[key] == nil {
			return "", false
		}
		raw = obj[key]
	} else if name != "item" {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	return string(raw), true
}
