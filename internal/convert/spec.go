package convert

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// spec converts a workflow spec. forWorkflow allows workflowTemplateRef.
func (c *converter) spec(in obj, path string, forWorkflow bool) (obj, error) {
	copied := []string{"entrypoint", "onExit", "activeDeadlineSeconds", "serviceAccountName", "parallelism"}
	if forWorkflow {
		copied = append(copied, "workflowTemplateRef")
	}
	out := c.pick(in, path, copied, "templates", "arguments", "podGC", "ttlStrategy", "synchronization", "volumes", "volumeClaimTemplates", "templateDefaults")
	c.volumes = map[string]bool{}
	if v := c.volumeClaimTemplates(asList(in["volumeClaimTemplates"]), join(path, "volumeClaimTemplates")); len(v) > 0 {
		out["volumeClaimTemplates"] = v
	}
	if v := c.podVolumes(asList(in["volumes"]), join(path, "volumes")); len(v) > 0 {
		out["volumes"] = v
	}
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
	c.defaults = asObj(in["templateDefaults"])
	for i, t := range asList(in["templates"]) {
		ct, err := c.template(withDefaults(asObj(t), c.defaults), index(join(path, "templates"), i))
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

var callFields = []string{fName, fTemplate, "templateRef", "when", "withItems", "withParam", "continueOn"}

// call converts the parts that a DAG task and a step share.
func (c *converter) call(in obj, path string, extra ...string) (obj, error) {
	out := c.pick(in, path, append(callFields, extra...), "arguments", "withSequence", "inline", "hooks", "onExit")
	if hook := c.exitHook(in, path); hook != nil {
		out["onExit"] = hook
	}
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

// synchronization keeps the first Argo mutex or semaphore. chronicle has one lock for each run.
func (c *converter) synchronization(in obj, path string) obj {
	var locks []obj
	if m := asObj(in["mutex"]); m != nil {
		locks = append(locks, obj{"mutex": obj{fName: m[fName]}})
	}
	for _, m := range asList(in["mutexes"]) {
		locks = append(locks, obj{"mutex": obj{fName: asObj(m)[fName]}})
	}
	sems := asList(in["semaphores"])
	if s := in["semaphore"]; s != nil {
		sems = append([]any{s}, sems...)
	}
	for _, s := range sems {
		if sem := c.semaphore(asObj(s), path); sem != nil {
			locks = append(locks, obj{"semaphore": sem})
		}
	}
	if len(locks) > 1 {
		c.warn(path, "chronicle has one lock for each run: it keeps the first lock")
	}
	if len(locks) == 0 {
		return nil
	}
	return locks[0]
}

// semaphore converts an Argo semaphore with a ConfigMap limit. The lock name comes from the ConfigMap name and key,
// so runs that share the ConfigMap key also share the lock, as in Argo.
func (c *converter) semaphore(in obj, path string) obj {
	ref := asObj(in[fConfigMap])
	name, _ := ref[fName].(string)
	key, _ := ref[fKey].(string)
	if name == "" || key == "" {
		c.warn(path, "chronicle reads the limit of a semaphore only from a ConfigMap key: set synchronization.semaphore by hand")
		return nil
	}
	c.warn(join(path, fConfigMap), "give ConfigMap %v the label chronicle.thinnesen.com/parameters: \"true\"", name)
	return obj{fName: lockName(name + "-" + key), fConfigMap: obj{fName: name, fKey: key}}
}

// lockName makes a valid lock name: lower case letters, digits and dashes, at most 48 characters.
func lockName(s string) string {
	b := []byte(strings.ToLower(s))
	for i, ch := range b {
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') {
			b[i] = '-'
		}
	}
	out := strings.Trim(string(b), "-")
	if len(out) > 48 {
		sum := sha256.Sum256([]byte(s))
		out = strings.TrimRight(out[:39], "-") + "-" + hex.EncodeToString(sum[:4])
	}
	return out
}

// exitHook converts the exit hook of a task or a step: hooks.exit, or the older onExit with a template name.
// Hooks with an expression need expressions, which chronicle does not have.
func (c *converter) exitHook(in obj, path string) obj {
	var hook obj
	if name, ok := in["onExit"].(string); ok {
		hook = obj{fTemplate: name}
	}
	for _, name := range slices.Sorted(maps.Keys(asObj(in["hooks"]))) {
		h := asObj(in["hooks"])[name]
		if name != "exit" {
			c.warn(join(join(path, "hooks"), name), "chronicle has only exit hooks: this hook is dropped")
			continue
		}
		hp := join(path, "hooks.exit")
		hook = c.pick(asObj(h), hp, []string{fTemplate}, "arguments")
		if a := asObj(asObj(h)["arguments"]); a != nil {
			args := c.pick(a, join(hp, "arguments"), nil, "parameters", "artifacts")
			if ps, ok := a["parameters"]; ok {
				args["parameters"] = c.parameters(asList(ps), join(hp, "arguments.parameters"))
			}
			if as, ok := a["artifacts"]; ok {
				args["artifacts"] = c.inputArtifacts(asList(as), join(hp, "arguments.artifacts"), "from")
			}
			hook["arguments"] = args
		}
	}
	return hook
}
