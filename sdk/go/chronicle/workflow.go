// Package chronicle defines chronicle workflows as Go code, renders them as YAML and submits them.
//
// Build templates with Container, Script, HTTP and DAG. Add tasks to a DAG with Template.Task. Use the
// placeholders that Workflow.Param, Template.Input and the Task outputs return to connect the steps.
package chronicle

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/validate"
)

// Workflow is a workflow definition.
type Workflow struct {
	name, generateName string
	labels             map[string]string
	params             []v1alpha1.Parameter
	entrypoint, onExit *Template
	serviceAccount     string
	parallelism        *int32
	sync               *v1alpha1.Synchronization
}

// New returns a workflow whose runs get the name prefix generateName, for example "etl-".
func New(generateName string) *Workflow {
	return &Workflow{generateName: generateName}
}

// Name gives the run a fixed name instead of a generated one.
func (w *Workflow) Name(name string) *Workflow {
	w.name, w.generateName = name, ""
	return w
}

// Label adds a label to the run.
func (w *Workflow) Label(key, value string) *Workflow {
	if w.labels == nil {
		w.labels = map[string]string{}
	}
	w.labels[key] = value
	return w
}

// ServiceAccount runs the workflow Pods with a Kubernetes service account.
func (w *Workflow) ServiceAccount(name string) *Workflow {
	w.serviceAccount = name
	return w
}

// Parallelism limits the Pods of a run that run at the same time.
func (w *Workflow) Parallelism(n int32) *Workflow {
	w.parallelism = &n
	return w
}

// Mutex lets one run with the lock name run at a time in the namespace. Other runs wait.
func (w *Workflow) Mutex(name string) *Workflow {
	w.sync = &v1alpha1.Synchronization{Mutex: &v1alpha1.Mutex{Name: name}}
	return w
}

// Semaphore lets limit runs with the lock name run at the same time in the namespace. Other runs wait.
func (w *Workflow) Semaphore(name string, limit int32) *Workflow {
	w.sync = &v1alpha1.Synchronization{Semaphore: &v1alpha1.Semaphore{Name: name, Limit: limit}}
	return w
}

// Param declares a workflow parameter with a default value and returns its placeholder.
func (w *Workflow) Param(name, value string) string {
	w.params = append(w.params, v1alpha1.Parameter{Name: name, Value: &value})
	return "{{workflow.parameters." + name + "}}"
}

// Entrypoint sets the template that the run starts with.
func (w *Workflow) Entrypoint(t *Template) *Workflow {
	w.entrypoint = t
	return w
}

// OnExit sets a template that runs after the entrypoint, also after a failure.
func (w *Workflow) OnExit(t *Template) *Workflow {
	w.onExit = t
	return w
}

// Spec returns the validated workflow spec. It contains each template that the entrypoint and the exit handler use.
func (w *Workflow) Spec() (*v1alpha1.WorkflowTemplateSpec, error) {
	if w.entrypoint == nil {
		return nil, errors.New("the workflow has no entrypoint")
	}
	spec := &v1alpha1.WorkflowTemplateSpec{
		Entrypoint:         w.entrypoint.t.Name,
		Arguments:          v1alpha1.Arguments{Parameters: w.params},
		ServiceAccountName: w.serviceAccount,
		Parallelism:        w.parallelism,
		Synchronization:    w.sync,
	}
	seen := map[string]*Template{}
	var errs []error
	var add func(t *Template)
	add = func(t *Template) {
		if prev, ok := seen[t.t.Name]; ok {
			if prev != t {
				errs = append(errs, fmt.Errorf("two different templates have the name %q", t.t.Name))
			}
			return
		}
		seen[t.t.Name] = t
		errs = append(errs, t.errs...)
		spec.Templates = append(spec.Templates, *t.t.DeepCopy())
		for _, c := range t.children {
			add(c)
		}
	}
	add(w.entrypoint)
	if w.onExit != nil {
		spec.OnExit = w.onExit.t.Name
		add(w.onExit)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	if list := validate.Spec(spec, true); len(list) > 0 {
		return nil, fmt.Errorf("the workflow is not valid: %w", list.ToAggregate())
	}
	return spec, nil
}

// Object returns the Workflow object.
func (w *Workflow) Object() (*v1alpha1.Workflow, error) {
	spec, err := w.Spec()
	if err != nil {
		return nil, err
	}
	return &v1alpha1.Workflow{
		APIVersion: v1alpha1.GroupVersion.String(), Kind: "Workflow",
		Name: w.name, GenerateName: w.generateName, Labels: w.labels,
		Spec: v1alpha1.WorkflowSpec{WorkflowTemplateSpec: *spec},
	}, nil
}

// YAML renders the Workflow object, for kubectl or GitOps.
func (w *Workflow) YAML() ([]byte, error) {
	obj, err := w.Object()
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("encode the workflow: %w", err)
	}
	// The status and the creation time are empty for a new object.
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	delete(m, "status")
	if meta, ok := m["metadata"].(map[string]any); ok {
		delete(meta, "creationTimestamp")
	}
	return yaml.Marshal(m)
}

// Template is a step definition: a container, a script or a DAG of tasks.
type Template struct {
	t        v1alpha1.Template
	children []*Template
	errs     []error
}

// Container returns a template that runs an image with a command.
func Container(name, image string, command ...string) *Template {
	c := &v1alpha1.ContainerTemplate{Image: image, Command: command}
	return &Template{t: v1alpha1.Template{Name: name, Container: c}}
}

// Script returns a template that runs a shell script in an image. A script reads its inputs from environment variables.
func Script(name, image, source string) *Template {
	return &Template{t: v1alpha1.Template{Name: name, Script: &v1alpha1.ScriptTemplate{
		Image: image, Source: source,
	}}}
}

// DAG returns a template that runs tasks in the order of their dependencies.
func DAG(name string) *Template {
	return &Template{t: v1alpha1.Template{Name: name, DAG: &v1alpha1.DAGTemplate{}}}
}

func (t *Template) container() *v1alpha1.ContainerTemplate {
	switch {
	case t.t.Container != nil:
		return t.t.Container
	case t.t.Script != nil:
		return &t.t.Script.ContainerTemplate
	}
	t.errs = append(t.errs, fmt.Errorf("template %q has no container", t.t.Name))
	return &v1alpha1.ContainerTemplate{}
}

// Args sets the arguments of the command.
func (t *Template) Args(args ...string) *Template {
	t.container().Args = args
	return t
}

// Env adds an environment variable. Use it to give inputs to a script.
func (t *Template) Env(name, value string) *Template {
	c := t.container()
	c.Env = append(c.Env, corev1.EnvVar{Name: name, Value: value})
	return t
}

// Input declares an input parameter and returns its placeholder.
func (t *Template) Input(name string) string {
	t.t.Inputs.Parameters = append(t.t.Inputs.Parameters, v1alpha1.Parameter{Name: name})
	return "{{inputs.parameters." + name + "}}"
}

// InputDefault declares an input parameter with a default value and returns its placeholder.
func (t *Template) InputDefault(name, value string) string {
	t.t.Inputs.Parameters = append(t.t.Inputs.Parameters, v1alpha1.Parameter{Name: name, Default: &value})
	return "{{inputs.parameters." + name + "}}"
}

// OutputFile declares an output parameter that gets the contents of a file in the container.
func (t *Template) OutputFile(name, path string) *Template {
	p := v1alpha1.Parameter{Name: name, ValueFrom: &v1alpha1.ValueFrom{Path: path}}
	t.t.Outputs.Parameters = append(t.t.Outputs.Parameters, p)
	return t
}

// InputArtifact declares an input artifact that the executor puts at path.
func (t *Template) InputArtifact(name, path string) *Template {
	t.t.Inputs.Artifacts = append(t.t.Inputs.Artifacts, v1alpha1.Artifact{Name: name, Path: path})
	return t
}

// OutputArtifact declares an output artifact that the executor saves from path.
func (t *Template) OutputArtifact(name, path string) *Template {
	t.t.Outputs.Artifacts = append(t.t.Outputs.Artifacts, v1alpha1.Artifact{Name: name, Path: path})
	return t
}

// Retry retries a failed node up to limit times, with a wait that starts at backoff and doubles each time.
func (t *Template) Retry(limit int32, backoff time.Duration) *Template {
	rs := &v1alpha1.RetryStrategy{Limit: limit}
	if backoff > 0 {
		rs.Backoff = &v1alpha1.Backoff{Duration: metav1.Duration{Duration: backoff}, Factor: new(int32(2))}
	}
	t.t.RetryStrategy = rs
	return t
}

// Timeout fails a node of the template, or one attempt of a retry, after d.
func (t *Template) Timeout(d time.Duration) *Template {
	t.t.Timeout = &metav1.Duration{Duration: d}
	return t
}

// Parallelism limits the Pods that run at the same time below a node of this DAG or steps template.
func (t *Template) Parallelism(n int32) *Template {
	t.t.Parallelism = &n
	return t
}

// Task adds a task to a DAG template. The task runs the template tmpl.
func (t *Template) Task(name string, tmpl *Template) *Task {
	if t.t.DAG == nil {
		t.errs = append(t.errs, fmt.Errorf("template %q is not a DAG: it cannot have task %q", t.t.Name, name))
		t.t.DAG = &v1alpha1.DAGTemplate{}
	}
	t.t.DAG.Tasks = append(t.t.DAG.Tasks, v1alpha1.DAGTask{Name: name, Template: tmpl.t.Name})
	t.children = append(t.children, tmpl)
	return &Task{dag: t, index: len(t.t.DAG.Tasks) - 1}
}

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

// TaskTemplate returns a template that sends its input parameters to a worker that runs the task type taskType.
func TaskTemplate(name, taskType string) *Template {
	return &Template{t: v1alpha1.Template{Name: name, Task: &v1alpha1.TaskTemplate{Name: taskType}}}
}

func (t *Template) task() *v1alpha1.TaskTemplate {
	if t.t.Task == nil {
		t.errs = append(t.errs, fmt.Errorf("template %q is not a task template", t.t.Name))
		t.t.Task = &v1alpha1.TaskTemplate{}
	}
	return t.t.Task
}

// Queue sets the queue of a task template.
func (t *Template) Queue(queue string) *Template {
	t.task().Queue = queue
	return t
}

// HeartbeatTimeout fails an attempt of a task template when the worker sends no heartbeat for d.
func (t *Template) HeartbeatTimeout(d time.Duration) *Template {
	t.task().HeartbeatTimeout = &metav1.Duration{Duration: d}
	return t
}

// ScheduleToStartTimeout fails an attempt of a task template when no worker takes the task in d.
func (t *Template) ScheduleToStartTimeout(d time.Duration) *Template {
	t.task().ScheduleToStartTimeout = &metav1.Duration{Duration: d}
	return t
}

// OutputField declares an output parameter of a task template. It gets the field of the result with the same name.
func (t *Template) OutputField(name string) *Template {
	t.t.Outputs.Parameters = append(t.t.Outputs.Parameters, v1alpha1.Parameter{Name: name})
	return t
}
