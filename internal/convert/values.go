package convert

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type obj = map[string]any

// Field names and kinds that the converter uses often.
const (
	fName        = "name"
	fValue       = "value"
	fTemplate    = "template"
	fKey         = "key"
	fConfigMap   = "configMapKeyRef"
	kindCron     = "CronWorkflow"
	kindWorkflow = "Workflow"
)

// converter collects the warnings of one document.
type converter struct {
	warnings []Warning
	// parent is the name of the template that the converter is in. names are the template names in use,
	// and inlined are the templates made from inline templates.
	parent  string
	names   map[string]bool
	inlined []any
	// volumes are the names of the volumes and claim templates that the converter kept.
	volumes map[string]bool
}

// Warning is a part of the Argo document that chronicle does not support. The converter drops it.
type Warning struct {
	// Path is the field, for example "spec.templates[2].sidecars".
	Path    string
	Message string
}

func (w Warning) String() string { return w.Path + ": " + w.Message }

func (c *converter) warn(path, format string, args ...any) {
	c.warnings = append(c.warnings, Warning{Path: path, Message: fmt.Sprintf(format, args...)})
}

// pick copies the keys in copied as they are. It skips the keys in handled, because the caller converts them. It warns
// about each other key.
func (c *converter) pick(in obj, path string, copied []string, handled ...string) obj {
	out := obj{}
	for _, k := range slices.Sorted(maps.Keys(in)) {
		switch {
		case slices.Contains(copied, k):
			out[k] = in[k]
		case slices.Contains(handled, k):
		default:
			c.warn(join(path, k), "chronicle does not support this field: it is dropped")
		}
	}
	return out
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func index(path string, i int) string { return fmt.Sprintf("%s[%d]", path, i) }

func asObj(v any) obj {
	m, _ := v.(obj)
	return m
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

// asInt reads an int or a string with an int, as Argo IntOrString fields have.
func asInt(v any) (int64, error) {
	switch x := v.(type) {
	case float64:
		return int64(x), nil
	case int64:
		return x, nil
	case int:
		return int64(x), nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%q is not a whole number", x)
		}
		return n, nil
	}
	return 0, fmt.Errorf("%v is not a whole number", v)
}

// duration turns an Argo duration into a Go duration string. A bare number means seconds in Argo.
func duration(v any) (string, error) {
	switch x := v.(type) {
	case float64, int64, int:
		n, _ := asInt(x)
		return fmt.Sprintf("%ds", n), nil
	case string:
		s := strings.TrimSpace(x)
		if _, err := strconv.ParseInt(s, 10, 64); err == nil {
			return s + "s", nil
		}
		return s, nil
	}
	return "", fmt.Errorf("%v is not a duration", v)
}

// placeholder matches one "{{...}}" placeholder.
var placeholder = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

// envName returns the environment variable that carries a placeholder into a script.
func envName(name string) string {
	var b strings.Builder
	b.WriteString("CHRONICLE_")
	for _, r := range strings.ToUpper(name) {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
