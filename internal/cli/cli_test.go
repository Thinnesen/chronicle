package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"

	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/gen/chronicle/v1/chroniclev1connect"
)

// stub is a fake server. It records the requests and answers with a run in a fixed phase.
type stub struct {
	chroniclev1connect.UnimplementedWorkflowServiceHandler
	chroniclev1connect.UnimplementedLogServiceHandler
	chroniclev1connect.UnimplementedTemplateServiceHandler
	chroniclev1connect.UnimplementedCronServiceHandler
	phase    string
	requests []any
	// listed is the last list request.
	listed *chroniclev1.ListWorkflowsRequest
	auth   string
	// restarted is the Restarted condition status of an applied retry ("True" or "False"), or empty.
	restarted string
	// breakLogs makes the first log stream fail after two lines, as during a server restart.
	breakLogs bool
	streams   int
	// failGets makes this many reads of a run fail with Unavailable first.
	failGets int
	// gates is the number of suspend nodes of the run that wait for an approval.
	gates int
	// archiveDown makes the list say that the archive did not answer.
	archiveDown   bool
	tries         []*chroniclev1.Try // the earlier tries of the run
	exitHandler   bool               // the run has an exit handler node
	clusterHidden bool               // the caller may not list cluster templates
	child         bool               // the run is a child run, and its node a starts a child run
}

func (s *stub) SubmitWorkflow(_ context.Context, r *connect.Request[chroniclev1.SubmitWorkflowRequest]) (*connect.Response[chroniclev1.SubmitWorkflowResponse], error) {
	s.requests, s.auth = append(s.requests, r.Msg), r.Header().Get("Authorization")
	return connect.NewResponse(&chroniclev1.SubmitWorkflowResponse{Summary: &chroniclev1.WorkflowSummary{Name: "hello-abc"}}), nil
}

func (s *stub) GetWorkflow(_ context.Context, r *connect.Request[chroniclev1.GetWorkflowRequest]) (*connect.Response[chroniclev1.GetWorkflowResponse], error) {
	if s.failGets > 0 {
		s.failGets--
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("the server restarts"))
	}
	status := map[string]any{"outputs": map[string]any{"parameters": []any{map[string]any{"name": "version", "value": "1.4.2"}},
		"artifacts": []any{map[string]any{"name": "report", "key": "k"}}}, "nodes": map[string]any{
		r.Msg.Name: map[string]any{"id": r.Msg.Name, "name": r.Msg.Name, "type": "DAG", "phase": s.phase, "children": []any{"n1"}},
		"n1":       map[string]any{"id": "n1", "name": r.Msg.Name + ".a", "displayName": "a", "type": "Pod", "phase": s.phase},
	}}
	nodes := status["nodes"].(map[string]any)
	if s.exitHandler {
		nodes["x"] = map[string]any{"id": "x", "name": r.Msg.Name + ".onExit", "displayName": r.Msg.Name + ".onExit", "type": "Pod", "phase": "Failed", "message": "notify failed"}
	}
	for i := range s.gates {
		id := fmt.Sprintf("g%d", i)
		nodes[id] = map[string]any{"id": id, "name": fmt.Sprintf("%s.gate%d", r.Msg.Name, i), "type": "Suspend", "phase": "Running"}
	}
	spec := map[string]any{}
	if s.restarted != "" {
		spec["restart"] = map[string]any{"generation": 1}
		status["restarts"] = 1
		status["conditions"] = []any{map[string]any{"type": "Restarted", "status": s.restarted, "reason": "Test",
			"message": `the run has no node "x"`, "lastTransitionTime": "2026-01-01T00:00:00Z"}}
	}
	phase := s.phase
	if r.Msg.Try != nil {
		// An earlier try comes from the archive, with its own retry count.
		phase, status["restarts"] = fmt.Sprintf("Failed in try %d", *r.Msg.Try), *r.Msg.Try
	}
	meta := map[string]any{"name": r.Msg.Name}
	if s.child {
		meta["annotations"] = map[string]any{"chronicle.thinnesen.com/parent": "release-q4"}
		n1 := nodes["n1"].(map[string]any)
		n1["type"], n1["childWorkflow"] = "ChildWorkflow", r.Msg.Name+"-0a1b2c3d"
	}
	wf, _ := structpb.NewStruct(map[string]any{"metadata": meta, "spec": spec, "status": status})
	return connect.NewResponse(&chroniclev1.GetWorkflowResponse{Workflow: wf, Tries: s.tries,
		Summary: &chroniclev1.WorkflowSummary{Name: r.Msg.Name, Namespace: r.Msg.Namespace, Phase: phase, NodesTotal: 1,
			ApprovalsWaiting: int32(s.gates), Archived: r.Msg.Try != nil}}), nil
}

func (s *stub) ListWorkflows(_ context.Context, r *connect.Request[chroniclev1.ListWorkflowsRequest]) (*connect.Response[chroniclev1.ListWorkflowsResponse], error) {
	s.listed = r.Msg
	return connect.NewResponse(&chroniclev1.ListWorkflowsResponse{ArchiveUnavailable: s.archiveDown, Workflows: []*chroniclev1.WorkflowSummary{{Namespace: "team-a", Name: "run-1", Phase: "Running", NodesDone: 1, NodesTotal: 3, ApprovalsWaiting: 1}, {Name: "run-2", Phase: "Running", WaitingReason: "Unschedulable"}, {Name: "run-3", Phase: "Succeeded", Archived: true}}}), nil
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
	if !s.breakLogs {
		return st.Send(&chroniclev1.StreamLogsResponse{NodeName: "hello.a", Line: "hi"})
	}
	s.streams++
	lines := []string{"one", "two", "three"}
	if s.streams == 1 {
		for _, l := range lines[:2] {
			_ = st.Send(&chroniclev1.StreamLogsResponse{NodeName: "hello.a", Line: l})
		}
		return connect.NewError(connect.CodeUnavailable, errors.New("the server restarts"))
	}
	for _, l := range lines {
		_ = st.Send(&chroniclev1.StreamLogsResponse{NodeName: "hello.a", Line: l})
	}
	return nil
}

// run runs the CLI with arguments against the stub and returns the output and the error.
func run(t *testing.T, s *stub, args ...string) (string, error) {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(chroniclev1connect.NewWorkflowServiceHandler(s))
	mux.Handle(chroniclev1connect.NewLogServiceHandler(s))
	mux.Handle(chroniclev1connect.NewTemplateServiceHandler(s))
	mux.Handle(chroniclev1connect.NewCronServiceHandler(s))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := (&Config{Server: srv.URL, Namespace: "team-a", Token: "tok"}).save(cfgPath); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cmd := NewCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
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

// TestRetryWatch checks that a retry that finished between two polls, or that the controller refused, ends the watch.
func TestRetryWatch(t *testing.T) {
	s := &stub{phase: "Succeeded", restarted: "True"}
	if out, err := run(t, s, "retry", "x", "--node", "x.a", "--watch"); err != nil || !strings.Contains(out, "x Succeeded") {
		t.Errorf("applied retry: %v\n%s", err, out)
	}
	s.restarted = "False"
	if _, err := run(t, s, "retry", "x", "--node", "x.b", "--watch"); err == nil || !strings.Contains(err.Error(), "refused the retry") {
		t.Errorf("refused retry: %v", err)
	}
}

func TestCommands(t *testing.T) {
	s := &stub{phase: "Running"}
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"list"}, "1/3"},
		{[]string{"list"}, "waiting for an approval"},
		{[]string{"list"}, "a step waits: Unschedulable"},
		{[]string{"list"}, "only in the archive"},
		{[]string{"get", "hello"}, "  ● a"},
		{[]string{"logs", "hello"}, "hello.a: hi"},
		{[]string{"approve", "hello", "hello.gate", "--reject", "-m", "no"}, "Rejected hello.gate in team-a/hello"},
		{[]string{"retry", "hello", "--node", "hello.a"}, "Retrying team-a/hello"},
		{[]string{"rerun", "hello", "-p", "x=1"}, "Started team-a/hello-new"},
		{[]string{"submit", "--from", "cluster/base"}, "Started team-a/hello-abc"},
		{[]string{"convert", "-f", "../convert/testdata/argo/hello-world.yaml"}, "apiVersion: chronicle.thinnesen.com/v1alpha1"},
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

func TestGetChild(t *testing.T) {
	out, err := run(t, &stub{phase: "Running", child: true}, "get", "nightly")
	if err != nil || !strings.Contains(out, "Parent:    release-q4") || !strings.Contains(out, "child run nightly-0a1b2c3d") {
		t.Errorf("err %v, output:\n%s", err, out)
	}
}

func TestSubmitAt(t *testing.T) {
	s := &stub{}
	out, err := run(t, s, "submit", "--from", "etl", "--at", "2026-10-21T03:00:00+02:00")
	if err != nil || !strings.HasPrefix(out, "Submitted team-a/hello-abc. It starts at ") {
		t.Fatalf("err %v, output %s", err, out)
	}
	if got := s.requests[0].(*chroniclev1.SubmitWorkflowRequest).StartAt.AsTime(); !got.Equal(time.Date(2026, 10, 21, 1, 0, 0, 0, time.UTC)) {
		t.Errorf("start time = %v", got)
	}
	if _, err := run(t, s, "submit", "--from", "etl", "--at", "2h"); err != nil {
		t.Fatal(err)
	}
	if got := time.Until(s.requests[1].(*chroniclev1.SubmitWorkflowRequest).StartAt.AsTime()); got < 119*time.Minute || got > 2*time.Hour {
		t.Errorf("start in %v", got)
	}
	if _, err := run(t, s, "submit", "--from", "etl", "--at", "tomorrow"); err == nil {
		t.Error("a bad start time gave no error")
	}
}

func TestListAllNamespaces(t *testing.T) {
	s := &stub{}
	out, err := run(t, s, "list", "-A")
	if err != nil || !strings.HasPrefix(out, "NAMESPACE") || !strings.Contains(out, "team-a") || !s.listed.AllNamespaces || s.listed.Namespace != "" {
		t.Errorf("err %v, request %v, output:\n%s", err, s.listed, out)
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

func TestChangeOrder(t *testing.T) {
	at := func(s int64) *metav1.Time { return &metav1.Time{Time: time.Unix(s, 0)} }
	changes := []v1alpha1.NodeStatus{
		{Name: "r", FinishedAt: at(20)},
		{Name: "r.load", StartedAt: at(10), FinishedAt: at(20)},
		{Name: "r.b", StartedAt: at(10)},
		{Name: "r.a", StartedAt: at(5), FinishedAt: at(10)},
	}
	slices.SortStableFunc(changes, changeOrder)
	got := make([]string, 0, len(changes))
	for _, n := range changes {
		got = append(got, n.Name)
	}
	if want := []string{"r.a", "r.b", "r.load", "r"}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

// TestReadObjectYAML12 checks that submit reads YAML 1.2: "n", "yes" and dates stay strings, as a user expects.
func TestReadObjectYAML12(t *testing.T) {
	file := filepath.Join(t.TempDir(), "wf.yaml")
	src := "base: &base {image: busybox}\nimage: {<<: *base, command: [run]}\n" +
		"spec:\n  arguments:\n    parameters:\n    - {name: n, value: yes}\n    - {name: day, value: 2026-10-19}\n    - {name: count, value: 3}\n"
	if err := os.WriteFile(file, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	obj, err := readObject(file)
	if err != nil {
		t.Fatal(err)
	}
	params := obj.Fields["spec"].GetStructValue().Fields["arguments"].GetStructValue().Fields["parameters"].GetListValue().Values
	first := params[0].GetStructValue().Fields
	if first["name"].GetStringValue() != "n" || first["value"].GetStringValue() != "yes" {
		t.Errorf("first parameter = %v", first)
	}
	if day := params[1].GetStructValue().Fields["value"].GetStringValue(); day != "2026-10-19" {
		t.Errorf("day = %q", day)
	}
	if img := obj.Fields["image"].GetStructValue().Fields["image"].GetStringValue(); img != "busybox" {
		t.Errorf("the merge key gave image %q", img)
	}
}

// TestLogsReconnect checks that a followed log connects again after a broken stream and prints no line twice.
func TestLogsReconnect(t *testing.T) {
	reconnectWait = time.Millisecond
	s := &stub{phase: "Running", breakLogs: true}
	out, err := run(t, s, "logs", "hello", "-f")
	if err != nil || out != "hello.a: one\nhello.a: two\nThe log stream stopped. Connecting again…\nhello.a: three\n" || s.streams != 2 {
		t.Fatalf("out %q, err %v, %d streams", out, err, s.streams)
	}
}

// TestWatchRetries checks that a watch survives a failed read of the run, as during a server restart.
func TestWatchRetries(t *testing.T) {
	pollInterval = time.Millisecond
	s := &stub{phase: "Succeeded", failGets: 1}
	if out, err := run(t, s, "watch", "x"); err != nil || !strings.Contains(out, "x Succeeded") {
		t.Fatalf("out %q, err %v", out, err)
	}
}
