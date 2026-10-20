package resolve

import (
	"context"
	"errors"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// maps holds ConfigMap data by name; a map without the label is under "nolabel".
type maps map[string]map[string]string

func (m maps) ConfigMapValue(_ context.Context, name, key string) (string, bool, error) {
	if name == "nolabel" {
		return "", false, errors.New(`ConfigMap "nolabel" has no label`)
	}
	data, ok := m[name]
	if !ok {
		return "", false, errors.New("not found")
	}
	v, ok := data[key]
	return v, ok, nil
}

func TestConfigMapValues(t *testing.T) {
	cms := maps{"settings": {"region": "eu"}}
	tests := []struct {
		name, spec string
		want       []string // parts of the resolved spec
		err        string
	}{
		{name: "a workflow parameter and an input", spec: `
entrypoint: main
arguments: {parameters: [{name: region, valueFrom: {configMapKeyRef: {name: settings, key: region}}}, {name: given, value: x}]}
templates:
- name: main
  inputs: {parameters: [{name: zone, default: z1, valueFrom: {configMapKeyRef: {name: settings, key: zone}}}]}
  container: {image: x}`, want: []string{"name: region\n    value: eu", "default: z1"}},
		{name: "a missing key without a default", spec: `
entrypoint: main
arguments: {parameters: [{name: zone, valueFrom: {configMapKeyRef: {name: settings, key: zone}}}]}
templates: [{name: main, container: {image: x}}]`, err: `ConfigMap "settings" has no key "zone"`},
		{name: "no label", spec: `
entrypoint: main
arguments: {parameters: [{name: a, valueFrom: {configMapKeyRef: {name: nolabel, key: a}}}]}
templates: [{name: main, container: {image: x}}]`, err: "has no label"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var spec v1alpha1.WorkflowTemplateSpec
			if err := yaml.UnmarshalStrict([]byte(tt.spec), &spec); err != nil {
				t.Fatal(err)
			}
			if !UsesConfigMaps(&spec) {
				t.Fatal("UsesConfigMaps is false")
			}
			err := ConfigMapValues(context.Background(), cms, &spec)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("err = %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			out, _ := yaml.Marshal(spec)
			for _, w := range tt.want {
				if !strings.Contains(string(out), w) {
					t.Errorf("spec does not contain %q:\n%s", w, out)
				}
			}
			if UsesConfigMaps(&spec) {
				t.Errorf("the resolved spec still reads ConfigMaps:\n%s", out)
			}
		})
	}
}
