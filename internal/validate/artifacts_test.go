package validate

import "testing"

// TestArtifactSources checks raw and http artifacts: in a template input, in an argument and in the workflow arguments.
func TestArtifactSources(t *testing.T) {
	const use = "- {name: use, inputs: {artifacts: [{name: f, path: /tmp/f}]}, container: {image: x, command: [cat, /tmp/f]}}\n"
	runCases(t, []specCase{
		{name: "raw and http template inputs", spec: `
entrypoint: main
arguments: {parameters: [{name: v, value: "1"}]}
templates:
- name: main
  inputs:
    parameters: [{name: tag, value: "{{workflow.parameters.v}}"}]
    artifacts:
    - {name: a, path: /tmp/a, raw: {data: "hello {{not.a.placeholder}}"}}
    - {name: b, path: /tmp/b, mode: 493, http: {url: "https://example.com/{{inputs.parameters.tag}}/tool"}}
  container: {image: x, command: [ls]}`},
		{name: "workflow arguments give the entrypoint artifact", spec: `
entrypoint: use
arguments: {artifacts: [{name: f, http: {url: "https://example.com/f"}}]}
templates:
` + use},
		{name: "a step gives a raw argument", spec: `
entrypoint: main
templates:
- name: main
  steps: [[{name: s, template: use, arguments: {artifacts: [{name: f, raw: {data: x}}]}}]]
` + use},
		{name: "two sources", spec: `
entrypoint: main
templates:
- name: main
  inputs: {artifacts: [{name: a, path: /tmp/a, raw: {data: x}, http: {url: "https://e.com"}}]}
  container: {image: x, command: [ls]}`, want: []string{"one source"}},
		{name: "raw output", spec: `
entrypoint: main
templates:
- name: main
  outputs: {artifacts: [{name: a, path: /tmp/a, raw: {data: x}}]}
  container: {image: x, command: [ls]}`, want: []string{"only an input or an argument can have raw or http"}},
		{name: "unknown placeholder in a url", spec: `
entrypoint: main
templates:
- name: main
  inputs: {artifacts: [{name: a, path: /tmp/a, http: {url: "https://e.com/{{inputs.parameters.nope}}"}}]}
  container: {image: x, command: [ls]}`, want: []string{`unknown placeholder "inputs.parameters.nope"`}},
		{name: "workflow argument with from", spec: `
entrypoint: use
arguments: {artifacts: [{name: f, from: "{{tasks.a.outputs.artifacts.b}}"}]}
templates:
` + use, want: []string{"a workflow artifact needs raw or http"}},
		{name: "workflow argument for no input", spec: `
entrypoint: use
arguments: {artifacts: [{name: f, raw: {data: x}}, {name: other, raw: {data: x}}]}
templates:
` + use, want: []string{`spec.arguments.artifacts[1].name: Not found: "other"`}},
	}, true)
}

func TestHTTPTemplate(t *testing.T) {
	runCases(t, []specCase{
		{name: "valid", spec: `
entrypoint: main
templates:
- name: main
  steps: [[{name: hook, template: hook}], [{name: u, template: u, arguments: {parameters: [{name: r, value: "{{steps.hook.outputs.result}}"}]}}]]
- name: hook
  http: {url: "https://e.com/{{workflow.name}}", headers: [{name: A, value: "{{workflow.uid}}"}], body: "{{pod.name}}"}
- {name: u, inputs: {parameters: [{name: r}]}, container: {image: x}}`},
		{name: "unknown placeholders", spec: `
entrypoint: main
templates:
- name: main
  http: {url: "https://e.com/{{a.b}}", headers: [{name: A, value: "{{c.d}}"}], body: "{{e.f}}"}`, want: []string{`"a.b"`, `"c.d"`, `"e.f"`}},
		{name: "no input artifacts", spec: `
entrypoint: main
templates:
- name: main
  inputs: {artifacts: [{name: f, path: /tmp/f, raw: {data: x}}]}
  http: {url: "https://e.com"}`, want: []string{"cannot have input artifacts"}},
	}, true)
}

func TestWithSequence(t *testing.T) {
	const c = "- {name: c, inputs: {parameters: [{name: x}]}, container: {image: x, args: ['{{inputs.parameters.x}}']}}"
	call := func(seq string) string {
		return "entrypoint: main\narguments: {parameters: [{name: num, value: '3'}]}\ntemplates:\n- name: main\n  dag:\n    tasks:\n" +
			"    - {name: s, template: c, withSequence: " + seq + ", arguments: {parameters: [{name: x, value: '{{item}}'}]}}\n" + c
	}
	runCases(t, []specCase{
		{name: "count from a parameter", spec: call(`{count: "{{workflow.parameters.num}}", format: "r%02d"}`)},
		{name: "count and end", spec: call("{count: 2, end: 3}"), want: []string{"give count or end"}},
		{name: "unknown placeholder", spec: call(`{end: "{{workflow.parameters.nope}}"}`), want: []string{`unknown placeholder "workflow.parameters.nope"`}},
		{name: "bad format", spec: call(`{count: 2, format: "%s"}`), want: []string{"format must have one number verb"}},
	}, true)
}

func TestConfigMapParameters(t *testing.T) {
	const ref = "valueFrom: {configMapKeyRef: {name: settings, key: region}}"
	runCases(t, []specCase{
		{name: "workflow and input parameters", spec: `
entrypoint: main
arguments: {parameters: [{name: region, ` + ref + `}]}
templates:
- name: main
  inputs: {parameters: [{name: zone, ` + ref + `}]}
  container: {image: x, args: ["{{inputs.parameters.zone}}", "{{workflow.parameters.region}}"]}`},
		{name: "an output", spec: `
entrypoint: main
templates:
- name: main
  outputs: {parameters: [{name: o, ` + ref + `}]}
  container: {image: x, command: [run]}`, want: []string{"only a workflow parameter or an input parameter can read a ConfigMap"}},
	}, true)
}

func TestTemplateLock(t *testing.T) {
	runCases(t, []specCase{
		{name: "a container", spec: "entrypoint: c\ntemplates:\n- {name: c, synchronization: {mutex: {name: prod}}, container: {image: x}}"},
		{name: "a dag", spec: "entrypoint: m\ntemplates:\n- {name: m, synchronization: {mutex: {name: prod}}, dag: {tasks: [{name: a, template: c}]}}\n- {name: c, container: {image: x}}",
			want: []string{"only a container, script, resource, http or task template can hold a lock"}},
	}, true)
}

func TestMemoize(t *testing.T) {
	runCases(t, []specCase{
		{name: "a container with an input in the key", spec: "entrypoint: c\ntemplates:\n- {name: c, inputs: {parameters: [{name: day, value: x}]}, memoize: {key: 'r-{{inputs.parameters.day}}'}, container: {image: x}}"},
		{name: "a dag", spec: "entrypoint: m\ntemplates:\n- {name: m, memoize: {key: k}, dag: {tasks: [{name: a, template: c}]}}\n- {name: c, container: {image: x}}",
			want: []string{"only a container, script, resource or http template can have memoize"}},
		{name: "an unknown placeholder", spec: "entrypoint: c\ntemplates:\n- {name: c, memoize: {key: '{{inputs.parameters.nope}}'}, container: {image: x}}",
			want: []string{`unknown placeholder "inputs.parameters.nope"`}},
	}, true)
}

func TestExitHook(t *testing.T) {
	const tmpl = "- {name: c, container: {image: x}}\n- {name: r, inputs: {parameters: [{name: s}]}, container: {image: x}}"
	runCases(t, []specCase{
		{name: "a hook that reads the status of its task", spec: "entrypoint: m\ntemplates:\n- {name: m, dag: {tasks: [{name: a, template: c, " +
			"onExit: {template: r, arguments: {parameters: [{name: s, value: '{{tasks.a.status}}'}]}}}]}}\n" + tmpl},
		{name: "an unknown template", spec: "entrypoint: m\ntemplates:\n- {name: m, steps: [[{name: a, template: c, onExit: {template: nope}}]]}\n" + tmpl,
			want: []string{`steps[0][0].onExit.template: Not found: "nope"`}},
		{name: "a missing argument", spec: "entrypoint: m\ntemplates:\n- {name: m, steps: [[{name: a, template: c, onExit: {template: r}}]]}\n" + tmpl,
			want: []string{`parameter "s" of template "r"`}},
	}, true)
}
