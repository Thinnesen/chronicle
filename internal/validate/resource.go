package validate

import (
	"bytes"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/schedule"
)

// Kinds are the kinds of the chronicle resources that Resource checks.
var Kinds = []string{"Workflow", "WorkflowTemplate", "ClusterWorkflowTemplate", "CronWorkflow"}

// Resource checks one chronicle resource in JSON offline. It decodes it strictly, so an unknown field is an error. It
// applies the rules of the admission webhook. It does not check the references to templates.
func Resource(data []byte) error {
	var head struct{ Kind string }
	if err := json.Unmarshal(data, &head); err != nil {
		return fmt.Errorf("not a resource: %w", err)
	}
	strict := func(v any) error {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(v); err != nil {
			return fmt.Errorf("not a valid %s: %w", head.Kind, err)
		}
		return nil
	}
	var errs field.ErrorList
	switch head.Kind {
	case "Workflow":
		var wf v1alpha1.Workflow
		if err := strict(&wf); err != nil {
			return err
		}
		if wf.Spec.WorkflowTemplateRef == nil {
			errs = Spec(&wf.Spec.WorkflowTemplateSpec, true)
		}
	case "WorkflowTemplate", "ClusterWorkflowTemplate":
		var t v1alpha1.WorkflowTemplate
		if err := strict(&t); err != nil {
			return err
		}
		errs = Spec(&t.Spec, false)
	case "CronWorkflow":
		var cw v1alpha1.CronWorkflow
		if err := strict(&cw); err != nil {
			return err
		}
		if _, err := schedule.Parse(cw.Spec.Schedules, cw.Spec.Timezone); err != nil {
			return fmt.Errorf("spec.schedules: %w", err)
		}
		if cw.Spec.WorkflowSpec.WorkflowTemplateRef == nil {
			errs = SpecAt(&cw.Spec.WorkflowSpec.WorkflowTemplateSpec, true, field.NewPath("spec", "workflowSpec"))
		}
	default:
		return fmt.Errorf("kind %q is not a chronicle resource", head.Kind)
	}
	if len(errs) > 0 {
		return fmt.Errorf("not valid in chronicle: %w", errs.ToAggregate())
	}
	return nil
}
