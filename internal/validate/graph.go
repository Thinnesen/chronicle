package validate

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/expr"
)

func (c *checker) checkDAG(dag *v1alpha1.DAGTemplate, path *field.Path, sc scope) {
	tasksPath := path.Child("tasks")
	deps := c.dagDependencies(dag, tasksPath)
	if cycle := findCycle(dag, deps); cycle != nil {
		c.errs = append(c.errs, field.Invalid(tasksPath, strings.Join(cycle, " -> "), "the DAG has a cycle"))
		return
	}
	for i, task := range dag.Tasks {
		ancestors := map[string]bool{}
		collectAncestors(task.Name, deps, ancestors)
		taskScope := sc
		for a := range ancestors {
			taskScope = taskScope.with(c.outputNames("tasks."+a, findTask(dag, a).Template)...)
		}
		hint := func(n string) string {
			for _, t := range dag.Tasks {
				if strings.HasPrefix(n, "tasks."+t.Name+".") && !ancestors[t.Name] {
					return fmt.Sprintf("task %q must depend on task %q to use %q", task.Name, t.Name, n)
				}
			}
			return ""
		}
		c.checkCall(task.Template, task.Arguments, tasksPath.Index(i), taskScope, hint)
	}
}

// dagDependencies returns the direct dependencies of each task and reports unknown names.
func (c *checker) dagDependencies(dag *v1alpha1.DAGTemplate, path *field.Path) map[string][]string {
	names := map[string]bool{}
	for _, t := range dag.Tasks {
		names[t.Name] = true
	}
	deps := map[string][]string{}
	for i, t := range dag.Tasks {
		tp := path.Index(i)
		for j, d := range t.Dependencies {
			if !names[d] {
				c.errs = append(c.errs, field.NotFound(tp.Child("dependencies").Index(j), d))
				continue
			}
			deps[t.Name] = append(deps[t.Name], d)
		}
		if t.Depends == "" {
			continue
		}
		e, err := expr.ParseDepends(t.Depends)
		if err != nil {
			c.errs = append(c.errs, field.Invalid(tp.Child("depends"), t.Depends, err.Error()))
			continue
		}
		for _, r := range e.Refs() {
			if !names[r.Task] {
				c.errs = append(c.errs, field.NotFound(tp.Child("depends"), r.Task))
				continue
			}
			deps[t.Name] = append(deps[t.Name], r.Task)
		}
	}
	return deps
}

// findCycle returns a dependency cycle as a list of task names, or nil.
func findCycle(dag *v1alpha1.DAGTemplate, deps map[string][]string) []string {
	const (
		unvisited = iota
		active
		done
	)
	state := map[string]int{}
	var stack []string
	var visit func(string) []string
	visit = func(n string) []string {
		state[n] = active
		stack = append(stack, n)
		for _, d := range deps[n] {
			switch state[d] {
			case active:
				for i, s := range stack {
					if s == d {
						return append(append([]string{}, stack[i:]...), d)
					}
				}
			case unvisited:
				if cycle := visit(d); cycle != nil {
					return cycle
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[n] = done
		return nil
	}
	for _, t := range dag.Tasks {
		if state[t.Name] == unvisited {
			if cycle := visit(t.Name); cycle != nil {
				return cycle
			}
		}
	}
	return nil
}

func collectAncestors(task string, deps map[string][]string, out map[string]bool) {
	for _, d := range deps[task] {
		if !out[d] {
			out[d] = true
			collectAncestors(d, deps, out)
		}
	}
}

func findTask(dag *v1alpha1.DAGTemplate, name string) *v1alpha1.DAGTask {
	for i := range dag.Tasks {
		if dag.Tasks[i].Name == name {
			return &dag.Tasks[i]
		}
	}
	return nil
}

func (c *checker) checkSteps(groups [][]v1alpha1.WorkflowStep, path *field.Path, sc scope) {
	seen := map[string]bool{}
	for i, group := range groups {
		groupScope := sc
		for j, step := range group {
			sp := path.Index(i).Index(j)
			if seen[step.Name] {
				c.errs = append(c.errs, field.Duplicate(sp.Child("name"), step.Name))
			}
			c.checkCall(step.Template, step.Arguments, sp, groupScope, nil)
		}
		for _, step := range group {
			seen[step.Name] = true
			sc = sc.with(c.outputNames("steps."+step.Name, step.Template)...)
		}
	}
}

// outputNames returns the placeholder names for the outputs of a node that runs a template.
func (c *checker) outputNames(prefix, template string) []string {
	t, ok := c.templates[template]
	if !ok {
		return nil
	}
	var names []string
	for _, p := range t.Outputs.Parameters {
		names = append(names, prefix+".outputs.parameters."+p.Name)
	}
	if t.Container != nil || t.Script != nil {
		names = append(names, prefix+".outputs.result")
	}
	return names
}
