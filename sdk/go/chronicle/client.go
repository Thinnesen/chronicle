package chronicle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"

	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/gen/chronicle/v1/chroniclev1connect"
)

// Client submits workflows to the chronicle API server.
type Client struct {
	// Workflows is the generated API client. Use it for the calls that Client does not wrap.
	Workflows chroniclev1connect.WorkflowServiceClient
	// Tasks is the API of the task workers. Worker uses it.
	Tasks chroniclev1connect.TaskServiceClient
}

// NewClient returns a client for the server URL, for example "https://chronicle.example.com".
// The token is an OIDC ID token or a Kubernetes service account token with the audience "chronicle".
func NewClient(server, token string) (*Client, error) {
	if server == "" || token == "" {
		return nil, errors.New("give the server URL and a token")
	}
	hc := &http.Client{Transport: bearer{token: token, next: http.DefaultTransport}}
	return &Client{
		Workflows: chroniclev1connect.NewWorkflowServiceClient(hc, server),
		Tasks:     chroniclev1connect.NewTaskServiceClient(hc, server),
	}, nil
}

// Submit starts a run of the workflow in a namespace and returns the name of the run.
func (c *Client) Submit(ctx context.Context, namespace string, w *Workflow) (string, error) {
	obj, err := w.Object()
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return "", fmt.Errorf("encode the workflow: %w", err)
	}
	var s structpb.Struct
	if err := s.UnmarshalJSON(data); err != nil {
		return "", fmt.Errorf("encode the workflow: %w", err)
	}
	resp, err := c.Workflows.SubmitWorkflow(ctx, connect.NewRequest(&chroniclev1.SubmitWorkflowRequest{
		Namespace: namespace, Source: &chroniclev1.SubmitWorkflowRequest_Workflow{Workflow: &s},
	}))
	if err != nil {
		return "", fmt.Errorf("submit the workflow: %w", err)
	}
	return resp.Msg.Summary.Name, nil
}

// SubmitTemplate starts a run of a WorkflowTemplate with parameters and returns the name of the run. For a
// ClusterWorkflowTemplate, give the template as "cluster/<name>".
func (c *Client) SubmitTemplate(
	ctx context.Context, namespace, template string, params map[string]string,
) (string, error) {
	name, cluster := strings.CutPrefix(template, "cluster/")
	source := &chroniclev1.TemplateSource{Name: name, ClusterScope: cluster}
	resp, err := c.Workflows.SubmitWorkflow(ctx, connect.NewRequest(&chroniclev1.SubmitWorkflowRequest{
		Namespace: namespace, Parameters: params, Source: &chroniclev1.SubmitWorkflowRequest_Template{Template: source},
	}))
	if err != nil {
		return "", fmt.Errorf("submit the template %s: %w", template, err)
	}
	return resp.Msg.Summary.Name, nil
}

// Phase returns the phase of a run: Pending, Running, Succeeded, Failed, Error or Suspended.
func (c *Client) Phase(ctx context.Context, namespace, name string) (string, error) {
	req := connect.NewRequest(&chroniclev1.GetWorkflowRequest{Namespace: namespace, Name: name})
	resp, err := c.Workflows.GetWorkflow(ctx, req)
	if err != nil {
		return "", fmt.Errorf("get the run: %w", err)
	}
	return resp.Msg.Summary.Phase, nil
}

// Wait reads a run every interval until it finishes, and returns its final phase: Succeeded, Failed or Error.
// A read that fails with an error that can heal, for example while the server restarts, is tried again.
// Wait stops when ctx ends; give ctx a deadline for a time limit.
func (c *Client) Wait(ctx context.Context, namespace, name string, every time.Duration) (string, error) {
	for {
		phase, err := c.Phase(ctx, namespace, name)
		switch {
		case err != nil && !healable(err):
			return "", err
		case phase == "Succeeded" || phase == "Failed" || phase == "Error":
			return phase, nil
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("wait for the run: %w", ctx.Err())
		case <-time.After(every):
		}
	}
}

// healable tells if an API error can go away by itself.
func healable(err error) bool {
	switch connect.CodeOf(err) {
	case connect.CodeUnavailable, connect.CodeUnknown, connect.CodeInternal, connect.CodeDeadlineExceeded:
		return true
	}
	return false
}

// bearer adds the token to each request.
type bearer struct {
	token string
	next  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(r)
}
