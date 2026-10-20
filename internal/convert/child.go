package convert

import (
	"slices"
	"strings"

	"sigs.k8s.io/yaml"
)

// childFromResource turns a resource step that creates an Argo Workflow from a template into a childWorkflow template.
// Then chronicle links the runs and stops the child with the parent. It returns nil for any other resource step.
func (c *converter) childFromResource(in obj, path string) obj {
	m, _ := in["manifest"].(string)
	if in["action"] != "create" || m == "" {
		return nil
	}
	var doc obj
	if yaml.Unmarshal([]byte(bareValue.ReplaceAllString(m, `$1"$2"`)), &doc) != nil {
		return nil
	}
	api, _ := doc["apiVersion"].(string)
	spec := asObj(doc["spec"])
	ref := asObj(spec["workflowTemplateRef"])
	if !strings.HasPrefix(api, "argoproj.io/") || doc["kind"] != "Workflow" || ref == nil || !onlyKeys(spec, "workflowTemplateRef", "arguments") ||
		!onlyKeys(ref, "name", "clusterScope") || !onlyKeys(asObj(spec["arguments"]), "parameters") {
		return nil
	}
	out := obj{"workflowTemplateRef": ref}
	if args := asObj(spec["arguments"]); args != nil {
		params := make([]any, 0, len(asList(args["parameters"])))
		for _, p := range asList(args["parameters"]) {
			if !onlyKeys(asObj(p), fName, fValue) {
				return nil
			}
			params = append(params, p)
		}
		out["arguments"] = obj{"parameters": params}
	}
	c.warn(path, "the step that creates an Argo Workflow is now a childWorkflow: chronicle links the runs and stops the child with the parent")
	return out
}

// onlyKeys tells if an object has no keys other than the given ones.
func onlyKeys(o obj, keys ...string) bool {
	for k := range o {
		if !slices.Contains(keys, k) {
			return false
		}
	}
	return true
}
