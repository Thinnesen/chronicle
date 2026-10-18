package expr

import (
	"fmt"
	"strings"
)

// Placeholders returns the trimmed names of the "{{name}}" placeholders in s, in order.
func Placeholders(s string) ([]string, error) {
	var names []string
	err := scan(s, func(_ int, _ int, name string) {
		names = append(names, name)
	})
	return names, err
}

// Substitute replaces each "{{name}}" placeholder in s with the value that resolve returns.
// It returns an error for a name that resolve does not know.
func Substitute(s string, resolve func(name string) (string, bool)) (string, error) {
	var b strings.Builder
	last := 0
	var missing []string
	err := scan(s, func(start, end int, name string) {
		v, ok := resolve(name)
		if !ok {
			missing = append(missing, name)
			return
		}
		b.WriteString(s[last:start])
		b.WriteString(v)
		last = end
	})
	if err != nil {
		return "", err
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("unknown placeholder %q", missing[0])
	}
	b.WriteString(s[last:])
	return b.String(), nil
}

// scan calls fn for each placeholder with its byte range and its trimmed name.
func scan(s string, fn func(start, end int, name string)) error {
	for i := 0; ; {
		open := strings.Index(s[i:], "{{")
		if open < 0 {
			return nil
		}
		start := i + open
		rest := s[start+2:]
		closing := strings.Index(rest, "}}")
		if closing < 0 {
			return fmt.Errorf("missing '}}' after position %d", start)
		}
		inner := rest[:closing]
		if strings.Contains(inner, "{{") {
			return fmt.Errorf("nested '{{' at position %d", start)
		}
		name := strings.TrimSpace(inner)
		if name == "" {
			return fmt.Errorf("empty placeholder at position %d", start)
		}
		end := start + 2 + closing + 2
		fn(start, end, name)
		i = end
	}
}
