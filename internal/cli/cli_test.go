package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"

	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/gen/chronicle/v1/chroniclev1connect"
)

// stub is a fake server. It records the requests and answers with a run in a fixed phase.
type stub struct {
	chroniclev1connect.UnimplementedWorkflowServiceHandler
	chroniclev1connect.UnimplementedLogServiceHandler
	phase    string
	requests []any
	auth     string
}

func (s *stub) SubmitWorkflow(_ context.Context, r *connect.Request[chroniclev1.SubmitWorkflowRequest]) (*connect.Response[chroniclev1.SubmitWorkflowResponse], error) {
	s.requests, s.auth = append(s.requests, r.Msg), r.Header().Get("Authorization")
	return connect.NewResponse(&chroniclev1.SubmitWorkflowResponse{Summary: &chroniclev1.WorkflowSummary{Name: "hello-abc"}}), nil
}

func (s *stub) GetWorkflow(_ context.Context, r *connect.Request[chroniclev1.GetWorkflowRequest]) (*connect.Response[chroniclev1.GetWorkflowResponse], error) {
	wf, _ := structpb.NewStruct(map[string]any{
		"metadata": map[string]any{"name": r.Msg.Name},
		"status": map[string]any{"nodes": map[string]any{
			r.Msg.Name: map[string]any{"id": r.Msg.Name, "name": r.Msg.Name, "type": "DAG", "phase": s.phase, "children": []any{"n1"}},
			"n1":       map[string]any{"id": "n1", "name": r.Msg.Name + ".a", "displayName": "a", "type": "Pod", "phase": s.phase},
		}},
	})
	return connect.NewResponse(&chroniclev1.GetWorkflowResponse{Workflow: wf,
		Summary: &chroniclev1.WorkflowSummary{Name: r.Msg.Name, Namespace: r.Msg.Namespace, Phase: s.phase, NodesTotal: 1}}), nil
}

func (s *stub) ListWorkflows(context.Context, *connect.Request[chroniclev1.ListWorkflowsRequest]) (*connect.Response[chroniclev1.ListWorkflowsResponse], error) {
	return connect.NewResponse(&chroniclev1.ListWorkflowsResponse{Workflows: []*chroniclev1.WorkflowSummary{{Name: "run-1", Phase: "Running", NodesDone: 1, NodesTotal: 3}}}), nil
}

func (s *stub) ApproveNode(_ context.Context, r *connect.Request[chroniclev1.ApproveNodeRequest]) (*connect.Response[chroniclev1.ApproveNodeResponse], error) {
	s.requests = append(s.requests, r.Msg)
	return connect.NewResponse(&chroniclev1.ApproveNodeResponse{}), nil
}

func (s *stub) RetryWorkflow(_ context.Context, r *connect.Request[chroniclev1.RetryWorkflowRequest]) (*connect.Response[chroniclev1.RetryWorkflowResponse], error) {
	s.requests = append(s.requests, r.Msg)
	return connect.NewResponse(&chroniclev1.RetryWorkflowResponse{}), nil
}

func (s *stub) RerunWorkflow(_ context.Context, r *connect.Request[chroniclev1.RerunWorkflowRequest]) (*connect.Response[chroniclev1.RerunWorkflowResponse], error) {
	s.requests = append(s.requests, r.Msg)
	return connect.NewResponse(&chroniclev1.RerunWorkflowResponse{Summary: &chroniclev1.WorkflowSummary{Name: "hello-new"}}), nil
}

func (s *stub) StreamLogs(_ context.Context, _ *connect.Request[chroniclev1.StreamLogsRequest], st *connect.ServerStream[chroniclev1.StreamLogsResponse]) error {
	return st.Send(&chroniclev1.StreamLogsResponse{NodeName: "hello.a", Line: "hi"})
}

// run runs the CLI with arguments against the stub and returns the output and the error.
func run(t *testing.T, s *stub, args ...string) (string, error) {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(chroniclev1connect.NewWorkflowServiceHandler(s))
	mux.Handle(chroniclev1connect.NewLogServiceHandler(s))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := (&Config{Server: srv.URL, Namespace: "team-a", Token: "tok"}).save(cfgPath); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cmd := NewCommand()
	cmd.SetOut(&out)
	cmd.SetArgs(append([]string{"--config", cfgPath}, args...))
	cmd.SetContext(context.Background())
	err := cmd.Execute()
	return out.String(), err
}

func TestSubmitAndWatch(t *testing.T) {
	file := filepath.Join(t.TempDir(), "wf.yaml")
	if err := os.WriteFile(file, []byte("metadata: {generateName: hello-}\nspec: {entrypoint: c}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &stub{phase: "Succeeded"}
	out, err := run(t, s, "submit", "-f", file, "-p", "who=world", "--watch")
	if err != nil {
		t.Fatal(err)
	}
	req := s.requests[0].(*chroniclev1.SubmitWorkflowRequest)
	if req.Namespace != "team-a" || req.Parameters["who"] != "world" || req.GetWorkflow().Fields["spec"] == nil || s.auth != "Bearer tok" {
		t.Errorf("request = %+v, auth %q", req, s.auth)
	}
	if !strings.Contains(out, "Started team-a/hello-abc") || !strings.Contains(out, "hello-abc Succeeded") {
		t.Errorf("output:\n%s", out)
	}
	s.phase = "Failed"
	if _, err := run(t, s, "watch", "x"); err != errRunFailed {
		t.Errorf("watch of a failed run: %v", err)
	}
}

func TestCommands(t *testing.T) {
	s := &stub{phase: "Running"}
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"list"}, "run-1  Running  1/3"},
		{[]string{"get", "hello"}, "● hello\n  ● a"},
		{[]string{"logs", "hello"}, "hello.a: hi"},
		{[]string{"approve", "hello", "hello.gate", "--reject", "-m", "no"}, "Rejected hello.gate in team-a/hello"},
		{[]string{"retry", "hello", "--node", "hello.a"}, "Retrying team-a/hello"},
		{[]string{"rerun", "hello", "-p", "x=1"}, "Started team-a/hello-new"},
		{[]string{"submit", "--from", "cluster/base"}, "Started team-a/hello-abc"},
	}
	for _, tt := range tests {
		t.Run(tt.args[0], func(t *testing.T) {
			out, err := run(t, s, tt.args...)
			if err != nil || !strings.Contains(out, tt.want) {
				t.Errorf("err %v, output:\n%s", err, out)
			}
		})
	}
	last := s.requests[len(s.requests)-1].(*chroniclev1.SubmitWorkflowRequest)
	if tmpl := last.GetTemplate(); tmpl.Name != "base" || !tmpl.ClusterScope {
		t.Errorf("template source = %+v", tmpl)
	}
	approve := s.requests[0].(*chroniclev1.ApproveNodeRequest)
	if approve.Approve || approve.Message != "no" {
		t.Errorf("approve = %+v", approve)
	}
	for _, bad := range [][]string{{"submit"}, {"submit", "-f", "x", "--from", "y"}, {"submit", "--from", "t", "-p", "nokey"}, {"get", "x", "-o", "xml"}} {
		if _, err := run(t, s, bad...); err == nil {
			t.Errorf("%v returned no error", bad)
		}
	}
}

func TestLoginWithToken(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "sub", "config.json")
	cmd := NewCommand()
	cmd.SetArgs([]string{"--config", cfgPath, "login", "--server", "https://chronicle.example/", "-n", "team-a", "--token-file", "-"})
	cmd.SetIn(strings.NewReader("sa-token\n"))
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	c, err := loadConfig(cfgPath)
	if err != nil || c.Token != "sa-token" || c.Server != "https://chronicle.example" || c.Namespace != "team-a" {
		t.Fatalf("config = %+v, %v", c, err)
	}
	if fi, _ := os.Stat(cfgPath); fi.Mode().Perm() != 0o600 {
		t.Errorf("config mode = %v", fi.Mode().Perm())
	}
}
