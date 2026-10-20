package validate

import (
	"path"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

var shells = []string{"sh", "bash", "ash", "dash", "zsh"}

// ShellWarnings warns about placeholders in a shell command (`sh -c`). The value of such a placeholder becomes shell
// code, so a parameter can run commands. It is a warning, not an error, because Argo workflows often do it.
func ShellWarnings(spec *v1alpha1.WorkflowTemplateSpec, p *field.Path) []string {
	var out []string
	for i, t := range spec.Templates {
		c := t.Container
		if c == nil || len(c.Command) == 0 || !slices.Contains(shells, path.Base(c.Command[0])) {
			continue
		}
		words := append(slices.Clone(c.Command[1:]), c.Args...)
		if !slices.Contains(words, "-c") || !slices.ContainsFunc(words, func(w string) bool { return strings.Contains(w, "{{") }) {
			continue
		}
		out = append(out, p.Child("templates").Index(i).Child("container").String()+
			": a placeholder in a shell command becomes shell code, so a parameter value can run commands. "+
			`Put the value in env and use "$NAME" in the command.`)
	}
	return out
}
