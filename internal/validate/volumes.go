package validate

import (
	"path"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// chronicleDirs hold the executor and the input artifacts in each Pod. A user volume cannot be at them, below them or
// above them.
var chronicleDirs = []string{"/var/run/chronicle", "/var/run/chronicle-inputs"}

// checkVolumes checks the names of the volumes and the volume claim templates, and remembers them for the mounts.
func (c *checker) checkVolumes(spec *v1alpha1.WorkflowTemplateSpec, p *field.Path) {
	c.volumes = map[string]bool{}
	check := func(name string, np *field.Path) {
		switch {
		case strings.HasPrefix(name, "chronicle-"):
			c.errs = append(c.errs, field.Invalid(np, name, "chronicle uses names that start with chronicle-"))
		case c.volumes[name]:
			c.errs = append(c.errs, field.Duplicate(np, name))
		}
		c.volumes[name] = true
	}
	for i, v := range spec.VolumeClaimTemplates {
		check(v.Name, p.Child("volumeClaimTemplates").Index(i).Child("name"))
	}
	for i, v := range spec.Volumes {
		check(v.Name, p.Child("volumes").Index(i).Child("name"))
	}
}

// checkMounts checks the volume mounts of a container: known volumes, and paths that do not hide other mounts.
func (c *checker) checkMounts(ct *v1alpha1.ContainerTemplate, t *v1alpha1.Template, p *field.Path) {
	used := map[string]string{}
	for _, a := range t.Inputs.Artifacts {
		used[path.Clean(a.Path)] = "the input artifact " + a.Name + " uses this path"
	}
	for i, m := range ct.VolumeMounts {
		mp := p.Child("volumeMounts").Index(i)
		if !c.volumes[m.Name] {
			c.errs = append(c.errs, field.NotFound(mp.Child("name"), m.Name))
		}
		at := path.Clean(m.MountPath)
		switch {
		case at == "/":
			c.errs = append(c.errs, field.Invalid(mp.Child("mountPath"), m.MountPath, "a volume cannot hide the root directory"))
		case slices.ContainsFunc(chronicleDirs, func(d string) bool { return within(at, d) || within(d, at) }):
			c.errs = append(c.errs, field.Invalid(mp.Child("mountPath"), m.MountPath, "chronicle uses the paths below /var/run/chronicle"))
		case used[at] != "":
			c.errs = append(c.errs, field.Invalid(mp.Child("mountPath"), m.MountPath, used[at]))
		}
		used[at] = "another mount uses this path"
		if m.SubPath != "" && (path.IsAbs(m.SubPath) || slices.Contains(strings.Split(m.SubPath, "/"), "..")) {
			c.errs = append(c.errs, field.Invalid(mp.Child("subPath"), m.SubPath, "give a relative path in the volume, without .."))
		}
	}
}

// within tells if the clean path p is dir or below it.
func within(p, dir string) bool {
	return p == dir || strings.HasPrefix(p, dir+"/")
}
