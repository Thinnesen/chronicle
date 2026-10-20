package server

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

func TestSubmitChecksTheTemplate(t *testing.T) {
	srv := setup(t)
	ctx := context.Background()
	tmpl := &v1alpha1.WorkflowTemplate{Name: "release", Namespace: "team-a", Spec: v1alpha1.WorkflowTemplateSpec{
		Entrypoint: "c",
		Arguments:  v1alpha1.Arguments{Parameters: []v1alpha1.Parameter{{Name: "env", Value: new("staging"), Enum: []string{"staging", "prod"}}}},
		Templates:  []v1alpha1.Template{{Name: "c", Container: &v1alpha1.ContainerTemplate{Image: "x"}}},
	}}
	if err := admin.Create(ctx, tmpl); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Delete(ctx, tmpl) })
	submitTo := func(template, env string) error {
		_, err := newClient(srv, "alice").SubmitWorkflow(ctx, connect.NewRequest(&chroniclev1.SubmitWorkflowRequest{
			Namespace: "team-a", Source: &chroniclev1.SubmitWorkflowRequest_Template{Template: &chroniclev1.TemplateSource{Name: template}},
			Parameters: map[string]string{"env": env}}))
		return err
	}
	submit := func(env string) error { return submitTo("release", env) }
	if err := submitTo("nope", "prod"); code(err) != connect.CodeNotFound {
		t.Errorf("a template that does not exist: %v", err)
	}
	if err := submit("hacked"); code(err) != connect.CodeInvalidArgument {
		t.Errorf("a value outside the enum: %v", err)
	}
	if err := submit("prod"); err != nil {
		t.Errorf("an allowed value: %v", err)
	}
	// A typo in a parameter name must not start a run with the defaults.
	_, err := newClient(srv, "alice").SubmitWorkflow(ctx, connect.NewRequest(&chroniclev1.SubmitWorkflowRequest{
		Namespace: "team-a", Source: &chroniclev1.SubmitWorkflowRequest_Template{Template: &chroniclev1.TemplateSource{Name: "release"}},
		Parameters: map[string]string{"envronment": "prod"}}))
	if code(err) != connect.CodeInvalidArgument || !strings.Contains(err.Error(), `no parameter "envronment"`) {
		t.Errorf("an unknown parameter: %v", err)
	}
}
