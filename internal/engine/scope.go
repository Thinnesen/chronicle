package engine

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/expr"
)

// globals returns the workflow placeholders: workflow.name, workflow.parameters.x and others.
func (e *engine) globals() map[string]string {
	created := e.in.CreatedAt.UTC().Format(time.RFC3339)
	scheduled := e.in.ScheduledTime
	if scheduled == "" {
		scheduled = created
	}
	g := map[string]string{
		"workflow.name": e.in.Name, "workflow.namespace": e.in.Namespace, "workflow.uid": e.in.UID,
		"workflow.status": e.status, "workflow.creationTimestamp": created, "workflow.scheduledTime": scheduled,
		"workflow.creationTimestamp.RFC3339": created, "workflow.mainEntrypoint": e.in.Spec.Entrypoint,
		"workflow.creationTimestamp.s": strconv.FormatInt(e.in.CreatedAt.Unix(), 10),
	}
	for c, layout := range expr.Strftime {
		g["workflow.creationTimestamp."+c] = e.in.CreatedAt.UTC().Format(layout)
	}
	for _, p := range e.in.Spec.Arguments.Parameters {
		if v := paramValue(p); v != nil {
			g["workflow.parameters."+p.Name] = *v
		}
	}
	for k, v := range e.in.Labels {
		g["workflow.labels."+k] = v
	}
	for k, v := range e.in.Annotations {
		g["workflow.annotations."+k] = v
	}
	g["workflow.labels.json"] = asJSON(e.in.Labels, "{}")
	g["workflow.annotations.json"] = asJSON(e.in.Annotations, "{}")
	g["workflow.parameters.json"] = asJSON(e.in.Spec.Arguments.Parameters, "[]")
	if out := e.workflowOutputs(); out != nil {
		for _, p := range out.Parameters {
			g["workflow.outputs.parameters."+p.Name] = *p.Value
		}
		for _, a := range out.Artifacts {
			g["workflow.outputs.artifacts."+a.Name] = a.Key
		}
	}
	g["workflow.serviceAccountName"] = e.in.Spec.ServiceAccountName
	g["workflow.duration"] = strconv.FormatFloat(e.in.Now.Sub(e.startedAt).Seconds(), 'f', -1, 64)
	g["workflow.failures"] = e.failures()
	return g
}

// asJSON encodes metadata or parameters. It returns empty for a nil map or list, where JSON has null.
func asJSON(v any, empty string) string {
	data, _ := json.Marshal(v) // Maps of strings and parameters always encode.
	if string(data) == "null" {
		return empty
	}
	return string(data)
}

// failures returns the failed steps of the run as JSON, in the form of Argo: the display name, the message, the
// template, the phase, the Pod and the end of each step. An exit handler can send them in a notification.
func (e *engine) failures() string {
	type failure struct {
		DisplayName  string `json:"displayName"`
		Message      string `json:"message"`
		TemplateName string `json:"templateName"`
		Phase        string `json:"phase"`
		PodName      string `json:"podName"`
		FinishedAt   string `json:"finishedAt"`
	}
	out := []failure{}
	for _, id := range slices.Sorted(maps.Keys(e.nodes)) {
		n := e.nodes[id]
		if (n.Phase != v1alpha1.NodeFailed && n.Phase != v1alpha1.NodeError) || !isWork(n.Type) {
			continue
		}
		f := failure{DisplayName: n.DisplayName, Message: n.Message, TemplateName: n.TemplateName, Phase: string(n.Phase), PodName: n.PodName}
		if n.FinishedAt != nil {
			f.FinishedAt = n.FinishedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, f)
	}
	slices.SortStableFunc(out, func(a, b failure) int { return strings.Compare(a.FinishedAt+a.DisplayName, b.FinishedAt+b.DisplayName) })
	data, _ := json.Marshal(out) // A slice of strings always encodes.
	return string(data)
}

// isWork tells if a node does work: a Pod, a suspend or a task.
func isWork(t v1alpha1.NodeType) bool {
	return t == v1alpha1.NodePod || t == v1alpha1.NodeSuspend || t == v1alpha1.NodeTask
}

// nodeScope returns a resolver for the template of a node that runs as a Pod: the input scope, and the Pod name,
// the node name and the number of the retry.
func (e *engine) nodeScope(c call, n v1alpha1.NodeStatus) expr.Resolver {
	inputs := e.inputScope(n.Inputs)
	own := map[string]string{"pod.name": n.PodName, "node.name": c.nodeName, "retries": strconv.Itoa(c.retry)}
	return func(name string) (string, bool) {
		if v, ok := own[name]; ok {
			return v, true
		}
		return inputs(name)
	}
}

func paramValue(p v1alpha1.Parameter) *string {
	if p.Value != nil {
		return p.Value
	}
	return p.Default
}

// inputScope returns a resolver for the globals and the input parameters of a node.
func (e *engine) inputScope(inputs *v1alpha1.Inputs) expr.Resolver {
	vals := e.globals()
	if inputs != nil {
		for _, p := range inputs.Parameters {
			if p.Value != nil {
				vals["inputs.parameters."+p.Name] = *p.Value
			}
		}
		for _, a := range inputs.Artifacts {
			if a.Key != "" {
				vals["inputs.artifacts."+a.Name] = a.Key
			}
		}
	}
	return func(name string) (string, bool) {
		v, ok := vals[name]
		return v, ok
	}
}

// resolveInputs gives each input parameter of a template its value: the fixed value, the argument or the default.
// A fixed value or a default can use the workflow placeholders. An argument is used as it is.
// Each input artifact gets the key of its argument.
func (e *engine) resolveInputs(t *v1alpha1.Template, c call) (*v1alpha1.Inputs, error) {
	if len(t.Inputs.Parameters) == 0 && len(t.Inputs.Artifacts) == 0 {
		return nil, nil
	}
	out := &v1alpha1.Inputs{}
	args := c.args
	for _, in := range t.Inputs.Parameters {
		v, fromTemplate := in.Value, true
		if v == nil {
			if a, ok := args[in.Name]; ok {
				v, fromTemplate = &a, false
			} else {
				v = in.Default
			}
		}
		if v == nil {
			return nil, fmt.Errorf("parameter %q has no value", in.Name)
		}
		if fromTemplate {
			s, err := expr.Substitute(*v, e.inputScope(nil))
			if err != nil {
				return nil, fmt.Errorf("parameter %q: %w", in.Name, err)
			}
			v = &s
		}
		if len(in.Enum) > 0 && !slices.Contains(in.Enum, *v) {
			return nil, fmt.Errorf("parameter %q: value %q is not one of %q", in.Name, *v, in.Enum)
		}
		out.Parameters = append(out.Parameters, v1alpha1.Parameter{Name: in.Name, Value: v})
	}
	// A URL of the template can use the input parameters, so the artifacts come after them.
	resolve := e.inputScope(&v1alpha1.Inputs{Parameters: out.Parameters})
	for _, in := range t.Inputs.Artifacts {
		a, ok, err := inputArtifact(in, c.artifacts, resolve)
		switch {
		case err != nil:
			return nil, err
		case ok:
			out.Artifacts = append(out.Artifacts, a)
		case !in.Optional:
			return nil, fmt.Errorf("artifact %q has no value", in.Name)
		}
	}
	return out, nil
}

// resolveArgs substitutes the placeholders in the argument values.
func resolveArgs(args v1alpha1.Arguments, resolve expr.Resolver) (map[string]string, error) {
	out := map[string]string{}
	for _, a := range args.Parameters {
		if a.Value == nil {
			continue
		}
		v, err := expr.Substitute(*a.Value, resolve)
		if err != nil {
			return nil, fmt.Errorf("argument %q: %w", a.Name, err)
		}
		out[a.Name] = v
	}
	return out, nil
}

// nodeOutput returns an output of a node: "outputs.result", "outputs.parameters.x", "exitCode", or a field of
// the node: "status" (its phase), "id", "startedAt" or "finishedAt".
// For a task group, it returns a JSON list with the output of each item.
func (e *engine) nodeOutput(n v1alpha1.NodeStatus, key string) (string, bool) {
	switch key {
	case "status":
		return string(n.Phase), true
	case "id":
		return n.ID, true
	case "startedAt":
		return timeText(n.StartedAt)
	case "finishedAt":
		return timeText(n.FinishedAt)
	case "exitCode":
		if n.Outputs == nil || n.Outputs.ExitCode == nil {
			return "", false
		}
		return strconv.Itoa(int(*n.Outputs.ExitCode)), true
	}
	if n.Type != v1alpha1.NodeTaskGroup {
		return outputOf(n.Outputs, key)
	}
	if strings.HasPrefix(key, "outputs.artifacts.") {
		// A list of keys is not one artifact.
		return "", false
	}
	if key == "outputs.parameters" {
		return e.groupParameters(n)
	}
	list := []string{}
	for _, id := range n.Children {
		c := e.nodes[id]
		if c.Phase == v1alpha1.NodeSkipped {
			continue
		}
		v, ok := outputOf(c.Outputs, key)
		if !ok {
			return "", false
		}
		list = append(list, v)
	}
	b, _ := json.Marshal(list)
	return string(b), true
}

// groupParameters returns the output parameters of each item of a task group as a JSON list of objects.
func (e *engine) groupParameters(g v1alpha1.NodeStatus) (string, bool) {
	list := []map[string]string{}
	for _, id := range g.Children {
		c := e.nodes[id]
		if c.Phase == v1alpha1.NodeSkipped {
			continue
		}
		if c.Outputs == nil {
			return "", false
		}
		m := map[string]string{}
		for _, p := range c.Outputs.Parameters {
			if p.Value != nil {
				m[p.Name] = *p.Value
			}
		}
		list = append(list, m)
	}
	b, _ := json.Marshal(list) // Maps of strings always encode.
	return string(b), true
}

func timeText(t *metav1.Time) (string, bool) {
	if t == nil {
		return "", false
	}
	return t.UTC().Format(time.RFC3339), true
}

func outputOf(o *v1alpha1.Outputs, key string) (string, bool) {
	if o == nil {
		return "", false
	}
	if key == "outputs.result" {
		if o.Result == nil {
			return "", false
		}
		return *o.Result, true
	}
	if name, ok := strings.CutPrefix(key, "outputs.artifacts."); ok {
		for _, a := range o.Artifacts {
			if a.Name == name && a.Key != "" {
				return a.Key, true
			}
		}
		return "", false
	}
	name, ok := strings.CutPrefix(key, "outputs.parameters.")
	if !ok {
		return "", false
	}
	for _, p := range o.Parameters {
		if p.Name == name && p.Value != nil {
			return *p.Value, true
		}
	}
	return "", false
}

// itemValue returns "{{item}}" or "{{item.key}}" for an item. Text values have no quotes. Other values are JSON.
func itemValue(item *apiextensionsv1.JSON, name string) (string, bool) {
	raw := item.Raw
	if key, ok := strings.CutPrefix(name, "item."); ok {
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil || obj[key] == nil {
			return "", false
		}
		raw = obj[key]
	} else if name != "item" {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	return string(raw), true
}
