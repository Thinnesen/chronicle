package server

import (
	"strings"
	"testing"

	"connectrpc.com/connect"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// TestToConnectInvalid removes the "<nil>" that Kubernetes writes for a rule on the whole object.
func TestToConnectInvalid(t *testing.T) {
	err := apierrors.NewInvalid(schema.GroupKind{Group: "chronicle.thinnesen.com", Kind: "Workflow"}, "x",
		field.ErrorList{field.Invalid(nil, "", "a shutdown cannot be removed")})
	got := toConnect(err)
	if connect.CodeOf(got) != connect.CodeInvalidArgument || strings.Contains(got.Error(), "<nil>") || !strings.Contains(got.Error(), "a shutdown cannot be removed") {
		t.Errorf("toConnect = %v (%v)", got, connect.CodeOf(got))
	}
}
