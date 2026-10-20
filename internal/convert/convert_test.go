package convert

import (
	"os"
	"path/filepath"
	"regexp"
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
		{file: "exit-handlers.yaml"},
		{file: "continue-on-fail.yaml"},
		{file: "global-outputs.yaml"},
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
			name:      "an artifact path outside /tmp",
			templates: "  - {name: main, outputs: {artifacts: [{name: o, path: /bin/out}]}, container: {image: x, command: [run]}}",
			want:      []string{"path: /bin/out", "cannot create /bin/out: use a path below /tmp"},
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
			name: "template defaults",
			templates: "  - {name: main, steps: [[{name: a, template: c}, {name: b, template: d}, {name: e, inline: {container: {image: z}}}]]}\n" +
				"  - {name: c, container: {image: x, command: [run]}}\n" +
				"  - {name: d, retryStrategy: {limit: 5}, script: {image: py, source: date}}\n" +
				"  templateDefaults: {retryStrategy: {limit: \"2\"}, container: {imagePullPolicy: Always}}",
			want: []string{"- container:\n      command:\n      - run\n      image: x\n      imagePullPolicy: Always\n    name: c\n    retryStrategy:\n      limit: 2",
				"retryStrategy:\n      limit: 5\n    script:\n      image: py\n      source: date",
				"- container:\n      image: z\n      imagePullPolicy: Always\n    name: main-e"},
		},
		{
			name: "volumes, claims and mounts",
			templates: "  - {name: main, container: {image: x, command: [run], volumeMounts: [{name: work, mountPath: /work}, {name: data, mountPath: /data}, " +
				"{name: tmp, mountPath: /scratch, readOnly: true}]}}\n" +
				"  volumeClaimTemplates: [{metadata: {name: work}, spec: {accessModes: [ReadWriteOnce], storageClassName: fast, resources: {requests: {storage: 1Gi}}}}]\n" +
				"  volumes: [{name: data, persistentVolumeClaim: {claimName: old}}, {name: tmp, emptyDir: {}}]\n  volumeClaimGC: {strategy: OnWorkflowCompletion}",
			want: []string{"volumeClaimTemplates:\n  - accessModes:\n    - ReadWriteOnce\n    name: work\n    storage: 1Gi\n    storageClassName: fast",
				"volumes:\n  - emptyDir: {}\n    name: tmp", "- mountPath: /scratch\n        name: tmp\n        readOnly: true",
				"spec.volumes[0]: chronicle supports emptyDir, secret and configMap volumes: the volume data is dropped",
				"spec.templates[0].container.volumeMounts[1]: the volume data is dropped, so the mount is dropped",
				"spec.volumeClaimGC: chronicle deletes the claims of a run that succeeded, and keeps them for a retry of a run that failed"},
		},
		{
			name:      "the first lock stays",
			templates: "  - {name: main, container: {image: x}}\n  synchronization: {mutexes: [{name: deploy}], semaphores: [{configMapKeyRef: {name: c, key: k}}]}",
			want:      []string{"synchronization:\n    mutex:\n      name: deploy", "it keeps the first lock"},
		},
		{
			name:      "a semaphore reads its limit from the ConfigMap",
			templates: "  - {name: main, container: {image: x}}\n  synchronization: {semaphore: {configMapKeyRef: {name: my-config, key: Workflow_Limit}}}",
			want: []string{"semaphore:\n      configMapKeyRef:\n        key: Workflow_Limit\n        name: my-config\n      name: my-config-workflow-limit",
				`give ConfigMap my-config the label chronicle.thinnesen.com/parameters: "true"`},
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
			want: []string{"withSequence:\n          count: \"3\"", `argument "extra" is not an input of template "c"`},
		},
		{
			name: "inline templates become named templates",
			templates: "  - {name: main, steps: [[{name: a, inline: {container: {image: x}}}], [{name: b.c, inline: {container: {image: z}}}]]}\n" +
				"  - {name: main-a, container: {image: w}}",
			want: []string{"template: main-a-2", "name: main-a-2", "template: main-b-c", "spec.templates[0].steps[0][0].inline: the inline template is now the template \"main-a-2\""},
		},
		{
			name: "raw and http artifacts stay, other sources and http headers go",
			templates: "  - {name: main, inputs: {artifacts: [{name: t, path: /tmp/t, mode: 0755}, {name: s, path: /tmp/s, optional: true, s3: {key: k}}]}, " +
				"outputs: {artifacts: [{name: o, path: /tmp/o, http: {url: 'https://e.com/up'}}]}, container: {image: x, command: [run]}}\n" +
				"  arguments: {artifacts: [{name: t, http: {url: 'https://e.com/t', headers: [{name: a, value: b}]}}]}",
			want: []string{"url: https://e.com/t", "mode: 493", "spec.arguments.artifacts[0].http.headers: chronicle does not support",
				"spec.templates[0].inputs.artifacts[1].s3: chronicle does not support", "spec.templates[0].outputs.artifacts[0].http: chronicle does not support"},
		},
		{
			name: "an http template",
			templates: "  - name: main\n    http:\n      url: https://e.com\n      method: POST\n      timeoutSeconds: 20\n" +
				"      successCondition: response.statusCode == 201\n      headers: [{name: A, valueFrom: {secretKeyRef: {name: s, key: k}}}]\n",
			want: []string{"method: POST", "secretKeyRef:", "http.successCondition: chronicle has no expressions", "http.timeoutSeconds: chronicle does not support"},
		},
		{
			name: "a sequence with a format",
			templates: "  - {name: main, steps: [[{name: s, template: c, withSequence: {start: \"9\", count: \"2\", format: \"day-%02d\"}, " +
				"arguments: {parameters: [{name: i, value: '{{item}}'}]}}]]}\n" +
				"  - {name: c, inputs: {parameters: [{name: i}]}, container: {image: x, args: ['{{inputs.parameters.i}}']}}",
			want: []string{"format: day-%02d"},
		},
		{
			name:      "a sequence with a format that is not one number",
			templates: "  - {name: main, steps: [[{name: s, template: c, withSequence: {count: \"2\", format: \"%s-%d\"}}]]}\n  - {name: c, container: {image: x}}",
			err:       "format must have one number verb",
		},
		{
			name: "valueFrom.parameter becomes a value",
			templates: "  - {name: main, outputs: {parameters: [{name: o, valueFrom: {parameter: '{{steps.s.outputs.result}}'}}]}, steps: [[{name: s, template: c}]]}\n" +
				"  - {name: c, container: {image: x, command: [run]}}",
			want: []string{"value: '{{steps.s.outputs.result}}'"},
		},
		{
			name: "a jq filter output is dropped",
			templates: "  - name: main\n    outputs: {parameters: [{name: job, valueFrom: {jsonPath: '{.metadata.name}'}}, {name: obj, valueFrom: {jqFilter: '.'}}]}\n" +
				"    resource: {action: get, manifest: '{apiVersion: v1, kind: ConfigMap, metadata: {name: c}}'}\n",
			want: []string{"jsonPath: '{.metadata.name}'", "outputs.parameters[1]: chronicle has no jq filters"},
		},
		{
			name: "a parameter from a ConfigMap",
			templates: "  - {name: main, inputs: {parameters: [{name: m, valueFrom: {configMapKeyRef: {name: cfg, key: msg}, default: hi}}]}, " +
				"container: {image: x, args: ['{{inputs.parameters.m}}']}}",
			want: []string{"configMapKeyRef:\n            key: msg\n            name: cfg", "default: hi", `give ConfigMap cfg the label chronicle.thinnesen.com/parameters: "true"`},
		},
		{
			name:      "a template mutex stays",
			templates: "  - {name: main, synchronization: {mutex: {name: db}}, container: {image: x, command: [migrate]}}",
			want:      []string{"synchronization:\n      mutex:\n        name: db"},
		},
		{
			name:      "memoize keeps the key and the age",
			templates: "  - {name: main, memoize: {key: k1, maxAge: 1h, cache: {configMap: {name: c}}}, container: {image: x, command: [run]}}",
			want:      []string{"memoize:\n      key: k1\n      maxAge: 1h", "memoize.cache: chronicle keeps the results in its archive database"},
		},
		{
			name: "exit hooks of steps",
			templates: "  - {name: main, steps: [[{name: a, template: c, hooks: {exit: {template: c}, running: {expression: 'x', template: c}}}, {name: b, template: c, onExit: c}]]}\n" +
				"  - {name: c, container: {image: x}}",
			want: []string{"onExit:\n          template: c", "steps[0][0].hooks.running: chronicle has only exit hooks"},
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

func TestLockName(t *testing.T) {
	long := lockName(strings.Repeat("Config.Map", 10) + "-")
	if len(long) > 48 || !regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`).MatchString(long) {
		t.Errorf("lockName gave %q, which is not a valid lock name", long)
	}
	if lockName("_a_") != "a" {
		t.Errorf("lockName(_a_) = %q, want a", lockName("_a_"))
	}
}
