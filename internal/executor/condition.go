package executor

import (
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// condOps are the operators of a condition term. Longer operators come first.
var condOps = []string{">=", "<=", "!=", "==", "=", ">", "<"}

// Condition is a parsed resource condition: terms that must all match.
type Condition []condTerm

type condTerm struct {
	path  []string
	op    string
	value string
}

// ParseCondition parses comma-separated "path operator value" terms, for example "status.succeeded > 0".
func ParseCondition(s string) (Condition, error) {
	var c Condition
	for raw := range strings.SplitSeq(s, ",") {
		term, err := parseTerm(strings.TrimSpace(raw))
		if err != nil {
			return nil, err
		}
		c = append(c, term)
	}
	return c, nil
}

func parseTerm(s string) (condTerm, error) {
	for _, op := range condOps {
		path, value, ok := strings.Cut(s, op)
		if !ok {
			continue
		}
		path, value = strings.TrimSpace(path), strings.TrimSpace(value)
		if path == "" || value == "" {
			break
		}
		return condTerm{path: strings.Split(path, "."), op: op, value: value}, nil
	}
	return condTerm{}, fmt.Errorf("bad condition term %q: use \"path operator value\"", s)
}

// Match tells if the object matches all terms. A missing field matches only "!=".
func (c Condition) Match(obj *unstructured.Unstructured) bool {
	for _, t := range c {
		v, found, _ := unstructured.NestedFieldNoCopy(obj.Object, t.path...)
		if !found {
			if t.op != "!=" {
				return false
			}
			continue
		}
		if !compare(fmt.Sprint(v), t.op, t.value) {
			return false
		}
	}
	return true
}

func compare(got, op, want string) bool {
	g, gerr := strconv.ParseFloat(got, 64)
	w, werr := strconv.ParseFloat(want, 64)
	numeric := gerr == nil && werr == nil
	switch op {
	case "=", "==":
		return got == want || numeric && g == w
	case "!=":
		return got != want && !(numeric && g == w)
	case ">":
		return numeric && g > w
	case "<":
		return numeric && g < w
	case ">=":
		return numeric && g >= w
	case "<=":
		return numeric && g <= w
	}
	return false
}
