package convert

import (
	"maps"
	"slices"
)

// templateKinds are the Argo template types. A default of a type applies only to the templates of that type.
var templateKinds = []string{"container", "script", "resource", "dag", "steps", "suspend", "http", "plugin", "containerSet", "data"}

// withDefaults puts the Argo templateDefaults under a template, as Argo does. The values of the template win.
func withDefaults(t, defaults obj) obj {
	out := obj{}
	for k, v := range defaults {
		if slices.Contains(templateKinds, k) && t[k] == nil {
			continue
		}
		out[k] = v
	}
	return mergeObj(out, t)
}

// mergeObj returns base with over on top. Objects merge key by key; other values of over replace those of base.
func mergeObj(base, over obj) obj {
	out := maps.Clone(base)
	for k, v := range over {
		if b, o := asObj(out[k]), asObj(v); b != nil && o != nil {
			out[k] = mergeObj(b, o)
		} else {
			out[k] = v
		}
	}
	return out
}
