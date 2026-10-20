package server

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

func TestListAllNamespaces(t *testing.T) {
	srv := setup(t)
	ctx := context.Background()
	viewer := func(ns string) []client.Object {
		return []client.Object{
			&rbacv1.Role{Name: "view", Namespace: ns, Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{"chronicle.thinnesen.com"}, Resources: []string{"workflows"}, Verbs: []string{"get", "list"},
			}}},
			&rbacv1.RoleBinding{Name: "erin", Namespace: ns,
				RoleRef:  rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: "view"},
				Subjects: []rbacv1.Subject{{APIGroup: "rbac.authorization.k8s.io", Kind: "User", Name: "erin"}}},
		}
	}
	objs := append([]client.Object{&corev1.Namespace{Name: "team-c"}, &corev1.Namespace{Name: "team-d"}}, viewer("team-a")...)
	objs = append(objs, viewer("team-c")...)
	// The same name in three namespaces: erin may see two of them.
	for _, ns := range []string{"team-a", "team-c", "team-d"} {
		wf := &v1alpha1.Workflow{Name: "twin", Namespace: ns}
		wf.Spec.Entrypoint = "gate"
		wf.Spec.Templates = []v1alpha1.Template{{Name: "gate", Suspend: &v1alpha1.SuspendTemplate{}}}
		objs = append(objs, wf)
	}
	for _, o := range objs {
		if err := admin.Create(ctx, o); client.IgnoreAlreadyExists(err) != nil {
			t.Fatal(err)
		}
	}

	// Pages of one run go through all runs once, in the order of the list.
	seen := map[string]int{}
	token := ""
	for range 100 {
		resp, err := newClient(srv, "erin").ListWorkflows(ctx, connect.NewRequest(&chroniclev1.ListWorkflowsRequest{AllNamespaces: true, Limit: 1, PageToken: token}))
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range resp.Msg.Workflows {
			seen[w.Namespace+"/"+w.Name]++
		}
		if token = resp.Msg.NextPageToken; token == "" {
			break
		}
	}
	if seen["team-a/twin"] != 1 || seen["team-c/twin"] != 1 {
		t.Errorf("erin saw %v", seen)
	}
	for k := range seen {
		if k[:7] != "team-a/" && k[:7] != "team-c/" {
			t.Errorf("erin saw %s outside her namespaces", k)
		}
	}

	none, err := newClient(srv, "bob").ListWorkflows(ctx, connect.NewRequest(&chroniclev1.ListWorkflowsRequest{AllNamespaces: true}))
	if err != nil || len(none.Msg.Workflows) != 0 {
		t.Errorf("bob: %v %v", err, none)
	}
	_, err = newClient(srv, "erin").ListWorkflows(ctx, connect.NewRequest(&chroniclev1.ListWorkflowsRequest{AllNamespaces: true, Namespace: "team-a"}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("a namespace with all namespaces: %v", err)
	}
}
