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

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"
	authenticationv1 "k8s.io/api/authentication/v1"
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

func TestPaginate(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	page, next, _ := paginate(items, 2, "")
	if len(page) != 2 || next != "2" {
		t.Fatalf("first page %v %q", page, next)
	}
	page, next, _ = paginate(items, 2, "4")
	if len(page) != 1 || next != "" {
		t.Fatalf("last page %v %q", page, next)
	}
	if page, _, _ := paginate(items, 0, "9"); page != nil {
		t.Fatalf("past the end %v", page)
	}
}

func TestListNamespaces(t *testing.T) {
	srv := setup(t)
	ctx := context.Background()
	list := func(user, group string) []*chroniclev1.NamespaceAccess {
		c := chroniclev1connect.NewNamespaceServiceClient(srv.Client(), srv.URL, connect.WithInterceptors(connect.UnaryInterceptorFunc(
			func(next connect.UnaryFunc) connect.UnaryFunc {
				return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
					req.Header().Set("X-Test-User", user)
					req.Header().Set("X-Test-Group", group)
					return next(ctx, req)
				}
			})))
		resp, err := c.ListNamespaces(ctx, connect.NewRequest(&chroniclev1.ListNamespacesRequest{}))
		if err != nil {
			t.Fatal(err)
		}
		return resp.Msg.Namespaces
	}
	if got := list("alice", ""); len(got) != 1 || got[0].Name != "team-a" || !got[0].CanSubmit {
		t.Errorf("alice sees %v", got)
	}
	if got := list("carol", "oidc:viewers"); len(got) != 1 || got[0].CanSubmit {
		t.Errorf("a viewer sees %v", got)
	}
	if got := list("bob", ""); len(got) != 0 {
		t.Errorf("bob sees %v", got)
	}
}

func TestServiceAccountToken(t *testing.T) {
	setup(t)
	ctx := context.Background()
	sa := &corev1.ServiceAccount{Name: "robot", Namespace: "team-a"}
	if err := admin.Create(ctx, sa); client.IgnoreAlreadyExists(err) != nil {
		t.Fatal(err)
	}
	rb := &rbacv1.RoleBinding{Name: "robot", Namespace: "team-a",
		RoleRef:  rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: "view"},
		Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: "robot", Namespace: "team-a"}}}
	if err := admin.Create(ctx, rb); client.IgnoreAlreadyExists(err) != nil {
		t.Fatal(err)
	}
	token := func(audience string) string {
		tr := &authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{Audiences: []string{audience}}}
		if err := admin.SubResource("token").Create(ctx, sa, tr); err != nil {
			t.Fatal(err)
		}
		return tr.Status.Token
	}
	h, err := NewHandler(Options{Config: cfg, Scheme: scheme, Authenticators: []auth.Authenticator{
		&auth.TokenReviewAuthenticator{Client: admin, Audiences: []string{"chronicle"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	list := func(tok string) error {
		c := chroniclev1connect.NewWorkflowServiceClient(srv.Client(), srv.URL)
		req := connect.NewRequest(&chroniclev1.ListWorkflowsRequest{Namespace: "team-a"})
		req.Header().Set("Authorization", "Bearer "+tok)
		_, err := c.ListWorkflows(ctx, req)
		return err
	}
	if err := list(token("chronicle")); err != nil {
		t.Errorf("a token for chronicle: %v", err)
	}
	if err := list(token("other")); code(err) != connect.CodeUnauthenticated {
		t.Errorf("a token for another audience: %v", err)
	}
}

func TestImpersonable(t *testing.T) {
	tests := []struct {
		id   auth.Identity
		want bool
	}{
		{auth.Identity{User: "oidc:a", Groups: []string{"oidc:g"}}, true},
		{auth.Identity{User: "system:admin"}, false},
		{auth.Identity{User: "oidc:a", Groups: []string{"system:masters"}}, false},
		{auth.Identity{User: "system:serviceaccount:ns:sa", Groups: []string{"system:serviceaccounts", "system:serviceaccounts:ns", "system:authenticated"}, ServiceAccount: true}, true},
		{auth.Identity{User: "system:serviceaccount:ns:sa", Groups: []string{"system:masters"}, ServiceAccount: true}, false},
		{auth.Identity{User: "system:serviceaccount:ns:sa", Groups: []string{"system:serviceaccounts:other"}, ServiceAccount: true}, false},
		{auth.Identity{User: "system:node:x", ServiceAccount: true}, false},
		{auth.Identity{User: "system:serviceaccount:ns:sa"}, false},
	}
	for _, tt := range tests {
		if got := tt.id.Impersonable(); got != tt.want {
			t.Errorf("%+v: Impersonable() = %v", tt.id, got)
		}
	}
}
