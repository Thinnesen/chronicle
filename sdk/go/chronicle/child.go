package chronicle

import (
	"fmt"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// ChildWorkflow returns a template that starts a run of the WorkflowTemplate template in the same namespace and waits
// for it. Declare its outputs with OutputField: they come from the workflow outputs of the child run.
func ChildWorkflow(name, template string) *Template {
	child := &v1alpha1.ChildWorkflowTemplate{WorkflowTemplateRef: v1alpha1.WorkflowTemplateRef{Name: template}}
	return &Template{t: v1alpha1.Template{Name: name, ChildWorkflow: child}}
}

func (t *Template) child() *v1alpha1.ChildWorkflowTemplate {
	if t.t.ChildWorkflow == nil {
		t.errs = append(t.errs, fmt.Errorf("template %q is not a child workflow template", t.t.Name))
		return &v1alpha1.ChildWorkflowTemplate{}
	}
	return t.t.ChildWorkflow
}

// ClusterScope makes a child workflow template start a ClusterWorkflowTemplate.
func (t *Template) ClusterScope() *Template {
	t.child().WorkflowTemplateRef.ClusterScope = true
	return t
}

// ChildArg gives a value to a parameter of the child run. The value can have placeholders.
func (t *Template) ChildArg(name, value string) *Template {
	c := t.child()
	c.Arguments.Parameters = append(c.Arguments.Parameters, v1alpha1.Parameter{Name: name, Value: &value})
	return t
}
