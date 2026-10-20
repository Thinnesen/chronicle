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

// SetupClusterWorkflowTemplateWebhookWithManager registers the webhook for ClusterWorkflowTemplate in the manager.
func SetupClusterWorkflowTemplateWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &chroniclev1alpha1.ClusterWorkflowTemplate{}).
		WithValidator(&ClusterWorkflowTemplateValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-chronicle-thinnesen-com-v1alpha1-clusterworkflowtemplate,mutating=false,failurePolicy=fail,sideEffects=None,groups=chronicle.thinnesen.com,resources=clusterworkflowtemplates,verbs=create;update,versions=v1alpha1,name=vclusterworkflowtemplate-v1alpha1.kb.io,admissionReviewVersions=v1

// ClusterWorkflowTemplateValidator checks the templates, the parameters and the DAGs of a ClusterWorkflowTemplate.
type ClusterWorkflowTemplateValidator struct{}

// ValidateCreate implements admission.Validator so a webhook will be registered for the type ClusterWorkflowTemplate.
func (v *ClusterWorkflowTemplateValidator) ValidateCreate(_ context.Context, obj *chroniclev1alpha1.ClusterWorkflowTemplate) (admission.Warnings, error) {
	return validate.ShellWarnings(&obj.Spec, field.NewPath("spec")), validateSpec("ClusterWorkflowTemplate", obj.GetName(), &obj.Spec, false)
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type ClusterWorkflowTemplate.
func (v *ClusterWorkflowTemplateValidator) ValidateUpdate(_ context.Context, oldObj, obj *chroniclev1alpha1.ClusterWorkflowTemplate) (admission.Warnings, error) {
	// Skip an unchanged spec, so that stricter rules never block a finalizer or label change.
	if equality.Semantic.DeepEqual(oldObj.Spec, obj.Spec) {
		return nil, nil
	}
	return validate.ShellWarnings(&obj.Spec, field.NewPath("spec")), validateSpec("ClusterWorkflowTemplate", obj.GetName(), &obj.Spec, false)
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type ClusterWorkflowTemplate.
func (v *ClusterWorkflowTemplateValidator) ValidateDelete(_ context.Context, _ *chroniclev1alpha1.ClusterWorkflowTemplate) (admission.Warnings, error) {
	return nil, nil
}
