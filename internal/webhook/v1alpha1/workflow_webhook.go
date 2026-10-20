package v1alpha1

import (
	"context"
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/validate"
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
func (v *WorkflowValidator) ValidateCreate(ctx context.Context, obj *chroniclev1alpha1.Workflow) (admission.Warnings, error) {
	if err := validateApprovers(ctx, nil, obj); err != nil {
		return nil, err
	}
	return validate.ShellWarnings(&obj.Spec.WorkflowTemplateSpec, field.NewPath("spec")), validateWorkflow(obj)
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type Workflow.
func (v *WorkflowValidator) ValidateUpdate(ctx context.Context, oldObj, obj *chroniclev1alpha1.Workflow) (admission.Warnings, error) {
	// Skip an unchanged spec, so that stricter rules never block a finalizer or label change.
	if equality.Semantic.DeepEqual(oldObj.Spec, obj.Spec) {
		return nil, nil
	}
	if err := validateApprovers(ctx, oldObj, obj); err != nil {
		return nil, err
	}
	return validate.ShellWarnings(&obj.Spec.WorkflowTemplateSpec, field.NewPath("spec")), validateWorkflow(obj)
}

// validateApprovers refuses an approval with a user that is not the user of the request. Kubernetes does the same for
// the username of a CertificateSigningRequest. An approval that the old object already has, with the same user, stays.
func validateApprovers(ctx context.Context, oldObj, obj *chroniclev1alpha1.Workflow) error {
	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		return fmt.Errorf("read the admission request: %w", err)
	}
	var errs field.ErrorList
	path := field.NewPath("spec", "approvals")
	for i, a := range obj.Spec.Approvals {
		if a.User == "" || a.User == req.UserInfo.Username {
			continue
		}
		if oldObj != nil && slices.ContainsFunc(oldObj.Spec.Approvals, func(o chroniclev1alpha1.Approval) bool {
			return o.Node == a.Node && o.User == a.User
		}) {
			continue
		}
		errs = append(errs, field.Forbidden(path.Index(i).Child("user"), "the user of an approval must be the user of the request"))
	}
	if len(errs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(schema.GroupKind{Group: chroniclev1alpha1.GroupVersion.Group, Kind: "Workflow"}, obj.Name, errs)
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
