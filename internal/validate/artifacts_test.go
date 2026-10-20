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
