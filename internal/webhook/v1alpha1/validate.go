package v1alpha1

import (
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/validate"
)

// validateSpec returns an Invalid API error when the spec has errors.
func validateSpec(kind, name string, spec *chroniclev1alpha1.WorkflowTemplateSpec, forWorkflow bool) error {
	errs := validate.Spec(spec, forWorkflow)
	if len(errs) == 0 {
		return nil
	}
	gk := schema.GroupKind{Group: chroniclev1alpha1.GroupVersion.Group, Kind: kind}
	return apierrors.NewInvalid(gk, name, errs)
}
