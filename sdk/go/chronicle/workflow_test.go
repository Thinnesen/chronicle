package chronicle

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/gen/chronicle/v1/chroniclev1connect"
)

func TestSpecErrors(t *testing.T) {
	echo := Container("echo", "busybox", "echo")
	echo.Args(echo.Input("msg"))
	tests := []struct {
		name  string
		build func() *Workflow
		want  string
	}{
		{"no entrypoint", func() *Workflow { return New("x-") }, "no entrypoint"},
		{"missing argument", func() *Workflow {
			d := DAG("main")
			d.Task("a", echo)
			return New("x-").Entrypoint(d)
		}, `parameter "msg" of template "echo"`},
		{"two templates with one name", func() *Workflow {
			d := DAG("main")
			d.Task("a", Container("c", "busybox"))
			d.Task("b", Container("c", "alpine"))
			return New("x-").Entrypoint(d)
		}, `two different templates have the name "c"`},
		{"task on a container", func() *Workflow {
			c := Container("c", "busybox")
			c.Task("a", echo)
			return New("x-").Entrypoint(c)
		}, `template "c" is not a DAG`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.build().Spec(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

type submitStub struct {
	chroniclev1connect.UnimplementedWorkflowServiceHandler
	req  *chroniclev1.SubmitWorkflowRequest
	auth string
	// phases are the answers of GetWorkflow, in order; "" is an Unavailable error.
	phases []string
}

func (s *submitStub) GetWorkflow(context.Context, *connect.Request[chroniclev1.GetWorkflowRequest]) (*connect.Response[chroniclev1.GetWorkflowResponse], error) {
	p := s.phases[0]
	if len(s.phases) > 1 {
		s.phases = s.phases[1:]
	}
	if p == "" {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("the server restarts"))
	}
	return connect.NewResponse(&chroniclev1.GetWorkflowResponse{Summary: &chroniclev1.WorkflowSummary{Phase: p}}), nil
}

func (s *submitStub) SubmitWorkflow(
	_ context.Context, r *connect.Request[chroniclev1.SubmitWorkflowRequest],
) (*connect.Response[chroniclev1.SubmitWorkflowResponse], error) {
	s.req, s.auth = r.Msg, r.Header().Get("Authorization")
	sum := &chroniclev1.WorkflowSummary{Name: "hello-x1"}
	return connect.NewResponse(&chroniclev1.SubmitWorkflowResponse{Summary: sum}), nil
}

func TestSubmit(t *testing.T) {
	stub := &submitStub{}
	mux := http.NewServeMux()
	mux.Handle(chroniclev1connect.NewWorkflowServiceHandler(stub))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	w := New("hello-").Entrypoint(Container("hi", "busybox", "echo", "hi"))
	name, err := c.Submit(context.Background(), "team-a", w)
	if err != nil || name != "hello-x1" {
		t.Fatalf("submit: %q, %v", name, err)
	}
	wf := stub.req.GetWorkflow().AsMap()
	if stub.auth != "Bearer tok" || stub.req.Namespace != "team-a" || wf["kind"] != "Workflow" {
		t.Errorf("request = %v, auth %q", wf, stub.auth)
	}
}

func TestWait(t *testing.T) {
	stub := &submitStub{phases: []string{"", "Running", "Succeeded"}}
	mux := http.NewServeMux()
	mux.Handle(chroniclev1connect.NewWorkflowServiceHandler(stub))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := NewClient(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if phase, err := c.Wait(context.Background(), "team-a", "x", time.Millisecond); err != nil || phase != "Succeeded" {
		t.Fatalf("wait: %q, %v", phase, err)
	}
}
