package v1alpha1

import (
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/validate"
)

// validateSpec returns an Invalid API error when the spec has errors.
func validateSpec(kind, name string, spec *chroniclev1alpha1.WorkflowTemplateSpec, forWorkflow bool) error {
	return validateSpecAt(kind, name, spec, forWorkflow, field.NewPath("spec"))
}

func validateSpecAt(kind, name string, spec *chroniclev1alpha1.WorkflowTemplateSpec, forWorkflow bool, path *field.Path) error {
	errs := validate.SpecAt(spec, forWorkflow, path)
	if len(errs) == 0 {
		return nil
	}
	gk := schema.GroupKind{Group: chroniclev1alpha1.GroupVersion.Group, Kind: kind}
	return apierrors.NewInvalid(gk, name, errs)
}
