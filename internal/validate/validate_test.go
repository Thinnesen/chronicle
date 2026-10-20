package validate

import (
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

func parseSpec(t *testing.T, s string) *v1alpha1.WorkflowTemplateSpec {
	t.Helper()
	var spec v1alpha1.WorkflowTemplateSpec
	if err := yaml.UnmarshalStrict([]byte(s), &spec); err != nil {
		t.Fatalf("bad test spec: %v", err)
	}
	return &spec
}

type specCase struct {
	name string
	spec string
	// want lists substrings of the expected errors. Empty means valid.
	want []string
	// n is the number of expected errors, if it is not len(want).
	n int
}

func runCases(t *testing.T, cases []specCase, forWorkflow bool) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			errs := Spec(parseSpec(t, tt.spec), forWorkflow)
			got := errs.ToAggregate()
			if len(tt.want) == 0 {
				if got != nil {
					t.Fatalf("unexpected errors: %v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("no errors, want %q", tt.want)
			}
			n := tt.n
			if n == 0 {
				n = len(tt.want)
			}
			if len(errs) != n {
				t.Errorf("got %d errors, want %d: %v", len(errs), n, got)
			}
			for _, w := range tt.want {
				if !strings.Contains(got.Error(), w) {
					t.Errorf("errors %q do not contain %q", got.Error(), w)
				}
			}
		})
	}
}

const valid = `
entrypoint: main
arguments:
  parameters:
  - name: greeting
    value: hello
templates:
- name: main
  dag:
    tasks:
    - name: a
      template: echo
      arguments:
        parameters:
        - name: msg
          value: "{{workflow.parameters.greeting}} from {{workflow.name}}"
    - name: b
      template: echo
      dependencies: [a]
      arguments:
        parameters:
        - name: msg
          value: "{{tasks.a.outputs.parameters.out}} {{tasks.a.outputs.result}}"
    - name: c
      template: steps
      depends: "a.Succeeded && (b || b.Failed)"
- name: echo
  inputs:
    parameters:
    - name: msg
    - name: color
      default: blue
      enum: [blue, red]
  outputs:
    parameters:
    - name: out
      valueFrom:
        path: /tmp/out
  container:
    image: alpine:3
    command: [echo, "{{inputs.parameters.msg}}"]
    env:
    - name: COLOR
      value: "{{inputs.parameters.color}}"
- name: steps
  steps:
  - - name: one
      template: echo
      arguments:
        parameters: [{name: msg, value: x}]
  - - name: two
      template: script
      arguments:
        parameters: [{name: msg, value: "{{steps.one.outputs.parameters.out}}"}]
- name: script
  inputs:
    parameters: [{name: msg}]
  script:
    image: python:3
    command: [python]
    env: [{name: MSG, value: "{{inputs.parameters.msg}}"}]
    source: print("hi")
`

func TestSpecValid(t *testing.T) {
	runCases(t, []specCase{{name: "full example", spec: valid}}, true)
}

func TestSpecTemplates(t *testing.T) {
	runCases(t, []specCase{
		{
			name: "unknown entrypoint",
			spec: "entrypoint: nope\ntemplates: [{name: a, container: {image: x}}]",
			want: []string{`entrypoint: Not found: "nope"`},
		},
		{
			name: "parallelism on a container template",
			spec: "entrypoint: a\ntemplates: [{name: a, parallelism: 2, container: {image: x}}]",
			want: []string{`templates[0].parallelism: Invalid value: 2: only a dag or steps template can limit its parallelism`},
		},
		{
			name: "unknown exit handler",
			spec: "entrypoint: a\nonExit: nope\ntemplates: [{name: a, container: {image: x}}]",
			want: []string{`onExit: Not found: "nope"`},
		},
		{
			name: "unknown task template",
			spec: "entrypoint: m\ntemplates: [{name: m, dag: {tasks: [{name: a, template: nope}]}}]",
			want: []string{`templates[0].dag.tasks[0].template: Not found: "nope"`},
		},
		{
			name: "unknown step template",
			spec: "entrypoint: m\ntemplates: [{name: m, steps: [[{name: a, template: nope}]]}]",
			want: []string{`templates[0].steps[0][0].template: Not found: "nope"`},
		},
		{
			name: "duplicate step name across groups",
			spec: `
entrypoint: m
templates:
- {name: m, steps: [[{name: a, template: c}], [{name: a, template: c}]]}
- {name: c, container: {image: x}}`,
			want: []string{`templates[0].steps[1][0].name: Duplicate value: "a"`},
		},
		{
			name: "placeholder in script source",
			spec: "entrypoint: s\ntemplates: [{name: s, script: {image: x, source: 'echo {{workflow.name}}'}}]",
			want: []string{"templates[0].script.source", "use environment variables"}, n: 1,
		},
		{
			name: "output parameter without source",
			spec: "entrypoint: c\ntemplates: [{name: c, outputs: {parameters: [{name: o}]}, container: {image: x}}]",
			want: []string{"templates[0].outputs.parameters[0]: Required value"},
		},
		{
			name: "output file without a command",
			spec: "entrypoint: c\ntemplates: [{name: c, outputs: {parameters: [{name: o, valueFrom: {path: /x}}]}, container: {image: x}}]",
			want: []string{"needs a command"},
		},
		{
			name: "placeholder in a manifest key",
			spec: "entrypoint: r\ntemplates: [{name: r, resource: {action: apply, manifest: \"apiVersion: v1\\nkind: ConfigMap\\nmetadata: {name: x}\\ndata: {'{{workflow.name}}': v}\"}}]",
			want: []string{"placeholders can be in values only"},
		},
		{
			name: "manifest without a name",
			spec: "entrypoint: r\ntemplates: [{name: r, resource: {action: apply, manifest: \"apiVersion: v1\\nkind: ConfigMap\"}}]",
			want: []string{"needs apiVersion, kind and metadata.name"},
		},
		{
			name: "jsonPath on a container",
			spec: "entrypoint: c\ntemplates: [{name: c, outputs: {parameters: [{name: o, valueFrom: {jsonPath: '{.a}'}}]}, container: {image: x, command: [a]}}]",
			want: []string{"only a resource template can use it"},
		},
		{
			name: "output path on a dag template",
			spec: `
entrypoint: m
templates:
- {name: m, outputs: {parameters: [{name: o, valueFrom: {path: /x}}]}, dag: {tasks: [{name: a, template: c}]}}
- {name: c, container: {image: x}}`,
			want: []string{"templates[0].outputs.parameters[0].valueFrom.path: Forbidden"},
		},
	}, true)
}

func TestSpecDAG(t *testing.T) {
	const tmpl = "\n- {name: c, container: {image: x}}"
	runCases(t, []specCase{
		{
			name: "cycle",
			spec: `
entrypoint: m
templates:
- name: m
  dag:
    tasks:
    - {name: a, template: c, dependencies: [c1]}
    - {name: b, template: c, depends: a}
    - {name: c1, template: c, depends: b.Succeeded}` + tmpl,
			want: []string{"templates[0].dag.tasks: Invalid value", "a -> c1 -> b -> a"}, n: 1,
		},
		{
			name: "self dependency",
			spec: "entrypoint: m\ntemplates:\n- {name: m, dag: {tasks: [{name: a, template: c, dependencies: [a]}]}}" + tmpl,
			want: []string{"a -> a"},
		},
		{
			name: "unknown dependency",
			spec: "entrypoint: m\ntemplates:\n- {name: m, dag: {tasks: [{name: a, template: c, dependencies: [x]}]}}" + tmpl,
			want: []string{`templates[0].dag.tasks[0].dependencies[0]: Not found: "x"`},
		},
		{
			name: "unknown task in depends",
			spec: "entrypoint: m\ntemplates:\n- {name: m, dag: {tasks: [{name: a, template: c, depends: 'x.Failed'}]}}" + tmpl,
			want: []string{`templates[0].dag.tasks[0].depends: Not found: "x"`},
		},
		{
			name: "bad depends syntax",
			spec: "entrypoint: m\ntemplates:\n- {name: m, dag: {tasks: [{name: a, template: c, depends: 'a &&'}]}}" + tmpl,
			want: []string{"templates[0].dag.tasks[0].depends: Invalid value"},
		},
		{
			name: "output of a task that is not a dependency",
			spec: `
entrypoint: m
templates:
- name: m
  dag:
    tasks:
    - {name: a, template: c}
    - {name: b, template: p, arguments: {parameters: [{name: x, value: "{{tasks.a.outputs.result}}"}]}}` + tmpl + `
- {name: p, inputs: {parameters: [{name: x}]}, container: {image: x}}`,
			want: []string{`must depend on task "a"`},
		},
		{
			name: "unknown output parameter",
			spec: `
entrypoint: m
templates:
- name: m
  dag:
    tasks:
    - {name: a, template: c}
    - {name: b, template: p, dependencies: [a], arguments: {parameters: [{name: x, value: "{{tasks.a.outputs.parameters.nope}}"}]}}` + tmpl + `
- {name: p, inputs: {parameters: [{name: x}]}, container: {image: x}}`,
			want: []string{`unknown placeholder "tasks.a.outputs.parameters.nope"`},
		},
		{
			name: "output of a later step group",
			spec: `
entrypoint: m
templates:
- name: m
  steps:
  - - {name: a, template: p, arguments: {parameters: [{name: x, value: "{{steps.b.outputs.result}}"}]}}
  - - {name: b, template: c}` + tmpl + `
- {name: p, inputs: {parameters: [{name: x}]}, container: {image: x}}`,
			want: []string{`unknown placeholder "steps.b.outputs.result"`},
		},
	}, true)
}

func TestSpecParameters(t *testing.T) {
	const p = "\n- {name: p, inputs: {parameters: [{name: x}, {name: w, default: a, enum: [a, b]}, {name: z, value: fixed}]}, container: {image: x}}"
	call := func(params string) string {
		return "entrypoint: m\ntemplates:\n- {name: m, dag: {tasks: [{name: t, template: p, arguments: {parameters: " + params + "}}]}}" + p
	}
	runCases(t, []specCase{
		{name: "all given", spec: call("[{name: x, value: '1'}, {name: w, value: b}]")},
		{name: "missing required", spec: call("[]"), want: []string{`arguments.parameters: Required value: parameter "x"`}},
		{name: "unknown argument", spec: call("[{name: x, value: '1'}, {name: q, value: '1'}]"), want: []string{`arguments.parameters[1].name: Not found: "q"`}},
		{name: "argument without value", spec: call("[{name: x}]"), want: []string{"arguments.parameters[0].value: Required value"}},
		{name: "value not in enum", spec: call("[{name: x, value: '1'}, {name: w, value: c}]"), want: []string{`arguments.parameters[1].value: Unsupported value: "c"`}},
		{name: "fixed input", spec: call("[{name: x, value: '1'}, {name: z, value: other}]"), want: []string{"arguments.parameters[1].name: Forbidden"}},
		{
			name: "unknown input placeholder",
			spec: "entrypoint: c\ntemplates: [{name: c, container: {image: '{{inputs.parameters.nope}}'}}]",
			want: []string{`templates[0].container.image: Invalid value`, `unknown placeholder "inputs.parameters.nope"`}, n: 1,
		},
		{
			name: "unknown workflow parameter",
			spec: "entrypoint: c\ntemplates: [{name: c, container: {image: x, args: ['{{workflow.parameters.nope}}']}}]",
			want: []string{`templates[0].container.args[0]: Invalid value`},
		},
		{
			name: "bad placeholder syntax",
			spec: "entrypoint: c\ntemplates: [{name: c, container: {image: x, workingDir: '{{'}}]",
			want: []string{`templates[0].container.workingDir: Invalid value`},
		},
		{
			name: "default not in enum",
			spec: "entrypoint: c\ntemplates: [{name: c, inputs: {parameters: [{name: e, default: z, enum: [a]}]}, container: {image: x}}]",
			want: []string{`templates[0].inputs.parameters[0].default: Unsupported value: "z"`},
		},
	}, true)
}

func TestSpecCalls(t *testing.T) {
	const p = "\n- {name: p, inputs: {parameters: [{name: x}]}, container: {image: x, command: [echo]}}"
	task := func(fields string) string {
		return "entrypoint: m\ntemplates:\n- {name: m, dag: {tasks: [{name: g, template: p, arguments: {parameters: [{name: x, value: v}]}}, {name: t, template: p, depends: g, " + fields + "}]}}" + p
	}
	runCases(t, []specCase{
		{name: "items", spec: task("withItems: [a, {k: 1}], arguments: {parameters: [{name: x, value: '{{item}} {{item.k}}'}]}")},
		{name: "param list and when", spec: task("withParam: '{{tasks.g.outputs.result}}', when: '{{item}} != skip && {{tasks.g.outputs.result}} == x', arguments: {parameters: [{name: x, value: '{{item}}'}]}")},
		{name: "items and param list", spec: task("withItems: [a], withParam: '[]', arguments: {parameters: [{name: x, value: v}]}"), want: []string{"use one of withItems, withParam and withSequence"}},
		{name: "item without fan-out", spec: task("arguments: {parameters: [{name: x, value: '{{item}}'}]}"), want: []string{`unknown placeholder "item"`}},
		{name: "bad when", spec: task("when: 'a ==', arguments: {parameters: [{name: x, value: v}]}"), want: []string{"templates[0].dag.tasks[1].when: Invalid value"}},
		{name: "unknown placeholder in when", spec: task("when: '{{nope}} == a', arguments: {parameters: [{name: x, value: v}]}"), want: []string{`unknown placeholder "nope"`}},
		{
			name: "DAG outputs use task outputs",
			spec: "entrypoint: m\ntemplates:\n- {name: m, outputs: {parameters: [{name: o, value: '{{tasks.a.outputs.result}}'}]}, dag: {tasks: [{name: a, template: c}]}}\n- {name: c, container: {image: x, command: [echo]}}",
		},
		{
			name: "entrypoint input without an argument",
			spec: "entrypoint: p\ntemplates:" + p,
			want: []string{`parameter "x" of the entrypoint template`},
		},
	}, true)
}

func TestSpecWorkflowArguments(t *testing.T) {
	const spec = "entrypoint: c\narguments: {parameters: [{name: a}]}\ntemplates: [{name: c, container: {image: x}}]"
	runCases(t, []specCase{{name: "template needs no value", spec: spec}}, false)
	runCases(t, []specCase{{name: "workflow needs a value", spec: spec, want: []string{`arguments.parameters[0].value: Required value`}}}, true)
}

func TestSpecArtifacts(t *testing.T) {
	const tmpl = "\n- {name: make, outputs: {artifacts: [{name: data, path: /out}]}, container: {image: x, command: [run]}}" +
		"\n- {name: use, inputs: {artifacts: [{name: data, path: /in}]}, script: {image: x, source: cat /in}}"
	dag := func(task string) string {
		return "entrypoint: m\ntemplates:\n- {name: m, dag: {tasks: [{name: a, template: make}, {name: b, template: use, depends: a, " + task + "}]}}" + tmpl
	}
	runCases(t, []specCase{
		{name: "pass an artifact", spec: dag(`arguments: {artifacts: [{name: data, from: "{{tasks.a.outputs.artifacts.data}}"}]}`)},
		{name: "missing argument", spec: dag(""), want: []string{`artifact "data" of template "use"`}},
		{name: "from is text", spec: dag(`arguments: {artifacts: [{name: data, from: "x{{tasks.a.outputs.artifacts.data}}"}]}`), want: []string{"from must be one artifact placeholder"}},
		{name: "unknown artifact", spec: dag(`arguments: {artifacts: [{name: data, from: "{{tasks.a.outputs.artifacts.nope}}"}]}`), want: []string{`unknown placeholder "tasks.a.outputs.artifacts.nope"`}},
		{
			name: "artifact placeholder in text",
			spec: dag(`arguments: {artifacts: [{name: data, from: "{{tasks.a.outputs.artifacts.data}}"}]}`) +
				"\n- {name: echo, container: {image: x, command: [echo, '{{workflow.name}}'], args: ['{{inputs.artifacts.data}}']}, inputs: {artifacts: [{name: data, path: /d}]}}",
			want: []string{"an artifact placeholder can be used only in arguments.artifacts[].from"},
		},
		{
			name: "fan-out has no artifact outputs",
			spec: "entrypoint: m\ntemplates:\n- {name: m, dag: {tasks: [{name: a, template: make, withItems: [x]}, {name: b, template: use, depends: a, arguments: {artifacts: [{name: data, from: '{{tasks.a.outputs.artifacts.data}}'}]}}]}}" + tmpl,
			want: []string{`unknown placeholder "tasks.a.outputs.artifacts.data"`},
		},
		{
			name: "templates that cannot use files",
			spec: "entrypoint: m\ntemplates:\n- {name: m, outputs: {artifacts: [{name: o, path: /o}]}, dag: {tasks: [{name: a, template: c}]}}" +
				"\n- {name: c, inputs: {artifacts: [{name: i, path: /i, optional: true}]}, outputs: {artifacts: [{name: o}]}, container: {image: x}}",
			want: []string{"an output artifact of a dag or steps template needs only from", "needs a command to download", "needs a command to save"},
			n:    3,
		},
		{name: "entrypoint needs an artifact", spec: "entrypoint: use\ntemplates:" + tmpl, want: []string{"give it in spec.arguments.artifacts"}},
	}, true)
}
