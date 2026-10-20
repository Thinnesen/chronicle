package convert

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// template converts one Argo template.
func (c *converter) template(in obj, path string) (obj, error) {
	defer func(p string) { c.parent = p }(c.parent)
	c.parent, _ = in[fName].(string)
	out := c.pick(in, path, []string{fName, "dag", "steps"},
		"inputs", "outputs", "container", "script", "suspend", "resource", "http", "retryStrategy", "timeout", "activeDeadlineSeconds",
		"parallelism", "synchronization", "memoize")
	var err error
	if v, ok := in["parallelism"]; ok {
		if out["dag"] != nil || out["steps"] != nil {
			out["parallelism"] = v
		} else {
			c.warn(join(path, "parallelism"), "only a dag or steps template can limit its parallelism: it is dropped")
		}
	}
	if v, ok := in["memoize"]; ok {
		if out["memoize"], err = c.memoize(asObj(v), join(path, "memoize")); err != nil {
			return nil, err
		}
	}
	if v, ok := in["synchronization"]; ok {
		if sync := c.synchronization(asObj(v), join(path, "synchronization")); sync != nil {
			out["synchronization"] = sync
		}
	}
	if v, ok := in["inputs"]; ok {
		out["inputs"] = c.inputs(asObj(v), join(path, "inputs"))
	}
	if v, ok := in["outputs"]; ok {
		out["outputs"] = c.outputs(asObj(v), join(path, "outputs"))
	}
	if v, ok := in["container"]; ok {
		out["container"] = c.container(asObj(v), join(path, "container"), nil)
	}
	if v, ok := in["script"]; ok {
		if out["script"], err = c.script(asObj(v), join(path, "script")); err != nil {
			return nil, err
		}
	}
	if v, ok := in["suspend"]; ok {
		if out["suspend"], err = c.suspend(asObj(v), join(path, "suspend")); err != nil {
			return nil, err
		}
	}
	if v, ok := in["resource"]; ok {
		out["resource"] = c.resource(asObj(v), join(path, "resource"))
	}
	if v, ok := in["http"]; ok {
		out["http"] = c.http(asObj(v), join(path, "http"))
	}
	if v, ok := in["retryStrategy"]; ok {
		if out["retryStrategy"], err = c.retry(asObj(v), join(path, "retryStrategy")); err != nil {
			return nil, err
		}
	}
	if err := c.timeout(in, out, path); err != nil {
		return nil, err
	}
	if dag := asObj(out["dag"]); dag != nil {
		if out["dag"], err = c.dag(dag, join(path, "dag")); err != nil {
			return nil, err
		}
	}
	if steps, ok := out["steps"]; ok {
		if out["steps"], err = c.steps(asList(steps), join(path, "steps")); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// timeout takes timeout, or else activeDeadlineSeconds, which is the same for a template.
func (c *converter) timeout(in, out obj, path string) error {
	v, ok := in["timeout"]
	if !ok {
		v, ok = in["activeDeadlineSeconds"]
	}
	if !ok {
		return nil
	}
	d, err := duration(v)
	if err != nil {
		return fmt.Errorf("%s: timeout: %w", path, err)
	}
	if strings.Contains(d, "{{") {
		c.warn(join(path, "timeout"), "a timeout cannot have placeholders: it is dropped")
		return nil
	}
	out["timeout"] = d
	return nil
}

var containerFields = []string{"image", "command", "args", "env", "envFrom", "workingDir", "resources", "imagePullPolicy"}

// container keeps the fields of the small chronicle container. The security context comes from chronicle.
func (c *converter) container(in obj, path string, extra []string) obj {
	out := c.pick(in, path, append(slices.Clone(containerFields), extra...), "volumeMounts")
	if m := c.volumeMounts(asList(in["volumeMounts"]), join(path, "volumeMounts")); len(m) > 0 {
		out["volumeMounts"] = m
	}
	return out
}

// script converts a script template. Placeholders in a shell script become environment variables,
// because chronicle never puts parameter values into code.
func (c *converter) script(in obj, path string) (obj, error) {
	out := c.container(in, path, []string{"source"})
	source, _ := out["source"].(string)
	names := placeholder.FindAllStringSubmatch(source, -1)
	if len(names) == 0 {
		return out, nil
	}
	if !isShell(out["command"]) {
		return nil, fmt.Errorf("%s.source: the script has placeholders and is not a shell script: "+
			"read the values from environment variables (set them in env), then convert again", path)
	}
	env := asList(out["env"])
	seen := map[string]bool{}
	for _, m := range names {
		name := envName(m[1])
		if !seen[name] {
			seen[name] = true
			env = append(env, obj{fName: name, fValue: "{{" + m[1] + "}}"})
		}
	}
	out["source"] = placeholder.ReplaceAllStringFunc(source, func(s string) string {
		return "${" + envName(placeholder.FindStringSubmatch(s)[1]) + "}"
	})
	out["env"] = env
	c.warn(join(path, "source"), "the placeholders in the script are now environment variables (%d)", len(seen))
	return out, nil
}

func isShell(command any) bool {
	cmd := asList(command)
	if len(cmd) == 0 {
		return true
	}
	first, _ := cmd[0].(string)
	base := first[strings.LastIndex(first, "/")+1:]
	return slices.Contains([]string{"sh", "bash", "ash", "dash", "zsh"}, base)
}

func (c *converter) suspend(in obj, path string) (obj, error) {
	out := c.pick(in, path, nil, "duration")
	if v, ok := in["duration"]; ok {
		d, err := duration(v)
		if err != nil {
			return nil, fmt.Errorf("%s.duration: %w", path, err)
		}
		out["duration"] = d
	}
	return out, nil
}

func (c *converter) resource(in obj, path string) obj {
	out := c.pick(in, path, []string{"action", "manifest", "successCondition", "failureCondition", "setOwnerReference"})
	if m, ok := out["manifest"].(string); ok {
		if quoted := bareValue.ReplaceAllString(m, `$1"$2"`); quoted != m {
			out["manifest"] = quoted
			c.warn(join(path, "manifest"), "placeholders that were whole YAML values now have quotes")
		}
	}
	if out["action"] == "replace" {
		c.warn(join(path, "action"), "chronicle has no replace action: it uses apply")
		out["action"] = "apply"
	}
	return out
}

// bareValue matches a YAML value or list item that is only a placeholder without quotes. chronicle parses the
// manifest before it puts the values in, so such a placeholder must be a string.
var bareValue = regexp.MustCompile(`(?m)^(\s*(?:[^\s#:"'][^#:]*:\s+|-\s+))(\{\{[^{}]+\}\})\s*$`)

// http keeps the request. A status from 200 to 299 is success in chronicle; Argo can test it with an expression.
func (c *converter) http(in obj, path string) obj {
	out := c.pick(in, path, []string{"url", "method", "body"}, "headers", "successCondition")
	if _, ok := in["successCondition"]; ok {
		c.warn(join(path, "successCondition"), "chronicle has no expressions: a status from 200 to 299 is success")
	}
	if hs, ok := in["headers"]; ok {
		list := make([]any, 0, len(asList(hs)))
		for i, h := range asList(hs) {
			hp := index(join(path, "headers"), i)
			ch := c.pick(asObj(h), hp, []string{fName, fValue}, "valueFrom")
			if vf := asObj(asObj(h)["valueFrom"]); vf != nil {
				ch["valueFrom"] = c.pick(vf, join(hp, "valueFrom"), []string{"secretKeyRef"})
			}
			list = append(list, ch)
		}
		out["headers"] = list
	}
	return out
}

// memoize keeps the key and the age. chronicle keeps the results in its archive database, not in a ConfigMap.
func (c *converter) memoize(in obj, path string) (obj, error) {
	out := c.pick(in, path, []string{fKey}, "maxAge", "cache")
	if _, ok := in["cache"]; ok {
		c.warn(join(path, "cache"), "chronicle keeps the results in its archive database, not in a ConfigMap")
	}
	if v, ok := in["maxAge"]; ok && v != "" {
		d, err := duration(v)
		if err != nil {
			return nil, fmt.Errorf("%s.maxAge: %w", path, err)
		}
		out["maxAge"] = d
	}
	return out, nil
}

func (c *converter) retry(in obj, path string) (obj, error) {
	out := c.pick(in, path, []string{"retryPolicy"}, "limit", "backoff")
	limit, err := asInt(in["limit"])
	if in["limit"] == nil {
		c.warn(join(path, "limit"), "an Argo retry without a limit retries forever; chronicle needs a limit and uses 10")
		limit, err = 10, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s.limit: %w", path, err)
	}
	out["limit"] = limit
	if out["retryPolicy"] == "OnTransientError" {
		c.warn(join(path, "retryPolicy"), "chronicle has no OnTransientError policy: it uses OnError")
		out["retryPolicy"] = "OnError"
	}
	if b := asObj(in["backoff"]); b != nil {
		bo := c.pick(b, join(path, "backoff"), nil, "duration", "factor", "maxDuration")
		for _, k := range []string{"duration", "maxDuration"} {
			if v, ok := b[k]; ok {
				if bo[k], err = duration(v); err != nil {
					return nil, fmt.Errorf("%s.backoff.%s: %w", path, k, err)
				}
			}
		}
		if v, ok := b["factor"]; ok {
			if bo["factor"], err = asInt(v); err != nil {
				return nil, fmt.Errorf("%s.backoff.factor: %w", path, err)
			}
		}
		out["backoff"] = bo
	}
	return out, nil
}

func (c *converter) inputs(in obj, path string) obj {
	out := c.pick(in, path, nil, "parameters", "artifacts")
	if ps, ok := in["parameters"]; ok {
		out["parameters"] = c.parameters(asList(ps), join(path, "parameters"))
	}
	if as, ok := in["artifacts"]; ok {
		out["artifacts"] = c.inputArtifacts(asList(as), join(path, "artifacts"), "path", "optional", "from", "globalName")
	}
	return out
}

// inputArtifacts converts input artifacts or artifact arguments. They keep raw text and the URL of http.
// Other sources, such as s3 or git, need credentials that chronicle does not have: they are dropped.
func (c *converter) inputArtifacts(list []any, path string, fields ...string) []any {
	plain := make([]any, 0, len(list))
	for _, a := range list {
		m := maps.Clone(asObj(a))
		delete(m, "http")
		plain = append(plain, m)
	}
	out := c.artifacts(plain, path, append(fields, "raw", "mode")...)
	for i, a := range list {
		if h := asObj(asObj(a)["http"]); h != nil {
			asObj(out[i])["http"] = c.pick(h, join(index(path, i), "http"), []string{"url"})
		}
	}
	return out
}

func (c *converter) outputs(in obj, path string) obj {
	out := c.pick(in, path, nil, "parameters", "artifacts", "result")
	if ps, ok := in["parameters"]; ok {
		list := make([]any, 0, len(asList(ps)))
		for i, p := range asList(ps) {
			pp := index(join(path, "parameters"), i)
			if _, jq := asObj(asObj(p)["valueFrom"])["jqFilter"]; jq {
				c.warn(pp, "chronicle has no jq filters: the output parameter is dropped; use valueFrom.jsonPath")
				continue
			}
			list = append(list, c.outputParameter(asObj(p), pp))
		}
		out["parameters"] = list
	}
	if as, ok := in["artifacts"]; ok {
		out["artifacts"] = c.artifacts(asList(as), join(path, "artifacts"), "path", "optional", "from", "globalName")
		for i, a := range asList(as) {
			// Argo runs the step as root; chronicle runs it as a user without root, who cannot create most paths.
			// An input works at any path, because chronicle mounts it there.
			if p, ok := asObj(a)["path"].(string); ok && p != "/tmp" && !strings.HasPrefix(p, "/tmp/") {
				c.warn(join(index(join(path, "artifacts"), i), "path"),
					"chronicle runs steps as the user 65532, which cannot create %s: use a path below /tmp", p)
			}
		}
	}
	return out
}

// outputParameter converts valueFrom. An Argo suspend output with valueFrom.supplied gets its value from the approval.
func (c *converter) outputParameter(in obj, path string) obj {
	out := c.pick(in, path, []string{fName, fValue, "default", "description", "enum", "globalName"}, "valueFrom")
	vf := asObj(in["valueFrom"])
	if vf == nil {
		return out
	}
	v := c.pick(vf, join(path, "valueFrom"), []string{"path", "jsonPath"}, "supplied", "default", "parameter")
	if d, ok := vf["default"]; ok {
		out["default"] = d
	}
	// An Argo output of a dag or steps template can read a parameter of a child; in chronicle it is a value.
	if p, ok := vf["parameter"]; ok {
		out[fValue] = p
	}
	if len(v) > 0 {
		out["valueFrom"] = v
	}
	return out
}

func (c *converter) parameters(list []any, path string) []any {
	out := make([]any, 0, len(list))
	for i, p := range list {
		pp := index(path, i)
		cp := c.pick(asObj(p), pp, []string{fName, fValue, "default", "enum", "description"}, "valueFrom")
		if vf := asObj(asObj(p)["valueFrom"]); vf != nil {
			c.configMapParameter(vf, cp, join(pp, "valueFrom"))
		}
		out = append(out, cp)
	}
	return out
}

// configMapParameter keeps a parameter that reads a ConfigMap. chronicle reads only ConfigMaps with its label.
func (c *converter) configMapParameter(vf, out obj, path string) {
	kept := c.pick(vf, path, []string{fConfigMap}, "default")
	if d, ok := vf["default"]; ok {
		out["default"] = d
	}
	ref := asObj(kept["configMapKeyRef"])
	if ref == nil {
		return
	}
	out["valueFrom"] = obj{"configMapKeyRef": c.pick(ref, join(path, "configMapKeyRef"), []string{fName, "key"})}
	c.warn(join(path, "configMapKeyRef"), "give ConfigMap %v the label chronicle.thinnesen.com/parameters: \"true\"", ref[fName])
}

// artifacts keeps the name and the given fields. The data always comes from the repository of the namespace.
func (c *converter) artifacts(list []any, path string, fields ...string) []any {
	out := make([]any, 0, len(list))
	for i, a := range list {
		out = append(out, c.pick(asObj(a), index(path, i), append([]string{fName}, fields...), "archive"))
	}
	return out
}
