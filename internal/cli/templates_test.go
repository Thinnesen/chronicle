package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

func (s *stub) ListTemplates(context.Context, *connect.Request[chroniclev1.ListTemplatesRequest]) (*connect.Response[chroniclev1.ListTemplatesResponse], error) {
	return connect.NewResponse(&chroniclev1.ListTemplatesResponse{Templates: []*chroniclev1.TemplateSummary{
		{Name: "etl", Description: "Load the sales data", Parameters: []*chroniclev1.TemplateParameter{
			{Name: "day"}, {Name: "region", Default: "eu", HasDefault: true}}},
		{Name: "base", ClusterScope: true, ManagedBy: "Argo CD"},
	}, ClusterTemplatesHidden: s.clusterHidden}), nil
}

func (s *stub) ListCronWorkflows(context.Context, *connect.Request[chroniclev1.ListCronWorkflowsRequest]) (*connect.Response[chroniclev1.ListCronWorkflowsResponse], error) {
	return connect.NewResponse(&chroniclev1.ListCronWorkflowsResponse{CronWorkflows: []*chroniclev1.CronSummary{
		{Name: "nightly", Schedules: []string{"0 2 * * *"}, Timezone: "Europe/Oslo", Template: "etl", Active: 1,
			NextScheduledAt: timestamppb.New(time.Now().Add(time.Hour))},
		{Name: "weekly", Schedules: []string{"0 3 * * 0", "0 4 * * 3"}, Suspend: true},
	}}), nil
}

func (s *stub) SuspendCronWorkflow(_ context.Context, r *connect.Request[chroniclev1.SuspendCronWorkflowRequest]) (*connect.Response[chroniclev1.SuspendCronWorkflowResponse], error) {
	s.requests = append(s.requests, r.Msg)
	return connect.NewResponse(&chroniclev1.SuspendCronWorkflowResponse{}), nil
}

func (s *stub) ResumeCronWorkflow(_ context.Context, r *connect.Request[chroniclev1.ResumeCronWorkflowRequest]) (*connect.Response[chroniclev1.ResumeCronWorkflowResponse], error) {
	s.requests = append(s.requests, r.Msg)
	return connect.NewResponse(&chroniclev1.ResumeCronWorkflowResponse{}), nil
}

func TestTemplates(t *testing.T) {
	out, err := run(t, &stub{}, "templates")
	if err != nil {
		t.Fatal(err)
	}
	// The name of a cluster template is the value for submit --from.
	for _, want := range []string{"NAME", "etl", "day, region=eu", "Load the sales data", "cluster/base", "Argo CD"} {
		if !strings.Contains(out, want) {
			t.Errorf("output has no %q:\n%s", want, out)
		}
	}
}

func TestCron(t *testing.T) {
	out, err := run(t, &stub{}, "cron", "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SCHEDULE", "nightly", "0 2 * * * (Europe/Oslo)", "in 1h", "weekly", "0 3 * * 0, 0 4 * * 3", "suspended"} {
		if !strings.Contains(out, want) {
			t.Errorf("output has no %q:\n%s", want, out)
		}
	}

	s := &stub{}
	for _, args := range [][]string{{"cron", "suspend", "nightly"}, {"cron", "resume", "nightly"}} {
		out, err := run(t, s, args...)
		if err != nil || !strings.Contains(out, "team-a/nightly") {
			t.Errorf("%v: %q %v", args, out, err)
		}
	}
	if len(s.requests) != 2 || s.requests[0].(*chroniclev1.SuspendCronWorkflowRequest).Name != "nightly" ||
		s.requests[1].(*chroniclev1.ResumeCronWorkflowRequest).Namespace != "team-a" {
		t.Errorf("requests = %+v", s.requests)
	}
}

func TestTemplatesSaysWhenClusterTemplatesAreHidden(t *testing.T) {
	if out, err := run(t, &stub{clusterHidden: true}, "templates"); err != nil || !strings.Contains(out, "You may not list the cluster templates") {
		t.Errorf("templates: %v %q", err, out)
	}
}
