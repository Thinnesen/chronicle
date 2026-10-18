// Package manifest parses the manifest of a resource template and substitutes placeholders
// in its string values only. A value can never change the structure of the object.
package manifest

import (
	"encoding/json"
	"fmt"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/Thinnesen/chronicle/internal/expr"
)

// Parse parses a YAML manifest into a JSON object tree.
func Parse(s string) (map[string]any, error) {
	data, err := yaml.YAMLToJSON([]byte(s))
	if err != nil {
		return nil, fmt.Errorf("bad manifest: %w", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("the manifest must be one object: %w", err)
	}
	return obj, nil
}

// Strings calls fn for each string value of the tree. It returns an error for a key with a placeholder.
func Strings(v any, fn func(s string) error) error {
	switch v := v.(type) {
	case map[string]any:
		for k, child := range v {
			if strings.Contains(k, "{{") {
				return fmt.Errorf("the key %q has a placeholder: placeholders can be in values only", k)
			}
			if err := Strings(child, fn); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range v {
			if err := Strings(child, fn); err != nil {
				return err
			}
		}
	case string:
		return fn(v)
	}
	return nil
}

// Substitute parses the manifest, replaces the placeholders in its string values and returns JSON.
func Substitute(s string, resolve expr.Resolver) (string, error) {
	obj, err := Parse(s)
	if err != nil {
		return "", err
	}
	if err := Strings(obj, func(string) error { return nil }); err != nil {
		return "", err
	}
	out, err := substitute(obj, resolve)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(out)
	return string(data), err
}

func substitute(v any, resolve expr.Resolver) (any, error) {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, child := range v {
			s, err := substitute(child, resolve)
			if err != nil {
				return nil, err
			}
			out[k] = s
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			s, err := substitute(child, resolve)
			if err != nil {
				return nil, err
			}
			out[i] = s
		}
		return out, nil
	case string:
		return expr.Substitute(v, resolve)
	}
	return v, nil
}
