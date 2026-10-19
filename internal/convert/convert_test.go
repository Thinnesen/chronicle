package convert

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestArgoExamples converts unchanged Argo examples. want is a part of the expected warning or error.
func TestArgoExamples(t *testing.T) {
	tests := []struct {
		file, warning, err string
	}{
		{file: "hello-world.yaml"},
		{file: "steps.yaml"},
		{file: "dag-diamond.yaml"},
		{file: "dag-nested.yaml"},
		{file: "dag-enhanced-depends.yaml"},
		{file: "loops-maps.yaml"},
		{file: "loops-param-result.yaml"},
		{file: "coinflip.yaml"},
		{file: "scripts-bash.yaml"},
		{file: "scripts-python.yaml"},
		{file: "suspend-template-outputs.yaml"},
		{file: "output-parameter.yaml"},
		{file: "artifact-passing.yaml"},
		{file: "cron-workflow.yaml"},
		{file: "timeouts-step.yaml"},
		{file: "retry-backoff.yaml", warning: "retryStrategy.backoff.cap: chronicle does not support this field"},
		{file: "exit-handlers.yaml", err: `unknown placeholder "workflow.failures"`},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "argo", tt.file))
			if err != nil {
				t.Fatal(err)
			}
			res, err := Convert(data)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("err = %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			for _, w := range res[0].Warnings {
				got += w.String() + "\n"
			}
			if tt.warning == "" && got != "" || !strings.Contains(got, tt.warning) {
				t.Errorf("warnings:\n%s\nwant %q", got, tt.warning)
			}
		})
	}
}

// TestRules checks the conversions that differ from a plain copy.
func TestRules(t *testing.T) {
	const head = "apiVersion: argoproj.io/v1alpha1\nkind: Workflow\nmetadata: {generateName: x-}\nspec:\n  entrypoint: main\n  templates:\n"
	tests := []struct {
		name, templates string
		want            []string // parts of the output YAML or of the warnings
		err             string
	}{
		{
			name:      "string numbers and bare durations",
			templates: `  - {name: main, retryStrategy: {limit: "3", backoff: {duration: "5", factor: "2"}}, container: {image: x, command: [run]}}`,
			want:      []string{"limit: 3", "duration: 5s", "factor: 2"},
		},
		{
			name:      "shell script placeholders become variables",
			templates: "  - {name: main, inputs: {parameters: [{name: who, value: a}]}, script: {image: x, source: 'echo {{inputs.parameters.who}}'}}",
			want:      []string{"source: echo ${CHRONICLE_INPUTS_PARAMETERS_WHO}", "value: '{{inputs.parameters.who}}'", "now environment variables (1)"},
		},
		{
			name:      "python script placeholders are refused",
			templates: "  - {name: main, inputs: {parameters: [{name: who, value: a}]}, script: {image: x, command: [python], source: 'print(\"{{inputs.parameters.who}}\")'}}",
			err:       "not a shell script",
		},
		{
			name: "parallelism of the workflow and of a dag",
			templates: "  - {name: main, parallelism: 2, dag: {tasks: [{name: a, template: c}]}}\n" +
				"  - {name: c, parallelism: 1, container: {image: x}}\n  parallelism: 5",
			want: []string{"parallelism: 5", "- dag:", "parallelism: 2", "spec.templates[1].parallelism: only a dag or steps template"},
		},
		{
			name: "unsupported fields are dropped",
			templates: "  - {name: main, nodeSelector: {a: b}, container: {image: x, securityContext: {privileged: true}}}\n" +
				"  podSpecPatch: '{}'",
			want: []string{"spec.templates[0].nodeSelector: chronicle does not support", "spec.templates[0].container.securityContext",
				"spec.podSpecPatch"},
		},
		{
			name: "sequence and unknown arguments",
			templates: "  - {name: main, steps: [[{name: s, template: c, withSequence: {count: \"3\"}, " +
				"arguments: {parameters: [{name: i, value: '{{item}}'}, {name: extra, value: v}]}}]]}\n" +
				"  - {name: c, inputs: {parameters: [{name: i}]}, container: {image: x, args: ['{{inputs.parameters.i}}']}}",
			want: []string{"- \"0\"\n", "- \"2\"\n", `argument "extra" is not an input of template "c"`},
		},
		{
			name: "bare placeholders in a manifest get quotes",
			templates: "  - name: main\n    inputs: {parameters: [{name: job, value: j}]}\n    resource:\n      action: delete\n" +
				"      manifest: |\n        apiVersion: batch/v1\n        kind: Job\n        metadata:\n          name: {{inputs.parameters.job}}\n",
			want: []string{`name: "{{inputs.parameters.job}}"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Convert([]byte(head + tt.templates))
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("err = %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := string(res[0].YAML)
			for _, w := range res[0].Warnings {
				got += w.String() + "\n"
			}
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("output does not contain %q:\n%s", w, got)
				}
			}
		})
	}
}

func TestRefusesOtherDocuments(t *testing.T) {
	_, err := Convert([]byte("apiVersion: v1\nkind: ConfigMap\nmetadata: {name: x}\n"))
	if err == nil || !strings.Contains(err.Error(), "not an Argo") {
		t.Errorf("err = %v", err)
	}
}
