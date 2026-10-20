package auth

import (
	"context"
	"net/http"
	"strings"
	"testing"

	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// reviewer returns an authenticator whose API server answers each TokenReview with status, and counts the reviews.
func reviewer(status authenticationv1.TokenReviewStatus, calls *int) *TokenReviewAuthenticator {
	scheme := runtime.NewScheme()
	_ = authenticationv1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.CreateOption) error {
			*calls++
			obj.(*authenticationv1.TokenReview).Status = status
			return nil
		},
	}).Build()
	return &TokenReviewAuthenticator{Client: c, Audiences: []string{"chronicle"}}
}

func bearer(token string) *http.Request {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func TestTokenReview(t *testing.T) {
	sa := authenticationv1.UserInfo{Username: "system:serviceaccount:ci:deploy", Groups: []string{"system:serviceaccounts"}}
	tests := []struct {
		name   string
		status authenticationv1.TokenReviewStatus
		err    string
	}{
		{name: "a service account token for chronicle", status: authenticationv1.TokenReviewStatus{Authenticated: true, User: sa, Audiences: []string{"chronicle"}}},
		{name: "a server that ignores the audience", status: authenticationv1.TokenReviewStatus{Authenticated: true, User: sa}, err: "audience"},
		{name: "a token for another audience", status: authenticationv1.TokenReviewStatus{Authenticated: true, User: sa, Audiences: []string{"vault"}}, err: "audience"},
		{name: "a token that is not valid", status: authenticationv1.TokenReviewStatus{}, err: "not valid"},
		{name: "a user token", status: authenticationv1.TokenReviewStatus{Authenticated: true, User: authenticationv1.UserInfo{Username: "alice"}, Audiences: []string{"chronicle"}},
			err: "only service account"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			a := reviewer(tt.status, &calls)
			for range 2 {
				id, err := a.Authenticate(bearer("token"))
				if tt.err != "" {
					if err == nil || !strings.Contains(err.Error(), tt.err) {
						t.Fatalf("err = %v, want %q", err, tt.err)
					}
					continue
				}
				if err != nil || id.User != sa.Username || !id.ServiceAccount {
					t.Fatalf("id = %+v, err = %v", id, err)
				}
			}
			// Only an accepted token is cached; a refused token is checked again each time.
			want := 2
			if tt.err == "" {
				want = 1
			}
			if calls != want {
				t.Errorf("%d reviews, want %d", calls, want)
			}
		})
	}
}

func TestImpersonable(t *testing.T) {
	tests := []struct {
		id   Identity
		want bool
	}{
		{Identity{User: "oidc:alice", Groups: []string{"oidc:team-a"}}, true},
		{Identity{User: "system:admin"}, false},
		{Identity{User: "oidc:alice", Groups: []string{"system:masters"}}, false},
		{Identity{User: "system:serviceaccount:ci:deploy", Groups: []string{"system:serviceaccounts", "system:serviceaccounts:ci", "system:authenticated"}, ServiceAccount: true}, true},
		{Identity{User: "system:serviceaccount:ci:deploy", Groups: []string{"system:serviceaccounts:kube-system"}, ServiceAccount: true}, false},
		{Identity{User: "system:serviceaccount:ci:deploy", Groups: []string{"system:masters"}, ServiceAccount: true}, false},
		{Identity{User: "system:serviceaccount:ci", ServiceAccount: true}, false},
		{Identity{User: "system:node:worker-1", ServiceAccount: true}, false},
	}
	for _, tt := range tests {
		if got := tt.id.Impersonable(); got != tt.want {
			t.Errorf("Impersonable(%+v) = %v, want %v", tt.id, got, tt.want)
		}
	}
}
