package validate

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

func TestShellWarnings(t *testing.T) {
	for _, tt := range []struct {
		name, container, want string
	}{
		{"a placeholder in sh -c", `{image: x, command: [sh, -c], args: ["echo {{inputs.parameters.msg}}"]}`, "spec.templates[0].container"},
		{"a placeholder in bash -c", `{image: x, command: [/bin/bash, -c, "echo {{workflow.name}}"]}`, "spec.templates[0].container"},
		{"a placeholder as a plain argument", `{image: x, command: [echo], args: ["{{inputs.parameters.msg}}"]}`, ""},
		{"a shell without placeholders", `{image: x, command: [sh, -c, "echo $MSG"]}`, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			spec := parseSpec(t, "entrypoint: c\ntemplates: [{name: c, inputs: {parameters: [{name: msg, value: m}]}, container: "+tt.container+"}]")
			got := ShellWarnings(spec, field.NewPath("spec"))
			if tt.want == "" && len(got) != 0 || tt.want != "" && (len(got) != 1 || !strings.HasPrefix(got[0], tt.want)) {
				t.Errorf("warnings = %q", got)
			}
		})
	}
}
