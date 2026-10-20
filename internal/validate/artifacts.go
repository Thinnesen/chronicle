package validate

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/expr"
)

// isArtifact tells if a placeholder names an artifact. Its value is a key in the artifact repository, not text.
func isArtifact(name string) bool {
	return strings.HasPrefix(name, "inputs.artifacts.") || strings.Contains(name, ".outputs.artifacts.")
}

// checkArtifacts checks the artifacts that a template declares.
// Only a container or script template with the executor can download or save files. A dag or steps template can pass
// on an artifact of one of its tasks or steps with from; sc has their outputs.
func (c *checker) checkArtifacts(t *v1alpha1.Template, path *field.Path, sc scope) {
	executor := t.Script != nil || t.Container != nil && len(t.Container.Command) > 0
	leaf := t.Container != nil || t.Script != nil
	for i, a := range t.Inputs.Artifacts {
		ap := path.Child("inputs", "artifacts").Index(i)
		switch {
		case t.Resource != nil || t.Suspend != nil || t.Task != nil:
			c.errs = append(c.errs, field.Forbidden(ap, "a resource, suspend or task template cannot have input artifacts"))
		case leaf && !executor:
			c.errs = append(c.errs, field.Forbidden(ap, "a container template needs a command to download an artifact"))
		case leaf && a.Path == "":
			c.errs = append(c.errs, field.Required(ap.Child("path"), "an input artifact of a container or script template needs a path"))
		}
		c.checkArtifactPath(a, ap)
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
		c.checkArtifactPath(a, ap)
	}
}

// checkPassedArtifact checks an output artifact of a dag or steps template: it names an artifact of a task or a step.
func (c *checker) checkPassedArtifact(a v1alpha1.Artifact, path *field.Path, sc scope) {
	if a.From == "" || a.Path != "" || a.Key != "" {
		c.errs = append(c.errs, field.Invalid(path, a.Name,
			"an output artifact of a dag or steps template needs only from, an artifact of one of its tasks or steps"))
		return
	}
	c.checkFrom(a.From, path.Child("from"), sc, nil)
}

func (c *checker) checkArtifactPath(a v1alpha1.Artifact, path *field.Path) {
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
		c.checkFrom(a.From, ap.Child("from"), sc, hint)
	}
	for _, in := range t.Inputs.Artifacts {
		if !in.Optional && !given[in.Name] {
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

// checkNoArtifactInputs checks a template that the workflow calls. A workflow cannot give artifacts.
func (c *checker) checkNoArtifactInputs(t *v1alpha1.Template, path *field.Path) {
	for _, in := range t.Inputs.Artifacts {
		if !in.Optional {
			c.errs = append(c.errs, field.Forbidden(path, fmt.Sprintf("the template needs artifact %q, but a workflow cannot give artifacts", in.Name)))
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
