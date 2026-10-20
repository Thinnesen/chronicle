package validate

import (
	"fmt"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/expr"
)

// isArtifact tells if a placeholder names an artifact. Its value is a key in the artifact repository, not text.
func isArtifact(name string) bool {
	if pathPlaceholder.MatchString(name) {
		return false
	}
	return strings.HasPrefix(name, "inputs.artifacts.") || strings.Contains(name, ".outputs.artifacts.")
}

// pathPlaceholder matches the placeholder of a declared path, for example "inputs.artifacts.data.path".
// Its value is text: the path in the container.
var pathPlaceholder = regexp.MustCompile(`^(inputs\.artifacts|outputs\.artifacts|outputs\.parameters)\.[^.]+\.path$`)

// checkArtifacts checks the artifacts that a template declares.
// Only a container or script template with the executor can download or save files. A dag or steps template can pass
// on an artifact of one of its tasks or steps with from; sc has their outputs.
func (c *checker) checkArtifacts(t *v1alpha1.Template, path *field.Path, sc scope) {
	executor := t.Script != nil || t.Container != nil && len(t.Container.Command) > 0
	leaf := t.Container != nil || t.Script != nil
	for i, a := range t.Inputs.Artifacts {
		ap := path.Child("inputs", "artifacts").Index(i)
		switch {
		case t.Resource != nil || t.Suspend != nil || t.Task != nil || t.HTTP != nil:
			c.errs = append(c.errs, field.Forbidden(ap, "a resource, suspend, task or http template cannot have input artifacts"))
		case leaf && !executor:
			c.errs = append(c.errs, field.Forbidden(ap, "a container template needs a command to download an artifact"))
		case leaf && a.Path == "":
			c.errs = append(c.errs, field.Required(ap.Child("path"), "an input artifact of a container or script template needs a path"))
		}
		c.checkArtifactPath(a, ap, true)
		c.checkSource(a, ap, sc)
	}
	for i, a := range t.Outputs.Artifacts {
		ap := path.Child("outputs", "artifacts").Index(i)
		if t.DAG != nil || t.Steps != nil {
			c.checkPassedArtifact(a, ap, sc)
			continue
		}
		switch {
		case !leaf:
			c.errs = append(c.errs, field.Forbidden(ap, "only a container or script template can save an artifact"))
		case !executor:
			c.errs = append(c.errs, field.Forbidden(ap, "a container template needs a command to save an artifact"))
		case a.Path == "":
			c.errs = append(c.errs, field.Required(ap.Child("path"), "an output artifact needs a path"))
		}
		c.checkArtifactPath(a, ap, false)
	}
}

// checkPassedArtifact checks an output artifact of a dag or steps template. It must name an artifact of a task or a
// step.
func (c *checker) checkPassedArtifact(a v1alpha1.Artifact, path *field.Path, sc scope) {
	if a.From == "" || a.Path != "" || a.Key != "" {
		c.errs = append(c.errs, field.Invalid(path, a.Name,
			"an output artifact of a dag or steps template needs only from, an artifact of one of its tasks or steps"))
		return
	}
	c.checkFrom(a.From, path.Child("from"), sc, nil)
}

func (c *checker) checkArtifactPath(a v1alpha1.Artifact, path *field.Path, input bool) {
	if !input && (a.Raw != nil || a.HTTP != nil || a.Mode != nil) {
		c.errs = append(c.errs, field.Forbidden(path, "only an input or an argument can have raw or http"))
	}
	if strings.Contains(a.Path, "{{") {
		c.errs = append(c.errs, field.Invalid(path.Child("path"), a.Path, "an artifact path cannot have placeholders"))
	}
	if a.From != "" || a.Key != "" {
		c.errs = append(c.errs, field.Forbidden(path, "only an argument can use from, and only the status sets key"))
	}
}

// checkArtifactArgs checks the artifact arguments of a call against the input artifacts of the template.
func (c *checker) checkArtifactArgs(t *v1alpha1.Template, call v1alpha1.Call, path *field.Path, sc scope, hint func(string) string) {
	given := map[string]bool{}
	for i, a := range call.Arguments.Artifacts {
		ap := path.Index(i)
		given[a.Name] = true
		if findArtifact(t.Inputs.Artifacts, a.Name) == nil {
			c.errs = append(c.errs, field.NotFound(ap.Child("name"), a.Name))
			continue
		}
		c.checkSource(a, ap, sc)
		if a.Raw == nil && a.HTTP == nil {
			c.checkFrom(a.From, ap.Child("from"), sc, hint)
		}
	}
	for _, in := range t.Inputs.Artifacts {
		if !in.Optional && !given[in.Name] && !hasSource(in) {
			c.errs = append(c.errs, field.Required(path, fmt.Sprintf("artifact %q of template %q", in.Name, call.Template)))
		}
	}
}

// checkFrom checks that from is exactly one artifact placeholder in the scope.
func (c *checker) checkFrom(from string, path *field.Path, sc scope, hint func(string) string) {
	names, err := expr.Placeholders(from)
	s := strings.TrimSpace(from)
	if err != nil || len(names) != 1 || !strings.HasPrefix(s, "{{") || !strings.HasSuffix(s, "}}") || !isArtifact(names[0]) {
		c.errs = append(c.errs, field.Invalid(path, from,
			`from must be one artifact placeholder, for example "{{tasks.make.outputs.artifacts.data}}"`))
		return
	}
	if sc.has(names[0]) {
		return
	}
	msg := fmt.Sprintf("unknown placeholder %q", names[0])
	if hint != nil && hint(names[0]) != "" {
		msg = hint(names[0])
	}
	c.errs = append(c.errs, field.Invalid(path, from, msg))
}

// checkSource checks the source of an artifact. An artifact has at most one of from, raw and http. The placeholders of
// a URL must be in the scope.
func (c *checker) checkSource(a v1alpha1.Artifact, path *field.Path, sc scope) {
	n := 0
	for _, set := range []bool{a.From != "", a.Raw != nil, a.HTTP != nil} {
		if set {
			n++
		}
	}
	if n > 1 {
		c.errs = append(c.errs, field.Invalid(path, a.Name, "an artifact has one source: from, raw or http"))
	}
	if a.HTTP != nil {
		c.checkText(a.HTTP.URL, path.Child("http", "url"), sc, nil)
	}
}

// hasSource tells if an artifact brings its own content: raw text or a URL.
func hasSource(a v1alpha1.Artifact) bool {
	return a.Raw != nil || a.HTTP != nil
}

// checkWorkflowArtifacts checks the artifacts that the workflow gives to its entrypoint. They need raw or http,
// because no step ran before. It returns their names.
func (c *checker) checkWorkflowArtifacts(spec *v1alpha1.WorkflowTemplateSpec, entry *v1alpha1.Template, path *field.Path) map[string]bool {
	given := map[string]bool{}
	for i, a := range spec.Arguments.Artifacts {
		ap := path.Index(i)
		given[a.Name] = true
		switch {
		case !hasSource(a):
			c.errs = append(c.errs, field.Required(ap, "a workflow artifact needs raw or http"))
		case entry != nil && findArtifact(entry.Inputs.Artifacts, a.Name) == nil:
			c.errs = append(c.errs, field.NotFound(ap.Child("name"), a.Name))
		}
		c.checkSource(a, ap, c.globals)
	}
	return given
}

// checkWorkflowInputs checks a template that the workflow calls. The workflow arguments or the template itself must
// give each artifact that it needs.
func (c *checker) checkWorkflowInputs(t *v1alpha1.Template, given map[string]bool, path *field.Path) {
	for _, in := range t.Inputs.Artifacts {
		if !in.Optional && !given[in.Name] && !hasSource(in) {
			c.errs = append(c.errs, field.Required(path, fmt.Sprintf("artifact %q of the template: give it in spec.arguments.artifacts", in.Name)))
		}
	}
}

func findArtifact(list []v1alpha1.Artifact, name string) *v1alpha1.Artifact {
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}

// pathNames returns the path placeholders of a template, for a container, script, resource or http template.
func pathNames(t *v1alpha1.Template) []string {
	var names []string
	for _, a := range t.Inputs.Artifacts {
		names = append(names, "inputs.artifacts."+a.Name+".path")
	}
	for _, a := range t.Outputs.Artifacts {
		names = append(names, "outputs.artifacts."+a.Name+".path")
	}
	for _, p := range t.Outputs.Parameters {
		if p.ValueFrom != nil && p.ValueFrom.Path != "" {
			names = append(names, "outputs.parameters."+p.Name+".path")
		}
	}
	return names
}
