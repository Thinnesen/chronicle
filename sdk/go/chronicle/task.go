package chronicle

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// Task is a task of a DAG.
type Task struct {
	dag   *Template
	index int
}

func (k *Task) task() *v1alpha1.DAGTask { return &k.dag.t.DAG.Tasks[k.index] }

// Name returns the name of the task.
func (k *Task) Name() string { return k.task().Name }

// After makes the task wait until the other tasks have succeeded or were skipped.
func (k *Task) After(tasks ...*Task) *Task {
	for _, d := range tasks {
		k.task().Dependencies = append(k.task().Dependencies, d.Name())
	}
	return k
}

// Depends sets a dependency expression, for example "build.Succeeded || build.Failed".
func (k *Task) Depends(expr string) *Task {
	k.task().Depends = expr
	return k
}

// Arg gives a value to an input parameter of the template.
func (k *Task) Arg(name, value string) *Task {
	a := &k.task().Arguments
	a.Parameters = append(a.Parameters, v1alpha1.Parameter{Name: name, Value: &value})
	return k
}

// ArtifactArg gives an artifact, from the Artifact placeholder of another task, to an input artifact of the template.
func (k *Task) ArtifactArg(name, from string) *Task {
	a := &k.task().Arguments
	a.Artifacts = append(a.Artifacts, v1alpha1.Artifact{Name: name, From: from})
	return k
}

// When runs the task only when the condition is true, for example "{{tasks.check.outputs.result}} == ok".
func (k *Task) When(cond string) *Task {
	k.task().When = cond
	return k
}

// ContinueOn lets the run go on after the task fails (onFailed) or cannot run (onError), as Argo's continueOn.
func (k *Task) ContinueOn(onFailed, onError bool) *Task {
	k.task().ContinueOn = &v1alpha1.ContinueOn{Failed: onFailed, Error: onError}
	return k
}

// OnExit runs hook after the task ends, also after a failure. The tasks after it wait for the hook. args are the
// parameter arguments of the hook; they can use the outputs and the status of the task.
func (k *Task) OnExit(hook *Template, args map[string]string) *Task {
	h := &v1alpha1.ExitHook{Template: hook.t.Name}
	for _, name := range slices.Sorted(maps.Keys(args)) {
		h.Arguments.Parameters = append(h.Arguments.Parameters, v1alpha1.Parameter{Name: name, Value: new(args[name])})
	}
	k.task().OnExit = h
	k.dag.children = append(k.dag.children, hook)
	return k
}

// Status returns the placeholder for the phase of the task, for example in an exit hook.
func (k *Task) Status() string { return "{{tasks." + k.Name() + ".status}}" }

// WithItems runs the task once for each item. The template uses "{{item}}" or "{{item.key}}".
func (k *Task) WithItems(items ...any) *Task {
	for _, it := range items {
		raw, err := json.Marshal(it)
		if err != nil {
			k.dag.errs = append(k.dag.errs, fmt.Errorf("task %q: item %v: %w", k.Name(), it, err))
			continue
		}
		k.task().WithItems = append(k.task().WithItems, apiextensionsv1.JSON{Raw: raw})
	}
	return k
}

// WithSequence runs the task once for each number from start, count times. count can be a placeholder, for example
// a workflow parameter. format makes the item text, for example "part-%d"; empty is "%d".
func (k *Task) WithSequence(count string, start int, format string) *Task {
	seq := &v1alpha1.Sequence{Count: new(intstr.FromString(count)), Format: format}
	if start != 0 {
		seq.Start = new(intstr.FromInt(start))
	}
	k.task().WithSequence = seq
	return k
}

// Result returns the placeholder for the standard output of the task.
func (k *Task) Result() string { return "{{tasks." + k.Name() + ".outputs.result}}" }

// Output returns the placeholder for an output parameter of the task.
func (k *Task) Output(name string) string {
	return "{{tasks." + k.Name() + ".outputs.parameters." + name + "}}"
}

// Artifact returns the placeholder for an output artifact of the task, for ArtifactArg.
func (k *Task) Artifact(name string) string {
	return "{{tasks." + k.Name() + ".outputs.artifacts." + name + "}}"
}
