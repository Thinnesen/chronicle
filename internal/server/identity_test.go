package server

import (
	"context"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/gen/chronicle/v1/chroniclev1connect"
	"github.com/Thinnesen/chronicle/internal/auth"
)

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
