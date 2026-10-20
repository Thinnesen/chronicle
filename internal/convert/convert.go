// Package convert turns Argo Workflows YAML into chronicle resources: Workflow, WorkflowTemplate,
// ClusterWorkflowTemplate and CronWorkflow. It keeps the fields that chronicle supports. It drops the others with a
// warning. It validates the result with the rules of the admission webhook.
package convert

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/validate"
)

const argoAPIVersion = "argoproj.io/v1alpha1"

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
	if !slices.ContainsFunc(out, func(r Result) bool { return r.YAML != nil }) {
		return nil, errors.New("the file has nothing to convert")
	}
	return out, nil
}

func name(doc obj) string {
	m := asObj(doc["metadata"])
	if n, ok := m[fName].(string); ok {
		return n
	}
	n, _ := m["generateName"].(string)
	return n
}

func convertDocument(doc obj) (Result, error) {
	kind, _ := doc["kind"].(string)
	if doc["apiVersion"] != argoAPIVersion {
		// For example a ConfigMap that a workflow reads.
		data, err := yaml.Marshal(doc)
		if err != nil {
			return Result{}, fmt.Errorf("encode the document: %w", err)
		}
		return Result{Kind: kind, Name: name(doc), YAML: data, Warnings: []Warning{{Message: "not an Argo resource: copied as it is"}}}, nil
	}
	if !slices.Contains(validate.Kinds, kind) {
		return Result{Kind: kind, Name: name(doc), Warnings: []Warning{{Message: "chronicle has no " + kind + ": the document is dropped"}}}, nil
	}
	c := &converter{}
	out := obj{
		"apiVersion": v1alpha1.GroupVersion.String(),
		"kind":       kind,
		"metadata":   c.metadata(asObj(doc["metadata"])),
	}
	var err error
	spec := asObj(doc["spec"])
	if kind == kindCron {
		out["spec"], err = c.cronSpec(spec, "spec")
	} else {
		out["spec"], err = c.spec(spec, "spec", kind == kindWorkflow)
	}
	if err != nil {
		return Result{}, err
	}
	if err := check(out); err != nil {
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
		case fName, "generateName", "namespace", "labels", "annotations":
			out[k] = v
		case "uid", "resourceVersion", "creationTimestamp", "generation", "managedFields", "selfLink":
		default:
			c.warn(join("metadata", k), "the field is dropped")
		}
	}
	return out
}

// check validates the result like the admission webhook.
func check(out obj) error {
	data, err := json.Marshal(out)
	if err != nil {
		return err
	}
	if err := validate.Resource(data); err != nil {
		return fmt.Errorf("the result is %w", err)
	}
	return nil
}
