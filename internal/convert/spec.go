package convert

import (
	"fmt"
	"slices"
)

// spec converts a workflow spec. forWorkflow allows workflowTemplateRef.
func (c *converter) spec(in obj, path string, forWorkflow bool) (obj, error) {
	copied := []string{"entrypoint", "onExit", "activeDeadlineSeconds", "serviceAccountName", "parallelism"}
	if forWorkflow {
		copied = append(copied, "workflowTemplateRef")
	}
	out := c.pick(in, path, copied, "templates", "arguments", "podGC", "ttlStrategy", "synchronization")
	if v, ok := in["synchronization"]; ok {
		if sync := c.synchronization(asObj(v), join(path, "synchronization")); sync != nil {
			out["synchronization"] = sync
		}
	}
	if v, ok := in["arguments"]; ok {
		a := asObj(v)
		args := c.pick(a, join(path, "arguments"), nil, "parameters", "artifacts")
		if ps, ok := a["parameters"]; ok {
			args["parameters"] = c.parameters(asList(ps), join(path, "arguments.parameters"))
		}
		if as, ok := a["artifacts"]; ok {
			args["artifacts"] = c.inputArtifacts(asList(as), join(path, "arguments.artifacts"))
		}
		out["arguments"] = args
	}
	if v, ok := in["podGC"]; ok {
		gc := c.pick(asObj(v), join(path, "podGC"), []string{"strategy"}, "deleteDelayDuration")
		if d, ok := asObj(v)["deleteDelayDuration"]; ok {
			s, err := duration(d)
			if err != nil {
				return nil, fmt.Errorf("%s.podGC.deleteDelayDuration: %w", path, err)
			}
			gc["deleteDelayDuration"] = s
		}
		out["podGC"] = gc
	}
	if v, ok := in["ttlStrategy"]; ok {
		out["ttlStrategy"] = c.pick(asObj(v), join(path, "ttlStrategy"),
			[]string{"secondsAfterCompletion", "secondsAfterSuccess", "secondsAfterFailure"})
	}
	templates := []any{}
	c.names, c.inlined = map[string]bool{}, nil
	for _, t := range asList(in["templates"]) {
		c.names[fmt.Sprint(asObj(t)[fName])] = true
	}
	for i, t := range asList(in["templates"]) {
		ct, err := c.template(asObj(t), index(join(path, "templates"), i))
		if err != nil {
			return nil, err
		}
		templates = append(templates, ct)
	}
	templates = append(templates, c.inlined...)
	if len(templates) > 0 {
		c.dropUnknownArgs(templates, join(path, "templates"))
		out["templates"] = templates
	}
	return out, nil
}

// dropUnknownArgs removes the parameter arguments that the called template does not declare.
// Argo ignores them; chronicle rejects them.
func (c *converter) dropUnknownArgs(templates []any, path string) {
	inputs := map[string][]string{}
	for _, t := range templates {
		tm := asObj(t)
		name, _ := tm[fName].(string)
		inputs[name] = []string{}
		for _, p := range asList(asObj(tm["inputs"])["parameters"]) {
			n, _ := asObj(p)[fName].(string)
			inputs[name] = append(inputs[name], n)
		}
	}
	prune := func(call obj, callPath string) {
		declared, ok := inputs[fmt.Sprint(call["template"])]
		args := asObj(call["arguments"])
		if !ok || args == nil || call["templateRef"] != nil {
			return
		}
		kept := []any{}
		for _, p := range asList(args["parameters"]) {
			n, _ := asObj(p)[fName].(string)
			if slices.Contains(declared, n) {
				kept = append(kept, p)
			} else {
				c.warn(callPath, "argument %q is not an input of template %q: it is dropped", n, call["template"])
			}
		}
		if len(kept) > 0 {
			args["parameters"] = kept
		} else {
			delete(args, "parameters")
		}
	}
	for i, t := range templates {
		tm, tp := asObj(t), index(path, i)
		for j, task := range asList(asObj(tm["dag"])["tasks"]) {
			prune(asObj(task), index(tp+".dag.tasks", j))
		}
		for j, g := range asList(tm["steps"]) {
			for k, step := range asList(g) {
				prune(asObj(step), index(index(tp+".steps", j), k))
			}
		}
	}
}

// cronSpec converts a CronWorkflow spec. Argo v3 has one schedule; chronicle and Argo v4 have a list.
func (c *converter) cronSpec(in obj, path string) (obj, error) {
	out := c.pick(in, path, []string{"schedules", "timezone", "concurrencyPolicy", "startingDeadlineSeconds",
		"successfulJobsHistoryLimit", "failedJobsHistoryLimit", "suspend"}, "schedule", "workflowSpec", "workflowMetadata")
	if s, ok := in["schedule"].(string); ok {
		out["schedules"] = append(asList(out["schedules"]), s)
	}
	if m := asObj(in["workflowMetadata"]); m != nil {
		out["workflowMetadata"] = c.pick(m, join(path, "workflowMetadata"), []string{"labels", "annotations"})
	}
	ws, err := c.spec(asObj(in["workflowSpec"]), join(path, "workflowSpec"), true)
	if err != nil {
		return nil, err
	}
	out["workflowSpec"] = ws
	return out, nil
}

var callFields = []string{fName, "template", "templateRef", "when", "withItems", "withParam", "continueOn"}

// call converts the parts that a DAG task and a step share.
func (c *converter) call(in obj, path string, extra ...string) (obj, error) {
	out := c.pick(in, path, append(callFields, extra...), "arguments", "withSequence", "inline")
	if _, ok := in["inline"]; ok {
		name, err := c.inline(in, path)
		if err != nil {
			return nil, err
		}
		out["template"] = name
	}
	if a := asObj(in["arguments"]); a != nil {
		args := c.pick(a, join(path, "arguments"), nil, "parameters", "artifacts")
		if ps, ok := a["parameters"]; ok {
			args["parameters"] = c.parameters(asList(ps), join(path, "arguments.parameters"))
		}
		if as, ok := a["artifacts"]; ok {
			args["artifacts"] = c.inputArtifacts(asList(as), join(path, "arguments.artifacts"), "from")
		}
		out["arguments"] = args
	}
	if seq := asObj(in["withSequence"]); seq != nil {
		out["withSequence"] = c.pick(seq, join(path, "withSequence"), []string{"count", "start", "end", "format"})
	}
	return out, nil
}

func (c *converter) dag(in obj, path string) (obj, error) {
	out := c.pick(in, path, []string{"failFast"}, "tasks")
	tasks := []any{}
	for i, t := range asList(in["tasks"]) {
		ct, err := c.call(asObj(t), index(join(path, "tasks"), i), "dependencies", "depends")
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, ct)
	}
	out["tasks"] = tasks
	return out, nil
}

func (c *converter) steps(groups []any, path string) ([]any, error) {
	out := []any{}
	for i, g := range groups {
		group := []any{}
		for j, s := range asList(g) {
			cs, err := c.call(asObj(s), index(index(path, i), j))
			if err != nil {
				return nil, err
			}
			group = append(group, cs)
		}
		out = append(out, group)
	}
	return out, nil
}

// synchronization keeps an Argo mutex. An Argo semaphore reads its limit from a ConfigMap, which chronicle does not do.
func (c *converter) synchronization(in obj, path string) obj {
	var names []any
	if m := asObj(in["mutex"]); m != nil {
		names = append(names, m["name"])
	}
	for _, m := range asList(in["mutexes"]) {
		names = append(names, asObj(m)["name"])
	}
	if _, ok := in["semaphore"]; ok || in["semaphores"] != nil {
		c.warn(path, "a semaphore takes its limit from a ConfigMap in Argo: set synchronization.semaphore with a limit by hand")
	}
	if len(names) > 1 {
		c.warn(path, "chronicle has one lock for each run: it keeps the first mutex")
	}
	if len(names) == 0 {
		return nil
	}
	return obj{"mutex": obj{"name": names[0]}}
}
