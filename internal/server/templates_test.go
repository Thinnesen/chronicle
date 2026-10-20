package server

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/gen/chronicle/v1/chroniclev1connect"
)

// userHeader sends the test identity on each call.
func userHeader(user, group string) connect.ClientOption {
	return connect.WithInterceptors(connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			req.Header().Set("X-Test-User", user)
			req.Header().Set("X-Test-Group", group)
			return next(ctx, req)
		}
	}))
}

func TestTemplatesAndCrons(t *testing.T) {
	srv := setup(t)
	ctx := context.Background()
	spec := v1alpha1.WorkflowTemplateSpec{
		Entrypoint: "c",
		Arguments: v1alpha1.Arguments{Parameters: []v1alpha1.Parameter{
			{Name: "env", Default: new("dev"), Enum: []string{"dev", "prod"}, Description: "Where to deploy"}, {Name: "tag"},
			{Name: "region", ValueFrom: &v1alpha1.ValueFrom{ConfigMapKeyRef: &v1alpha1.ConfigMapKeyRef{Name: "settings", Key: "region"}}},
		}},
		Templates: []v1alpha1.Template{{Name: "c", Container: &v1alpha1.ContainerTemplate{Image: "alpine"}}},
	}
	objs := []client.Object{
		&v1alpha1.WorkflowTemplate{Name: "deploy", Namespace: "team-a", Annotations: map[string]string{DescriptionAnnotation: "Deploys the API"}, Spec: spec},
		&v1alpha1.ClusterWorkflowTemplate{Name: "shared", Spec: spec},
		&v1alpha1.CronWorkflow{Name: "nightly", Namespace: "team-a", Spec: v1alpha1.CronWorkflowSpec{Schedules: []string{"0 3 * * *"},
			WorkflowSpec: v1alpha1.WorkflowSpec{WorkflowTemplateRef: &v1alpha1.WorkflowTemplateRef{Name: "deploy"}}}},
		&v1alpha1.CronWorkflow{Name: "gitops", Namespace: "team-a", Labels: map[string]string{"kustomize.toolkit.fluxcd.io/name": "apps"},
			Spec: v1alpha1.CronWorkflowSpec{Schedules: []string{"@daily"}, WorkflowSpec: v1alpha1.WorkflowSpec{WorkflowTemplateRef: &v1alpha1.WorkflowTemplateRef{Name: "deploy"}}}},
	}
	for _, o := range objs {
		if err := admin.Create(ctx, o); client.IgnoreAlreadyExists(err) != nil {
			t.Fatal(err)
		}
	}
	templates := chroniclev1connect.NewTemplateServiceClient(srv.Client(), srv.URL, userHeader("alice", ""))
	list, err := templates.ListTemplates(ctx, connect.NewRequest(&chroniclev1.ListTemplatesRequest{Namespace: "team-a"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Msg.Templates) != 1 {
		t.Fatalf("alice cannot list cluster templates, but sees %d templates", len(list.Msg.Templates))
	}
	d := list.Msg.Templates[0]
	if d.Description != "Deploys the API" || len(d.Parameters) != 3 || d.Parameters[0].Default != "dev" || !d.Parameters[0].HasDefault || d.Parameters[1].HasDefault ||
		d.Parameters[2].ConfigMapName != "settings" || d.Parameters[2].ConfigMapKey != "region" {
		t.Errorf("summary = %+v", d)
	}
	if got, err := templates.GetTemplate(ctx, connect.NewRequest(&chroniclev1.GetTemplateRequest{Namespace: "team-a", Name: "deploy"})); err != nil || got.Msg.Template.Fields["spec"] == nil {
		t.Errorf("get: %v", err)
	} else {
		checkKind(t, got.Msg.Template, "WorkflowTemplate")
	}
	if _, err := chroniclev1connect.NewTemplateServiceClient(srv.Client(), srv.URL, userHeader("carol", "oidc:viewers")).ListTemplates(ctx,
		connect.NewRequest(&chroniclev1.ListTemplatesRequest{Namespace: "team-a"})); code(err) != connect.CodePermissionDenied {
		t.Errorf("a user without template access: %v", err)
	}

	crons := chroniclev1connect.NewCronServiceClient(srv.Client(), srv.URL, userHeader("alice", ""))
	cl, err := crons.ListCronWorkflows(ctx, connect.NewRequest(&chroniclev1.ListCronWorkflowsRequest{Namespace: "team-a"}))
	if err != nil || len(cl.Msg.CronWorkflows) != 2 || cl.Msg.CronWorkflows[0].ManagedBy != "Flux" || cl.Msg.CronWorkflows[1].Template != "deploy" {
		t.Fatalf("crons: %v %+v", err, cl)
	}
	sus, err := crons.SuspendCronWorkflow(ctx, connect.NewRequest(&chroniclev1.SuspendCronWorkflowRequest{Namespace: "team-a", Name: "nightly"}))
	if err != nil || !sus.Msg.Summary.Suspend {
		t.Errorf("suspend: %v", err)
	}
	if _, err := crons.ResumeCronWorkflow(ctx, connect.NewRequest(&chroniclev1.ResumeCronWorkflowRequest{Namespace: "team-a", Name: "nightly"})); err != nil {
		t.Errorf("resume: %v", err)
	}
	_, err = crons.SuspendCronWorkflow(ctx, connect.NewRequest(&chroniclev1.SuspendCronWorkflowRequest{Namespace: "team-a", Name: "gitops"}))
	if code(err) != connect.CodeFailedPrecondition {
		t.Errorf("suspend of a GitOps object: %v", err)
	}
	if _, err := crons.GetCronWorkflow(ctx, connect.NewRequest(&chroniclev1.GetCronWorkflowRequest{Namespace: "team-a", Name: "nightly"})); err != nil {
		t.Errorf("get cron: %v", err)
	}
}
