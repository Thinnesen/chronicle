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
	"github.com/Thinnesen/chronicle/internal/manifest"
)

// Spec validates a workflow definition at the path "spec". When forWorkflow is true, each workflow parameter must have a value.
func Spec(spec *v1alpha1.WorkflowTemplateSpec, forWorkflow bool) field.ErrorList {
	return SpecAt(spec, forWorkflow, field.NewPath("spec"))
}

// SpecAt validates a workflow definition at a path, for example "spec.workflowSpec" in a CronWorkflow.
func SpecAt(spec *v1alpha1.WorkflowTemplateSpec, forWorkflow bool, path *field.Path) field.ErrorList {
	c := &checker{templates: map[string]*v1alpha1.Template{}, globals: scope{}}
	for i := range spec.Templates {
		c.templates[spec.Templates[i].Name] = &spec.Templates[i]
	}
	for _, g := range []string{"workflow.name", "workflow.namespace", "workflow.uid", "workflow.status",
		"workflow.creationTimestamp", "workflow.scheduledTime"} {
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
	if t, ok := c.templates[spec.Entrypoint]; !ok {
		c.errs = append(c.errs, field.NotFound(path.Child("entrypoint"), spec.Entrypoint))
	} else {
		for _, in := range t.Inputs.Parameters {
			if in.Value == nil && in.Default == nil && findParam(spec.Arguments.Parameters, in.Name) == nil {
				c.errs = append(c.errs, field.Required(path.Child("arguments", "parameters"),
					fmt.Sprintf("parameter %q of the entrypoint template", in.Name)))
			}
		}
		c.checkNoArtifactInputs(t, path.Child("entrypoint"))
	}
	if t, ok := c.templates[spec.OnExit]; spec.OnExit != "" && !ok {
		c.errs = append(c.errs, field.NotFound(path.Child("onExit"), spec.OnExit))
	} else if ok {
		c.checkNoArtifactInputs(t, path.Child("onExit"))
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

// has tells if a name is in the scope, also through a "prefix.*" name.
func (s scope) has(name string) bool {
	if s[name] {
		return true
	}
	for i := strings.LastIndexByte(name, '.'); i > 0; i = strings.LastIndexByte(name[:i], '.') {
		if s[name[:i]+".*"] {
			return true
		}
	}
	return false
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
		pp := path.Child("inputs", "parameters").Index(i)
		c.checkEnum(&p, pp)
		if p.Value != nil {
			c.checkText(*p.Value, pp.Child("value"), c.globals, nil)
		}
		if p.Default != nil {
			c.checkText(*p.Default, pp.Child("default"), c.globals, nil)
		}
	}
	for _, a := range t.Inputs.Artifacts {
		sc = sc.with("inputs.artifacts." + a.Name)
	}
	c.checkArtifacts(t, path)
	c.checkOutputs(t, path.Child("outputs", "parameters"), sc.with(c.childOutputs(t)...))
	switch {
	case t.Container != nil:
		c.checkContainer(t.Container, path.Child("container"), sc)
	case t.Script != nil:
		c.checkContainer(&t.Script.ContainerTemplate, path.Child("script"), sc)
		if strings.Contains(t.Script.Source, "{{") {
			c.errs = append(c.errs, field.Invalid(path.Child("script", "source"), "{{...}}",
				"a script source cannot have placeholders: use environment variables"))
		}
	case t.Resource != nil:
		c.checkManifest(t.Resource, path.Child("resource"), sc)
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
		hasJSONPath := p.ValueFrom != nil && p.ValueFrom.JSONPath != ""
		switch {
		case hasJSONPath != (t.Resource != nil):
			c.errs = append(c.errs, field.Forbidden(pp.Child("valueFrom", "jsonPath"), "a resource template needs valueFrom.jsonPath, and only a resource template can use it"))
		case hasJSONPath:
		case hasPath && !leaf:
			c.errs = append(c.errs, field.Forbidden(pp.Child("valueFrom", "path"), "only a container or script template can read an output file"))
		case hasPath && t.Container != nil && len(t.Container.Command) == 0:
			c.errs = append(c.errs, field.Forbidden(pp.Child("valueFrom", "path"), "a container template needs a command to read an output file"))
		case !hasPath && p.Value == nil && t.Suspend == nil:
			c.errs = append(c.errs, field.Required(pp, "an output parameter needs valueFrom.path or a value"))
		case p.Value != nil:
			c.checkText(*p.Value, pp.Child("value"), sc, nil)
		}
	}
}

// checkManifest checks that the manifest is one object and that placeholders are in string values only.
func (c *checker) checkManifest(r *v1alpha1.ResourceTemplate, path *field.Path, sc scope) {
	mp := path.Child("manifest")
	obj, err := manifest.Parse(r.Manifest)
	if err == nil {
		err = manifest.Strings(obj, func(s string) error {
			c.checkText(s, mp, sc, nil)
			return nil
		})
	}
	if err != nil {
		c.errs = append(c.errs, field.Invalid(mp, "", err.Error()))
		return
	}
	meta, _ := obj["metadata"].(map[string]any)
	if obj["apiVersion"] == nil || obj["kind"] == nil || meta["name"] == nil && (r.Action != v1alpha1.ResourceCreate || meta["generateName"] == nil) {
		c.errs = append(c.errs, field.Invalid(mp, "", "the manifest needs apiVersion, kind and metadata.name (or metadata.generateName for create)"))
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
func (c *checker) checkCall(call v1alpha1.Call, path *field.Path, sc scope, hint func(string) string) {
	name, args := call.Template, call.Arguments
	t, ok := c.templates[name]
	if !ok && call.TemplateRef == nil {
		c.errs = append(c.errs, field.NotFound(path.Child("template"), name))
		return
	}
	if len(call.WithItems) > 0 && call.WithParam != "" {
		c.errs = append(c.errs, field.Forbidden(path.Child("withParam"), "use withItems or withParam, not both"))
	}
	c.checkText(call.WithParam, path.Child("withParam"), sc, hint)
	if len(call.WithItems) > 0 || call.WithParam != "" {
		sc = sc.with("item", itemPrefix)
	}
	c.checkWhen(call.When, path.Child("when"), sc, hint)
	if call.TemplateRef != nil {
		// The referenced template is checked when the controller resolves it.
		for i, a := range args.Parameters {
			if a.Value != nil {
				c.checkText(*a.Value, path.Child("arguments", "parameters").Index(i).Child("value"), sc, hint)
			}
		}
		for i, a := range args.Artifacts {
			c.checkFrom(a.From, path.Child("arguments", "artifacts").Index(i).Child("from"), sc, hint)
		}
		return
	}
	c.checkArtifactArgs(t, call, path.Child("arguments", "artifacts"), sc, hint)
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

// itemPrefix in a scope permits "{{item.key}}" for any key. A scope name that ends with ".*" permits any name with its prefix.
const itemPrefix = "item.*"

func (c *checker) checkWhen(s string, path *field.Path, sc scope, hint func(string) string) {
	if s == "" {
		return
	}
	w, err := expr.ParseWhen(s)
	if err != nil {
		c.errs = append(c.errs, field.Invalid(path, s, err.Error()))
		return
	}
	c.checkNames(s, w.Placeholders(), path, sc, hint)
}

// checkText checks that each placeholder in s is in the scope.
func (c *checker) checkText(s string, path *field.Path, sc scope, hint func(string) string) {
	names, err := expr.Placeholders(s)
	if err != nil {
		c.errs = append(c.errs, field.Invalid(path, s, err.Error()))
		return
	}
	c.checkNames(s, names, path, sc, hint)
}

func (c *checker) checkNames(s string, names []string, path *field.Path, sc scope, hint func(string) string) {
	for _, n := range names {
		if isArtifact(n) {
			c.errs = append(c.errs, field.Invalid(path, s, "an artifact placeholder can be used only in arguments.artifacts[].from"))
			continue
		}
		if sc.has(n) {
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
