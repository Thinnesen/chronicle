package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLint(t *testing.T) {
	dir := t.TempDir()
	write := func(name, data string) string {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	const head = "apiVersion: chronicle.thinnesen.com/v1alpha1\nkind: WorkflowTemplate\n"
	good := write("ok/etl.yaml", head+"metadata: {name: etl}\nspec: {entrypoint: c, templates: [{name: c, container: {image: x}}]}\n"+
		"---\napiVersion: v1\nkind: ConfigMap\nmetadata: {name: other}\n")
	bad := write("bad.yaml", head+"metadata: {name: bad}\nspec: {entrypoint: c, templates: [{name: c, container: {image: x, args: ['{{inputs.parameters.nope}}']}}]}\n")
	helm := write("chart/job.yaml", "{{ if .Values.on }}\nkind: Job\n{{ end }}\n")
	argo := write("argo.yml", "apiVersion: argoproj.io/v1alpha1\nkind: Workflow\nmetadata: {generateName: x-}\nspec: {entrypoint: c}\n")

	shell := write("shell/echo.yaml", head+"metadata: {name: echo}\nspec: {entrypoint: c, templates: [{name: c, inputs: {parameters: [{name: msg, value: m}]}, "+
		"container: {image: x, command: [sh, -c, 'echo {{inputs.parameters.msg}}']}}]}\n")
	out, err := run(t, &stub{}, "lint", shell)
	if err != nil || !strings.Contains(out, "echo.yaml: WorkflowTemplate echo: warning: spec.templates[0].container: a placeholder in a shell command") {
		t.Errorf("shell: %v\n%s", err, out)
	}
	out, err = run(t, &stub{}, "lint", filepath.Join(dir, "ok"))
	if err != nil || !strings.Contains(out, "etl.yaml: WorkflowTemplate etl is valid") || !strings.Contains(out, "1 resource is valid") {
		t.Errorf("good: %v\n%s", err, out)
	}
	out, err = run(t, &stub{}, "lint", good, bad, argo, filepath.Dir(helm))
	for _, want := range []string{`bad.yaml: WorkflowTemplate bad: not valid in chronicle`, `unknown placeholder "inputs.parameters.nope"`,
		"argo.yml: Workflow x-: an Argo Workflows resource: convert it first with chronicle convert", "job.yaml: not valid YAML", "3 of 4 resources have problems"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not contain %q:\n%s", want, out)
		}
	}
	if err == nil {
		t.Error("lint of bad files returned no error")
	}
}
