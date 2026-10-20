// Package resolve turns a Workflow spec with workflowTemplateRef or templateRef into one plain spec.
package resolve

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// ErrInvalid marks a reference that cannot resolve until the workflow or the templates change.
var ErrInvalid = errors.New("invalid template reference")

// Invalidf returns an error that is ErrInvalid but has only the given message.
func Invalidf(format string, args ...any) error {
	return invalidError(fmt.Sprintf(format, args...))
}

type invalidError string

func (e invalidError) Error() string      { return string(e) }
func (invalidError) Is(target error) bool { return target == ErrInvalid }

// maxTemplates is the limit of the CRD schema for the templates of a spec.
const maxTemplates = 200

// Getter reads WorkflowTemplates in the namespace of the workflow and ClusterWorkflowTemplates.
type Getter interface {
	Get(ctx context.Context, name string, clusterScope bool) (*v1alpha1.WorkflowTemplateSpec, error)
}

// Needed tells if a spec has references to resolve.
func Needed(spec *v1alpha1.WorkflowSpec) bool {
	if spec.WorkflowTemplateRef != nil {
		return true
	}
	for _, t := range spec.Templates {
		for _, c := range calls(&t) {
			if c.TemplateRef != nil {
				return true
			}
		}
	}
	return false
}

// Workflow returns the resolved spec: the referenced WorkflowTemplate with the values of the Workflow on top. It adds a
// copy of each template that a templateRef names, with the name "<template set>--<template>".
func Workflow(ctx context.Context, g Getter, spec *v1alpha1.WorkflowSpec) (*v1alpha1.WorkflowTemplateSpec, error) {
	out := spec.WorkflowTemplateSpec.DeepCopy()
	if ref := spec.WorkflowTemplateRef; ref != nil {
		base, err := g.Get(ctx, ref.Name, ref.ClusterScope)
		if err != nil {
			return nil, err
		}
		// An argument that the template does not declare is a mistake, for example a typo. The API refuses it too.
		for _, a := range spec.Arguments.Parameters {
			if !slices.ContainsFunc(base.Arguments.Parameters, func(p v1alpha1.Parameter) bool { return p.Name == a.Name }) {
				return nil, Invalidf("the template %s has no parameter %q", ref.Name, a.Name)
			}
		}
		out = merge(base, &spec.WorkflowTemplateSpec)
	}
	r := &resolver{g: g, out: out, added: map[string]bool{}, own: map[string]bool{}, sources: map[string]*v1alpha1.WorkflowTemplateSpec{}}
	for _, t := range out.Templates {
		r.own[t.Name] = true
	}
	for i := 0; i < len(out.Templates); i++ {
		if err := r.rewrite(ctx, &out.Templates[i], nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// merge puts the values that a Workflow sets on top of the spec of its WorkflowTemplate.
func merge(base, wf *v1alpha1.WorkflowTemplateSpec) *v1alpha1.WorkflowTemplateSpec {
	out := base.DeepCopy()
	if wf.Entrypoint != "" {
		out.Entrypoint = wf.Entrypoint
	}
	// Workflow checked that the template declares each argument.
	for _, a := range wf.Arguments.Parameters {
		for i := range out.Arguments.Parameters {
			if out.Arguments.Parameters[i].Name == a.Name {
				out.Arguments.Parameters[i].Value = a.Value
			}
		}
	}
	if wf.OnExit != "" {
		out.OnExit = wf.OnExit
	}
	if wf.ServiceAccountName != "" {
		out.ServiceAccountName = wf.ServiceAccountName
	}
	if wf.ActiveDeadlineSeconds != nil {
		out.ActiveDeadlineSeconds = wf.ActiveDeadlineSeconds
	}
	if wf.PodGC != nil {
		out.PodGC = wf.PodGC
	}
	if wf.TTLStrategy != nil {
		out.TTLStrategy = wf.TTLStrategy
	}
	out.VolumeClaimTemplates = byName(out.VolumeClaimTemplates, wf.VolumeClaimTemplates, func(v v1alpha1.VolumeClaimTemplate) string { return v.Name })
	out.Volumes = byName(out.Volumes, wf.Volumes, func(v v1alpha1.Volume) string { return v.Name })
	return out
}

// byName puts the items of over on top of base: an item replaces the item of base with the same name.
func byName[T any](base, over []T, name func(T) string) []T {
	for _, o := range over {
		i := slices.IndexFunc(base, func(b T) bool { return name(b) == name(o) })
		if i >= 0 {
			base[i] = o
		} else {
			base = append(base, o)
		}
	}
	return base
}

type resolver struct {
	g       Getter
	out     *v1alpha1.WorkflowTemplateSpec
	added   map[string]bool
	own     map[string]bool // the names of the templates of the workflow itself
	sources map[string]*v1alpha1.WorkflowTemplateSpec
}

// rewrite replaces each templateRef of a template with the name of an included copy.
// from is the template set that the template came from, or nil for the workflow itself.
func (r *resolver) rewrite(ctx context.Context, t *v1alpha1.Template, from *v1alpha1.TemplateRef) error {
	for _, c := range calls(t) {
		ref := c.TemplateRef
		if ref == nil && from != nil {
			// A plain name in an included template names a template of the same template set.
			ref = &v1alpha1.TemplateRef{Name: from.Name, Template: c.Template, ClusterScope: from.ClusterScope}
		}
		if ref == nil {
			continue
		}
		name, err := r.include(ctx, ref)
		if err != nil {
			return err
		}
		c.Template, c.TemplateRef = name, nil
	}
	return nil
}

// include copies a referenced template into the spec once and returns its new name.
func (r *resolver) include(ctx context.Context, ref *v1alpha1.TemplateRef) (string, error) {
	set := ref.Name
	if ref.ClusterScope {
		set = "cluster--" + ref.Name
	}
	name := set + "--" + ref.Template
	if r.own[name] {
		return "", fmt.Errorf("%w: the template name %q is used for the copy of a referenced template: rename the template", ErrInvalid, name)
	}
	if r.added[name] {
		return name, nil
	}
	src, err := r.source(ctx, ref)
	if err != nil {
		return "", err
	}
	var copied *v1alpha1.Template
	for i := range src.Templates {
		if src.Templates[i].Name == ref.Template {
			copied = src.Templates[i].DeepCopy()
		}
	}
	if copied == nil {
		return "", fmt.Errorf("%w: template %q is not in %s", ErrInvalid, ref.Template, describe(ref))
	}
	if len(r.out.Templates) >= maxTemplates {
		return "", fmt.Errorf("%w: the resolved workflow has more than %d templates", ErrInvalid, maxTemplates)
	}
	copied.Name = name
	r.added[name] = true
	if err := r.rewrite(ctx, copied, ref); err != nil {
		return "", err
	}
	r.out.Templates = append(r.out.Templates, *copied)
	return name, nil
}

func (r *resolver) source(ctx context.Context, ref *v1alpha1.TemplateRef) (*v1alpha1.WorkflowTemplateSpec, error) {
	key := fmt.Sprint(ref.ClusterScope, "/", ref.Name)
	if s, ok := r.sources[key]; ok {
		return s, nil
	}
	s, err := r.g.Get(ctx, ref.Name, ref.ClusterScope)
	if err != nil {
		return nil, err
	}
	r.sources[key] = s
	return s, nil
}

func describe(ref *v1alpha1.TemplateRef) string {
	if ref.ClusterScope {
		return fmt.Sprintf("ClusterWorkflowTemplate %q", ref.Name)
	}
	return fmt.Sprintf("WorkflowTemplate %q", ref.Name)
}

// calls returns pointers to the calls of the DAG tasks and the steps of a template.
func calls(t *v1alpha1.Template) []*v1alpha1.Call {
	var out []*v1alpha1.Call
	if t.DAG != nil {
		for i := range t.DAG.Tasks {
			out = append(out, &t.DAG.Tasks[i].Call)
		}
	}
	for i := range t.Steps {
		for j := range t.Steps[i] {
			out = append(out, &t.Steps[i][j].Call)
		}
	}
	return out
}
