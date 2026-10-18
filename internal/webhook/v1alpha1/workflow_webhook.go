package v1alpha1

import (
	"context"

	"k8s.io/apimachinery/pkg/api/equality"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

// SetupWorkflowWebhookWithManager registers the webhook for Workflow in the manager.
func SetupWorkflowWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &chroniclev1alpha1.Workflow{}).
		WithValidator(&WorkflowValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-chronicle-thinnesen-com-v1alpha1-workflow,mutating=false,failurePolicy=fail,sideEffects=None,groups=chronicle.thinnesen.com,resources=workflows,verbs=create;update,versions=v1alpha1,name=vworkflow-v1alpha1.kb.io,admissionReviewVersions=v1

// WorkflowValidator checks the templates, the parameters and the DAGs of a Workflow.
type WorkflowValidator struct{}

// ValidateCreate implements admission.Validator so a webhook will be registered for the type Workflow.
func (v *WorkflowValidator) ValidateCreate(_ context.Context, obj *chroniclev1alpha1.Workflow) (admission.Warnings, error) {
	return nil, validateWorkflow(obj)
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type Workflow.
func (v *WorkflowValidator) ValidateUpdate(_ context.Context, oldObj, obj *chroniclev1alpha1.Workflow) (admission.Warnings, error) {
	// Skip an unchanged spec, so that stricter rules never block a finalizer or label change.
	if equality.Semantic.DeepEqual(oldObj.Spec, obj.Spec) {
		return nil, nil
	}
	return nil, validateWorkflow(obj)
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type Workflow.
func (v *WorkflowValidator) ValidateDelete(_ context.Context, _ *chroniclev1alpha1.Workflow) (admission.Warnings, error) {
	return nil, nil
}

// validateWorkflow validates an inline spec. The controller validates a spec from a template reference when it runs it.
func validateWorkflow(obj *chroniclev1alpha1.Workflow) error {
	if obj.Spec.WorkflowTemplateRef != nil {
		return nil
	}
	return validateSpec("Workflow", obj.GetName(), &obj.Spec.WorkflowTemplateSpec, true)
}
