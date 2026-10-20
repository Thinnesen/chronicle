package convert

import (
	"fmt"
	"regexp"
)

// inline turns the inline template of a DAG task or a step into a named template, because chronicle has no
// inline templates. The name is "<parent template>-<task>", made unique.
func (c *converter) inline(in obj, path string) (string, error) {
	base := invalidName.ReplaceAllString(c.parent+"-"+fmt.Sprint(in[fName]), "-")
	name := base
	for i := 2; c.names[name]; i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	c.names[name] = true
	t := obj{fName: name}
	for k, v := range asObj(in["inline"]) {
		if k != fName {
			t[k] = v
		}
	}
	out, err := c.template(t, join(path, "inline"))
	if err != nil {
		return "", err
	}
	c.inlined = append(c.inlined, out)
	c.warn(join(path, "inline"), "the inline template is now the template %q", name)
	return name, nil
}

var invalidName = regexp.MustCompile(`[^-a-zA-Z0-9_]`)
