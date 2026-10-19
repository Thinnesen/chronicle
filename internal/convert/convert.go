// Package convert turns Argo Workflows YAML (Workflow, WorkflowTemplate, ClusterWorkflowTemplate and
// CronWorkflow) into chronicle resources. It keeps the fields that chronicle supports, drops the others
// with a warning, and validates the result with the rules of the admission webhook.
package convert

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	"k8s.io/apimachinery/pkg/util/validation/field"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/schedule"
	"github.com/Thinnesen/chronicle/internal/validate"
)

const argoAPIVersion = "argoproj.io/v1alpha1"

var kinds = []string{"Workflow", "WorkflowTemplate", "ClusterWorkflowTemplate", "CronWorkflow"}

// Result is one converted document.
type Result struct {
	Kind, Name string
	// YAML is the chronicle resource.
	YAML     []byte
	Warnings []Warning
}

// Convert converts each document of an Argo YAML file. It stops at the first document that it cannot convert.
func Convert(data []byte) ([]Result, error) {
	dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var out []Result
	for i := 0; ; i++ {
		var doc obj
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("document %d: %w", i+1, err)
		}
		if doc == nil {
			continue
		}
		r, err := convertDocument(doc)
		if err != nil {
			return nil, fmt.Errorf("document %d (%s %s): %w", i+1, doc["kind"], name(doc), err)
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, errors.New("the file has no documents")
	}
	return out, nil
}

func name(doc obj) string {
	m := asObj(doc["metadata"])
	if n, ok := m["name"].(string); ok {
		return n
	}
	n, _ := m["generateName"].(string)
	return n
}

func convertDocument(doc obj) (Result, error) {
	kind, _ := doc["kind"].(string)
	if doc["apiVersion"] != argoAPIVersion || !slices.Contains(kinds, kind) {
		return Result{}, fmt.Errorf("not an Argo Workflow, WorkflowTemplate, ClusterWorkflowTemplate or CronWorkflow")
	}
	c := &converter{}
	out := obj{
		"apiVersion": v1alpha1.GroupVersion.String(),
		"kind":       kind,
		"metadata":   c.metadata(asObj(doc["metadata"])),
	}
	var err error
	spec := asObj(doc["spec"])
	if kind == "CronWorkflow" {
		out["spec"], err = c.cronSpec(spec, "spec")
	} else {
		out["spec"], err = c.spec(spec, "spec", kind == "Workflow")
	}
	if err != nil {
		return Result{}, err
	}
	if err := check(kind, out); err != nil {
		return Result{}, err
	}
	data, err := yaml.Marshal(out)
	if err != nil {
		return Result{}, fmt.Errorf("encode the result: %w", err)
	}
	return Result{Kind: kind, Name: name(doc), YAML: data, Warnings: c.warnings}, nil
}

// metadata keeps the fields that a new object can have. Server-set fields of an exported object go away.
func (c *converter) metadata(in obj) obj {
	out := obj{}
	for k, v := range in {
		switch k {
		case "name", "generateName", "namespace", "labels", "annotations":
			out[k] = v
		case "uid", "resourceVersion", "creationTimestamp", "generation", "managedFields", "selfLink":
		default:
			c.warn(join("metadata", k), "the field is dropped")
		}
	}
	return out
}

// check decodes the result into the chronicle type, so that an unknown field is an error,
// and validates it like the admission webhook.
func check(kind string, out obj) error {
	data, err := json.Marshal(out)
	if err != nil {
		return err
	}
	strict := func(v any) error {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(v); err != nil {
			return fmt.Errorf("the result is not a valid %s: %w", kind, err)
		}
		return nil
	}
	var errs field.ErrorList
	switch kind {
	case "Workflow":
		var wf v1alpha1.Workflow
		if err := strict(&wf); err != nil {
			return err
		}
		if wf.Spec.WorkflowTemplateRef == nil {
			errs = validate.Spec(&wf.Spec.WorkflowTemplateSpec, true)
		}
	case "WorkflowTemplate", "ClusterWorkflowTemplate":
		var t v1alpha1.WorkflowTemplate
		if err := strict(&t); err != nil {
			return err
		}
		errs = validate.Spec(&t.Spec, false)
	case "CronWorkflow":
		var cw v1alpha1.CronWorkflow
		if err := strict(&cw); err != nil {
			return err
		}
		if _, err := schedule.Parse(cw.Spec.Schedules, cw.Spec.Timezone); err != nil {
			return fmt.Errorf("spec.schedules: %w", err)
		}
		if cw.Spec.WorkflowSpec.WorkflowTemplateRef == nil {
			errs = validate.SpecAt(&cw.Spec.WorkflowSpec.WorkflowTemplateSpec, true, field.NewPath("spec", "workflowSpec"))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("the result is not valid in chronicle: %w", errs.ToAggregate())
	}
	return nil
}
