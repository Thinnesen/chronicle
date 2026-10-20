package v1alpha1

import (
	"context"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/schedule"
	"github.com/Thinnesen/chronicle/internal/validate"
)

// SetupCronWorkflowWebhookWithManager registers the webhook for CronWorkflow in the manager.
func SetupCronWorkflowWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &chroniclev1alpha1.CronWorkflow{}).
		WithValidator(&CronWorkflowValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-chronicle-thinnesen-com-v1alpha1-cronworkflow,mutating=false,failurePolicy=fail,sideEffects=None,groups=chronicle.thinnesen.com,resources=cronworkflows,verbs=create;update,versions=v1alpha1,name=vcronworkflow-v1alpha1.kb.io,admissionReviewVersions=v1

// CronWorkflowValidator checks the schedules and the workflow spec of a CronWorkflow.
type CronWorkflowValidator struct{}

// ValidateCreate implements admission.Validator so a webhook will be registered for the type CronWorkflow.
func (v *CronWorkflowValidator) ValidateCreate(_ context.Context, obj *chroniclev1alpha1.CronWorkflow) (admission.Warnings, error) {
	return validate.ShellWarnings(&obj.Spec.WorkflowSpec.WorkflowTemplateSpec, field.NewPath("spec", "workflowSpec")), validateCron(obj)
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type CronWorkflow.
func (v *CronWorkflowValidator) ValidateUpdate(_ context.Context, oldObj, obj *chroniclev1alpha1.CronWorkflow) (admission.Warnings, error) {
	if equality.Semantic.DeepEqual(oldObj.Spec, obj.Spec) {
		return nil, nil
	}
	return validate.ShellWarnings(&obj.Spec.WorkflowSpec.WorkflowTemplateSpec, field.NewPath("spec", "workflowSpec")), validateCron(obj)
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type CronWorkflow.
func (v *CronWorkflowValidator) ValidateDelete(_ context.Context, _ *chroniclev1alpha1.CronWorkflow) (admission.Warnings, error) {
	return nil, nil
}

func validateCron(obj *chroniclev1alpha1.CronWorkflow) error {
	gk := schema.GroupKind{Group: chroniclev1alpha1.GroupVersion.Group, Kind: "CronWorkflow"}
	if _, err := schedule.Parse(obj.Spec.Schedules, obj.Spec.Timezone); err != nil {
		return apierrors.NewInvalid(gk, obj.Name, field.ErrorList{field.Invalid(field.NewPath("spec", "schedules"), obj.Spec.Schedules, err.Error())})
	}
	if obj.Spec.WorkflowSpec.WorkflowTemplateRef != nil {
		return nil
	}
	return validateSpecAt("CronWorkflow", obj.Name, &obj.Spec.WorkflowSpec.WorkflowTemplateSpec, true, field.NewPath("spec", "workflowSpec"))
}
