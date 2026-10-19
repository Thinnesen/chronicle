package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/gen/chronicle/v1/chroniclev1connect"
	"github.com/Thinnesen/chronicle/internal/auth"
)

var (
	cfg    *rest.Config
	scheme = runtime.NewScheme()
	admin  client.Client
)

func TestMain(m *testing.M) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		os.Exit(m.Run())
	}
	_ = clientgoscheme.AddToScheme(scheme)
	_ = v1alpha1.AddToScheme(scheme)
	env := &envtest.Environment{CRDDirectoryPaths: []string{filepath.Join("..", "..", "config", "crd", "bases")}, ErrorIfCRDPathMissing: true}
	var err error
	if cfg, err = env.Start(); err != nil {
		panic(err)
	}
	if admin, err = client.New(cfg, client.Options{Scheme: scheme}); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = env.Stop()
	os.Exit(code)
}

// headerAuth is a test authenticator: the user is in the X-Test-User header.
type headerAuth struct{}

func (headerAuth) Authenticate(r *http.Request) (*auth.Identity, error) {
	if u := r.Header.Get("X-Test-User"); u != "" {
		return &auth.Identity{User: u, Groups: []string{"test:users", r.Header.Get("X-Test-Group")}}, nil
	}
	return nil, nil
}

// setup starts the API server and gives the user "alice" edit rights on Workflows in the namespace "team-a".
func setup(t *testing.T) *httptest.Server {
	t.Helper()
	if cfg == nil {
		t.Skip("envtest is not set up: run make test")
	}
	ctx := context.Background()
	objs := []client.Object{
		&corev1.Namespace{Name: "team-a"},
		&rbacv1.Role{Name: "edit", Namespace: "team-a", Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{"chronicle.thinnesen.com"}, Resources: []string{"workflows"},
			Verbs: []string{"get", "list", "create", "update", "delete"},
		}, {
			APIGroups: []string{"chronicle.thinnesen.com"}, Resources: []string{"workflowtemplates", "cronworkflows"},
			Verbs: []string{"get", "list", "update"},
		}, {
			APIGroups: []string{""}, Resources: []string{"pods/log"}, Verbs: []string{"get"},
		}, {
			APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"list"},
		}}},
		&rbacv1.RoleBinding{Name: "alice", Namespace: "team-a",
			RoleRef:  rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: "edit"},
			Subjects: []rbacv1.Subject{{APIGroup: "rbac.authorization.k8s.io", Kind: "User", Name: "alice"}}},
		&rbacv1.Role{Name: "view", Namespace: "team-a", Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{"chronicle.thinnesen.com"}, Resources: []string{"workflows"}, Verbs: []string{"get", "list"},
		}}},
		&rbacv1.RoleBinding{Name: "viewers", Namespace: "team-a",
			RoleRef:  rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: "view"},
			Subjects: []rbacv1.Subject{{APIGroup: "rbac.authorization.k8s.io", Kind: "Group", Name: "oidc:viewers"}}},
	}
	for _, o := range objs {
		if err := admin.Create(ctx, o); client.IgnoreAlreadyExists(err) != nil {
			t.Fatal(err)
		}
	}
	h, err := NewHandler(Options{Config: cfg, Scheme: scheme, Authenticators: []auth.Authenticator{headerAuth{}}})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(h)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func newClient(srv *httptest.Server, user string, opts ...connect.ClientOption) chroniclev1connect.WorkflowServiceClient {
	return newGroupClient(srv, user, "", opts...)
}

func newGroupClient(srv *httptest.Server, user, group string, opts ...connect.ClientOption) chroniclev1connect.WorkflowServiceClient {
	opts = append(opts, connect.WithInterceptors(connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if user != "" {
				req.Header().Set("X-Test-User", user)
				req.Header().Set("X-Test-Group", group)
			}
			return next(ctx, req)
		}
	})))
	return chroniclev1connect.NewWorkflowServiceClient(srv.Client(), srv.URL, opts...)
}

// noError is the code of a call without an error. Connect codes start at 1.
const noError connect.Code = 0

func code(err error) connect.Code {
	if err == nil {
		return noError
	}
	if ce, ok := errors.AsType[*connect.Error](err); ok {
		return ce.Code()
	}
	return connect.CodeUnknown
}

func TestWorkflowService(t *testing.T) {
	srv := setup(t)
	ctx := context.Background()
	alice := newClient(srv, "alice", connect.WithGRPC())

	spec, _ := structpb.NewStruct(map[string]any{
		"metadata": map[string]any{"name": "hello"},
		"spec": map[string]any{
			"entrypoint": "gate",
			"arguments":  map[string]any{"parameters": []any{map[string]any{"name": "who", "value": "x"}}},
			"templates":  []any{map[string]any{"name": "gate", "suspend": map[string]any{}}},
		},
	})
	sub, err := alice.SubmitWorkflow(ctx, connect.NewRequest(&chroniclev1.SubmitWorkflowRequest{
		Namespace: "team-a", Source: &chroniclev1.SubmitWorkflowRequest_Workflow{Workflow: spec},
		Parameters: map[string]string{"who": "world"}, Labels: map[string]string{"team": "a"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if sub.Msg.Summary.Name != "hello" || sub.Msg.Summary.Labels["team"] != "a" {
		t.Fatalf("submit summary = %+v", sub.Msg.Summary)
	}

	tmpl, err := alice.SubmitWorkflow(ctx, connect.NewRequest(&chroniclev1.SubmitWorkflowRequest{
		Namespace: "team-a", Source: &chroniclev1.SubmitWorkflowRequest_Template{Template: &chroniclev1.TemplateSource{Name: "nightly"}},
	}))
	if err != nil || tmpl.Msg.Summary.Template != "nightly" || !strings.HasPrefix(tmpl.Msg.Summary.Name, "nightly-") {
		t.Fatalf("submit from a template: %v %+v", err, tmpl)
	}

	list, err := alice.ListWorkflows(ctx, connect.NewRequest(&chroniclev1.ListWorkflowsRequest{Namespace: "team-a", LabelSelector: "team=a"}))
	if err != nil || len(list.Msg.Workflows) != 1 {
		t.Fatalf("list: %v %+v", err, list)
	}

	got, err := alice.GetWorkflow(ctx, connect.NewRequest(&chroniclev1.GetWorkflowRequest{Namespace: "team-a", Name: "hello"}))
	if err != nil {
		t.Fatal(err)
	}
	params := got.Msg.Workflow.Fields["spec"].GetStructValue().Fields["arguments"].GetStructValue().Fields["parameters"].GetListValue()
	if v := params.Values[0].GetStructValue().Fields["value"].GetStringValue(); v != "world" {
		t.Errorf("parameter who = %q", v)
	}

	// The controller does not run here, so give the run the waiting node that ApproveNode needs.
	var hello v1alpha1.Workflow
	if err := admin.Get(ctx, client.ObjectKey{Namespace: "team-a", Name: "hello"}, &hello); err != nil {
		t.Fatal(err)
	}
	hello.Status.Nodes = map[string]v1alpha1.NodeStatus{"hello": {ID: "hello", Name: "hello", Type: v1alpha1.NodeSuspend, TemplateName: "gate", Phase: v1alpha1.NodeRunning}}
	if err := admin.Status().Update(ctx, &hello); err != nil {
		t.Fatal(err)
	}

	steps := []func() error{
		func() error {
			_, err := alice.SuspendWorkflow(ctx, connect.NewRequest(&chroniclev1.SuspendWorkflowRequest{Namespace: "team-a", Name: "hello"}))
			return err
		},
		func() error {
			_, err := alice.ApproveNode(ctx, connect.NewRequest(&chroniclev1.ApproveNodeRequest{Namespace: "team-a", Name: "hello", Node: "hello", Approve: true}))
			return err
		},
		func() error {
			_, err := alice.StopWorkflow(ctx, connect.NewRequest(&chroniclev1.StopWorkflowRequest{Namespace: "team-a", Name: "hello"}))
			return err
		},
		func() error {
			_, err := alice.TerminateWorkflow(ctx, connect.NewRequest(&chroniclev1.TerminateWorkflowRequest{Namespace: "team-a", Name: "hello"}))
			return err
		},
	}
	for i, step := range steps {
		if err := step(); err != nil {
			t.Fatalf("action %d: %v", i, err)
		}
	}
	var wf v1alpha1.Workflow
	if err := admin.Get(ctx, client.ObjectKey{Namespace: "team-a", Name: "hello"}, &wf); err != nil {
		t.Fatal(err)
	}
	if !wf.Spec.Suspend || wf.Spec.Shutdown != v1alpha1.ShutdownTerminate || len(wf.Spec.Approvals) != 1 {
		t.Errorf("spec after the actions = %+v", wf.Spec)
	}
	if _, err := alice.ApproveNode(ctx, connect.NewRequest(&chroniclev1.ApproveNodeRequest{Namespace: "team-a", Name: "hello", Node: "hello"})); code(err) != connect.CodeFailedPrecondition {
		t.Errorf("a second decision: %v", err)
	}
	if _, err := alice.DeleteWorkflow(ctx, connect.NewRequest(&chroniclev1.DeleteWorkflowRequest{Namespace: "team-a", Name: "hello"})); err != nil {
		t.Fatal(err)
	}

	var events corev1.EventList
	if err := admin.List(ctx, &events, client.InNamespace("team-a")); err != nil {
		t.Fatal(err)
	}
	reasons := map[string]string{}
	for _, e := range events.Items {
		if e.InvolvedObject.Name == "hello" {
			reasons[e.Reason] = e.Message
		}
	}
	for _, want := range []string{"SubmitWorkflow", "SuspendWorkflow", "ApproveNode", "StopWorkflow", "TerminateWorkflow", "DeleteWorkflow"} {
		if !strings.Contains(reasons[want], "by alice") {
			t.Errorf("audit event %s = %q", want, reasons[want])
		}
	}
	if !strings.Contains(reasons["ApproveNode"], `node "hello", approve true`) {
		t.Errorf("approval event = %q", reasons["ApproveNode"])
	}
	if _, ok := reasons["GetWorkflow"]; ok || len(reasons) != 6 {
		t.Errorf("events = %v (reads and refused calls must not write events)", reasons)
	}
}

func TestWorkflowServiceErrors(t *testing.T) {
	srv := setup(t)
	ctx := context.Background()
	tests := []struct {
		name string
		call func() error
		want connect.Code
	}{
		{"no identity", func() error {
			_, err := newClient(srv, "").ListWorkflows(ctx, connect.NewRequest(&chroniclev1.ListWorkflowsRequest{Namespace: "team-a"}))
			return err
		}, connect.CodeUnauthenticated},
		{"no permission", func() error {
			_, err := newClient(srv, "bob").ListWorkflows(ctx, connect.NewRequest(&chroniclev1.ListWorkflowsRequest{Namespace: "team-a"}))
			return err
		}, connect.CodePermissionDenied},
		{"a viewer group cannot submit", func() error {
			_, err := newGroupClient(srv, "carol", "oidc:viewers").SubmitWorkflow(ctx, connect.NewRequest(&chroniclev1.SubmitWorkflowRequest{
				Namespace: "team-a", Source: &chroniclev1.SubmitWorkflowRequest_Template{Template: &chroniclev1.TemplateSource{Name: "t"}}}))
			return err
		}, connect.CodePermissionDenied},
		{"a viewer group can list", func() error {
			_, err := newGroupClient(srv, "carol", "oidc:viewers").ListWorkflows(ctx, connect.NewRequest(&chroniclev1.ListWorkflowsRequest{Namespace: "team-a"}))
			return err
		}, noError},
		{"other namespace", func() error {
			_, err := newClient(srv, "alice").ListWorkflows(ctx, connect.NewRequest(&chroniclev1.ListWorkflowsRequest{Namespace: "default"}))
			return err
		}, connect.CodePermissionDenied},
		{"no namespace", func() error {
			_, err := newClient(srv, "alice").ListWorkflows(ctx, connect.NewRequest(&chroniclev1.ListWorkflowsRequest{}))
			return err
		}, connect.CodeInvalidArgument},
		{"invalid workflow", func() error {
			spec, _ := structpb.NewStruct(map[string]any{"spec": map[string]any{"entrypoint": "x"}})
			_, err := newClient(srv, "alice").SubmitWorkflow(ctx, connect.NewRequest(&chroniclev1.SubmitWorkflowRequest{
				Namespace: "team-a", Source: &chroniclev1.SubmitWorkflowRequest_Workflow{Workflow: spec}}))
			return err
		}, connect.CodeInvalidArgument},
		{"unknown field", func() error {
			spec, _ := structpb.NewStruct(map[string]any{"spec": map[string]any{"entrypoint": "x", "bogus": 1}})
			_, err := newClient(srv, "alice").SubmitWorkflow(ctx, connect.NewRequest(&chroniclev1.SubmitWorkflowRequest{
				Namespace: "team-a", Source: &chroniclev1.SubmitWorkflowRequest_Workflow{Workflow: spec}}))
			return err
		}, connect.CodeInvalidArgument},
		{"not found", func() error {
			_, err := newClient(srv, "alice").GetWorkflow(ctx, connect.NewRequest(&chroniclev1.GetWorkflowRequest{Namespace: "team-a", Name: "nope"}))
			return err
		}, connect.CodeNotFound},
		{"system user", func() error {
			_, err := newClient(srv, "system:admin").ListWorkflows(ctx, connect.NewRequest(&chroniclev1.ListWorkflowsRequest{Namespace: "team-a"}))
			return err
		}, connect.CodePermissionDenied},
		{"bad page token", func() error {
			_, err := newClient(srv, "alice").ListWorkflows(ctx, connect.NewRequest(&chroniclev1.ListWorkflowsRequest{Namespace: "team-a", PageToken: "x"}))
			return err
		}, connect.CodeInvalidArgument},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := code(tt.call()); got != tt.want {
				t.Errorf("code = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestHTTPJSON checks the plain HTTP/JSON form of the API, as the UI and curl use it.
func TestHTTPJSON(t *testing.T) {
	srv := setup(t)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/chronicle.v1.WorkflowService/ListWorkflows", strings.NewReader(`{"namespace":"team-a"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "alice")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(string(body), "{") {
		t.Errorf("status %d, body %s", resp.StatusCode, body)
	}
}

func TestCursor(t *testing.T) {
	t0 := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	c, err := parseCursor(cursor{Time: t0, Name: "b"}.token())
	if err != nil {
		t.Fatal(err)
	}
	// Newest first, then by name: a newer run and "a" at the same time were on an earlier page.
	if !c.before(t0.Add(time.Second), "z") || !c.before(t0, "a") || !c.before(t0, "b") || c.before(t0, "c") || c.before(t0.Add(-time.Second), "a") {
		t.Error("wrong order")
	}
	if _, err := parseCursor("4"); code(err) != connect.CodeInvalidArgument {
		t.Errorf("a bad token: %v", err)
	}
}
