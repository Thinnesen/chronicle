package v1alpha1

import (
	"context"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/validation/field"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/validate"
)

// SetupWorkflowTemplateWebhookWithManager registers the webhook for WorkflowTemplate in the manager.
func SetupWorkflowTemplateWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &chroniclev1alpha1.WorkflowTemplate{}).
		WithValidator(&WorkflowTemplateValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-chronicle-thinnesen-com-v1alpha1-workflowtemplate,mutating=false,failurePolicy=fail,sideEffects=None,groups=chronicle.thinnesen.com,resources=workflowtemplates,verbs=create;update,versions=v1alpha1,name=vworkflowtemplate-v1alpha1.kb.io,admissionReviewVersions=v1

// WorkflowTemplateValidator checks the templates, the parameters and the DAGs of a WorkflowTemplate.
type WorkflowTemplateValidator struct{}

// ValidateCreate implements admission.Validator so a webhook will be registered for the type WorkflowTemplate.
func (v *WorkflowTemplateValidator) ValidateCreate(_ context.Context, obj *chroniclev1alpha1.WorkflowTemplate) (admission.Warnings, error) {
	return validate.ShellWarnings(&obj.Spec, field.NewPath("spec")), validateSpec("WorkflowTemplate", obj.GetName(), &obj.Spec, false)
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type WorkflowTemplate.
func (v *WorkflowTemplateValidator) ValidateUpdate(_ context.Context, oldObj, obj *chroniclev1alpha1.WorkflowTemplate) (admission.Warnings, error) {
	// Skip an unchanged spec, so that stricter rules never block a finalizer or label change.
	if equality.Semantic.DeepEqual(oldObj.Spec, obj.Spec) {
		return nil, nil
	}
	return validate.ShellWarnings(&obj.Spec, field.NewPath("spec")), validateSpec("WorkflowTemplate", obj.GetName(), &obj.Spec, false)
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type WorkflowTemplate.
func (v *WorkflowTemplateValidator) ValidateDelete(_ context.Context, _ *chroniclev1alpha1.WorkflowTemplate) (admission.Warnings, error) {
	return nil, nil
}
