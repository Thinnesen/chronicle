// Package validate checks a workflow spec for errors that the CRD schema cannot find:
// unknown templates, unknown parameters, bad placeholders and DAG cycles.
package validate

import (
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/expr"
)

// Spec validates a workflow definition. When forWorkflow is true, each workflow parameter must have a value.
func Spec(spec *v1alpha1.WorkflowTemplateSpec, forWorkflow bool) field.ErrorList {
	path := field.NewPath("spec")
	c := &checker{templates: map[string]*v1alpha1.Template{}, globals: scope{}}
	for i := range spec.Templates {
		c.templates[spec.Templates[i].Name] = &spec.Templates[i]
	}
	for _, g := range []string{"workflow.name", "workflow.namespace", "workflow.uid"} {
		c.globals[g] = true
	}
	for i, p := range spec.Arguments.Parameters {
		c.globals["workflow.parameters."+p.Name] = true
		pp := path.Child("arguments", "parameters").Index(i)
		if forWorkflow && p.Value == nil && p.Default == nil {
			c.errs = append(c.errs, field.Required(pp.Child("value"), "a workflow parameter needs a value or a default"))
		}
		c.checkEnum(&p, pp)
	}
	if _, ok := c.templates[spec.Entrypoint]; !ok {
		c.errs = append(c.errs, field.NotFound(path.Child("entrypoint"), spec.Entrypoint))
	}
	for i := range spec.Templates {
		c.checkTemplate(&spec.Templates[i], path.Child("templates").Index(i))
	}
	return c.errs
}

// scope is the set of placeholder names that a text can use.
type scope map[string]bool

func (s scope) with(names ...string) scope {
	out := make(scope, len(s)+len(names))
	for k := range s {
		out[k] = true
	}
	for _, n := range names {
		out[n] = true
	}
	return out
}

type checker struct {
	templates map[string]*v1alpha1.Template
	globals   scope
	errs      field.ErrorList
}

func (c *checker) checkTemplate(t *v1alpha1.Template, path *field.Path) {
	sc := c.globals
	for i, p := range t.Inputs.Parameters {
		sc = sc.with("inputs.parameters." + p.Name)
		c.checkEnum(&p, path.Child("inputs", "parameters").Index(i))
	}
	c.checkOutputs(t, path.Child("outputs", "parameters"), sc)
	switch {
	case t.Container != nil:
		c.checkContainer(t.Container, path.Child("container"), sc)
	case t.Script != nil:
		c.checkContainer(&t.Script.ContainerTemplate, path.Child("script"), sc)
		if strings.Contains(t.Script.Source, "{{") {
			c.errs = append(c.errs, field.Invalid(path.Child("script", "source"), "{{...}}",
				"a script source cannot have placeholders: use environment variables"))
		}
	case t.DAG != nil:
		c.checkDAG(t.DAG, path.Child("dag"), sc)
	case t.Steps != nil:
		c.checkSteps(t.Steps, path.Child("steps"), sc)
	}
}

func (c *checker) checkOutputs(t *v1alpha1.Template, path *field.Path, sc scope) {
	leaf := t.Container != nil || t.Script != nil
	for i, p := range t.Outputs.Parameters {
		pp := path.Index(i)
		hasPath := p.ValueFrom != nil && p.ValueFrom.Path != ""
		switch {
		case hasPath && !leaf:
			c.errs = append(c.errs, field.Forbidden(pp.Child("valueFrom", "path"), "only a container or script template can read an output file"))
		case !hasPath && p.Value == nil:
			c.errs = append(c.errs, field.Required(pp, "an output parameter needs valueFrom.path or a value"))
		case p.Value != nil:
			c.checkText(*p.Value, pp.Child("value"), sc, nil)
		}
	}
}

func (c *checker) checkContainer(ct *v1alpha1.ContainerTemplate, path *field.Path, sc scope) {
	c.checkText(ct.Image, path.Child("image"), sc, nil)
	c.checkText(ct.WorkingDir, path.Child("workingDir"), sc, nil)
	for i, s := range ct.Command {
		c.checkText(s, path.Child("command").Index(i), sc, nil)
	}
	for i, s := range ct.Args {
		c.checkText(s, path.Child("args").Index(i), sc, nil)
	}
	for i, e := range ct.Env {
		c.checkText(e.Value, path.Child("env").Index(i).Child("value"), sc, nil)
	}
}

// checkCall checks a call of a template from a DAG task or a step.
// hint explains why a placeholder is not in the scope, or returns "".
func (c *checker) checkCall(name string, args v1alpha1.Arguments, path *field.Path, sc scope, hint func(string) string) {
	t, ok := c.templates[name]
	if !ok {
		c.errs = append(c.errs, field.NotFound(path.Child("template"), name))
		return
	}
	argsPath := path.Child("arguments", "parameters")
	for i, a := range args.Parameters {
		ap := argsPath.Index(i)
		in := findParam(t.Inputs.Parameters, a.Name)
		switch {
		case in == nil:
			c.errs = append(c.errs, field.NotFound(ap.Child("name"), a.Name))
		case in.Value != nil:
			c.errs = append(c.errs, field.Forbidden(ap.Child("name"), fmt.Sprintf("parameter %q of template %q has a fixed value", a.Name, name)))
		case a.Value == nil:
			c.errs = append(c.errs, field.Required(ap.Child("value"), "an argument needs a value"))
		default:
			c.checkText(*a.Value, ap.Child("value"), sc, hint)
			if len(in.Enum) > 0 && !strings.Contains(*a.Value, "{{") && !slices.Contains(in.Enum, *a.Value) {
				c.errs = append(c.errs, field.NotSupported(ap.Child("value"), *a.Value, in.Enum))
			}
		}
	}
	for _, in := range t.Inputs.Parameters {
		if in.Value == nil && in.Default == nil && findParam(args.Parameters, in.Name) == nil {
			c.errs = append(c.errs, field.Required(argsPath, fmt.Sprintf("parameter %q of template %q", in.Name, name)))
		}
	}
}

func (c *checker) checkEnum(p *v1alpha1.Parameter, path *field.Path) {
	if len(p.Enum) == 0 {
		return
	}
	if p.Value != nil && !slices.Contains(p.Enum, *p.Value) {
		c.errs = append(c.errs, field.NotSupported(path.Child("value"), *p.Value, p.Enum))
	}
	if p.Default != nil && !slices.Contains(p.Enum, *p.Default) {
		c.errs = append(c.errs, field.NotSupported(path.Child("default"), *p.Default, p.Enum))
	}
}

// checkText checks that each placeholder in s is in the scope.
func (c *checker) checkText(s string, path *field.Path, sc scope, hint func(string) string) {
	names, err := expr.Placeholders(s)
	if err != nil {
		c.errs = append(c.errs, field.Invalid(path, s, err.Error()))
		return
	}
	for _, n := range names {
		if sc[n] {
			continue
		}
		msg := fmt.Sprintf("unknown placeholder %q", n)
		if hint != nil {
			if h := hint(n); h != "" {
				msg = h
			}
		}
		c.errs = append(c.errs, field.Invalid(path, s, msg))
	}
}

func findParam(params []v1alpha1.Parameter, name string) *v1alpha1.Parameter {
	for i := range params {
		if params[i].Name == name {
			return &params[i]
		}
	}
	return nil
}
