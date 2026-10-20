package validate

import "testing"

func TestSpecChildWorkflow(t *testing.T) {
	child := func(args string) string {
		return "entrypoint: c\ntemplates: [{name: c, outputs: {parameters: [{name: rows}]}, childWorkflow: {workflowTemplateRef: {name: etl}, arguments: " + args + "}}]"
	}
	runCases(t, []specCase{
		{name: "valid", spec: child(`{parameters: [{name: day, value: "{{workflow.name}}"}]}`)},
		{name: "no value", spec: child(`{parameters: [{name: day}]}`), want: []string{"needs a value"}},
		{name: "artifacts", spec: child(`{artifacts: [{name: a, raw: {data: x}}]}`), want: []string{"cannot get artifacts"}},
		{name: "unknown placeholder", spec: child(`{parameters: [{name: day, value: "{{inputs.parameters.nope}}"}]}`), want: []string{"inputs.parameters.nope"}},
	}, true)
}
