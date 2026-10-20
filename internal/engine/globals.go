package engine

import (
	"cmp"
	"maps"
	"slices"
	"time"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// workflowOutputs returns the workflow outputs: the output parameters with a globalName, from the nodes that
// succeeded. When two nodes give the same name, the one that finished later wins, as in Argo.
func (e *engine) workflowOutputs() *v1alpha1.Outputs {
	var done []v1alpha1.NodeStatus
	for _, n := range e.nodes {
		if n.Phase == v1alpha1.NodeSucceeded && n.Outputs != nil && n.Type != v1alpha1.NodeRetry {
			done = append(done, n)
		}
	}
	slices.SortFunc(done, func(a, b v1alpha1.NodeStatus) int {
		return cmp.Or(finishTime(a).Compare(finishTime(b)), cmp.Compare(a.Name, b.Name))
	})
	values, keys := map[string]string{}, map[string]string{}
	for _, n := range done {
		t := e.templates[n.TemplateName]
		if t == nil {
			continue
		}
		for _, p := range t.Outputs.Parameters {
			for _, o := range n.Outputs.Parameters {
				if p.GlobalName != "" && o.Name == p.Name && o.Value != nil {
					values[p.GlobalName] = *o.Value
				}
			}
		}
		for _, a := range t.Outputs.Artifacts {
			for _, o := range n.Outputs.Artifacts {
				if a.GlobalName != "" && o.Name == a.Name && o.Key != "" {
					keys[a.GlobalName] = o.Key
				}
			}
		}
	}
	if len(values) == 0 && len(keys) == 0 {
		return nil
	}
	out := &v1alpha1.Outputs{}
	for _, name := range slices.Sorted(maps.Keys(values)) {
		out.Parameters = append(out.Parameters, v1alpha1.Parameter{Name: name, Value: new(values[name])})
	}
	for _, name := range slices.Sorted(maps.Keys(keys)) {
		out.Artifacts = append(out.Artifacts, v1alpha1.Artifact{Name: name, Key: keys[name]})
	}
	return out
}

func finishTime(n v1alpha1.NodeStatus) time.Time {
	if n.FinishedAt == nil {
		return time.Time{}
	}
	return n.FinishedAt.Time
}
