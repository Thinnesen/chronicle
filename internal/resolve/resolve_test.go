package resolve

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/validate"
)

// fake holds template sets by "cluster/name" or "ns/name".
type fake map[string]string

func (f fake) Get(_ context.Context, name string, cluster bool) (*v1alpha1.WorkflowTemplateSpec, error) {
	key := "ns/" + name
	if cluster {
		key = "cluster/" + name
	}
	s, ok := f[key]
	if !ok {
		return nil, fmt.Errorf("%s not found", key)
	}
	var spec v1alpha1.WorkflowTemplateSpec
	if err := yaml.UnmarshalStrict([]byte(s), &spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

var sets = fake{
	"ns/pipeline": `
entrypoint: main
arguments: {parameters: [{name: env, value: dev}, {name: tag, value: latest}]}
serviceAccountName: builder
templates:
- {name: main, dag: {tasks: [{name: b, templateRef: {name: lib, template: build}}]}}`,
	"ns/lib": `
templates:
- name: build
  steps:
  - - {name: compile, template: compile}
  - - {name: say, templateRef: {name: base, template: echo, clusterScope: true}}
- {name: compile, container: {image: golang, command: [go, build]}}
- {name: ping, steps: [[{name: pong, template: pong}]]}
- {name: pong, steps: [[{name: ping, template: ping}]]}`,
	"cluster/base": `templates: [{name: echo, container: {image: busybox, command: [echo]}}]`,
}

func parse(t *testing.T, s string) *v1alpha1.WorkflowSpec {
	t.Helper()
	var spec v1alpha1.WorkflowSpec
	if err := yaml.UnmarshalStrict([]byte(s), &spec); err != nil {
		t.Fatal(err)
	}
	return &spec
}

func TestWorkflow(t *testing.T) {
	spec := parse(t, "workflowTemplateRef: {name: pipeline}\narguments: {parameters: [{name: env, value: prod}, {name: extra, value: x}]}\nserviceAccountName: deployer")
	if !Needed(spec) {
		t.Fatal("Needed() = false")
	}
	out, err := Workflow(context.Background(), sets, spec)
	if err != nil {
		t.Fatal(err)
	}
	if errs := validate.Spec(out, true); len(errs) > 0 {
		t.Fatalf("the resolved spec is invalid: %v", errs)
	}
	names := make([]string, 0, len(out.Templates))
	for _, tmpl := range out.Templates {
		names = append(names, tmpl.Name)
	}
	// A copy comes after the templates that it uses.
	if got := strings.Join(names, " "); got != "main lib--compile cluster--base--echo lib--build" {
		t.Errorf("templates = %s", got)
	}
	args := out.Arguments.Parameters
	if len(args) != 3 || *args[0].Value != "prod" || *args[1].Value != "latest" || args[2].Name != "extra" || out.ServiceAccountName != "deployer" {
		t.Errorf("merged spec = %+v", out)
	}
	if c := out.Templates[3].Steps[1][0].Call; c.Template != "cluster--base--echo" || c.TemplateRef != nil {
		t.Errorf("nested call = %+v", c)
	}
}

func TestWorkflowCycleAndErrors(t *testing.T) {
	ctx := context.Background()
	out, err := Workflow(ctx, sets, parse(t, "entrypoint: m\ntemplates: [{name: m, steps: [[{name: p, templateRef: {name: lib, template: ping}}]]}]"))
	if err != nil || len(out.Templates) != 3 {
		t.Fatalf("a cycle of templates: %v, %d templates", err, len(out.Templates))
	}
	for _, tt := range []struct{ spec, want string }{
		{"workflowTemplateRef: {name: nope}", "ns/nope not found"},
		{"entrypoint: m\ntemplates: [{name: m, dag: {tasks: [{name: a, templateRef: {name: lib, template: nope}}]}}]", `template "nope" is not in WorkflowTemplate "lib"`},
		{"entrypoint: m\ntemplates: [{name: m, dag: {tasks: [{name: a, templateRef: {name: x, template: other, clusterScope: true}}]}}]", "cluster/x not found"},
		{"entrypoint: m\ntemplates: [{name: lib--compile, container: {image: x}}, {name: m, dag: {tasks: [{name: a, templateRef: {name: lib, template: compile}}]}}]", "rename the template"},
	} {
		if _, err := Workflow(ctx, sets, parse(t, tt.spec)); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: error %v, want %q", tt.spec, err, tt.want)
		}
	}
	if Needed(parse(t, "entrypoint: m\ntemplates: [{name: m, container: {image: x}}]")) {
		t.Error("Needed() = true for a plain spec")
	}
}
