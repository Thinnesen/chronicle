package expr

import (
	"fmt"
	"strconv"
	"strings"
)

// When is a parsed "when" condition, for example "{{tasks.a.outputs.result}} == heads".
// Placeholder values are always literal operands. A value can never change the structure of the condition.
type When struct {
	root wnode
}

// Resolver returns the value of a placeholder name.
type Resolver func(name string) (string, bool)

type wnode interface {
	eval(Resolver) (string, error)
	placeholders(*[]string)
}

type (
	wOr      struct{ l, r wnode }
	wAnd     struct{ l, r wnode }
	wNot     struct{ x wnode }
	wCompare struct {
		op   string
		l, r wnode
	}
	// wText is a quoted string or a bare word. A quoted string can contain placeholders.
	wText        struct{ s string }
	wPlaceholder struct{ name string }
)

// ParseWhen parses a when condition.
func ParseWhen(s string) (When, error) {
	toks, err := whenTokens(s)
	if err != nil {
		return When{}, err
	}
	p := &wparser{toks: toks}
	root, err := p.or()
	if err != nil {
		return When{}, err
	}
	if p.pos != len(toks) {
		return When{}, fmt.Errorf("unexpected %q in when condition", toks[p.pos].text)
	}
	return When{root: root}, nil
}

// Placeholders returns the placeholder names in the condition.
func (w When) Placeholders() []string {
	var out []string
	w.root.placeholders(&out)
	return out
}

// Eval evaluates the condition.
func (w When) Eval(resolve Resolver) (bool, error) {
	v, err := w.root.eval(resolve)
	if err != nil {
		return false, err
	}
	return toBool(v)
}

func toBool(v string) (bool, error) {
	switch v {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("%q is not true or false", v)
}

func fromBool(b bool) string { return strconv.FormatBool(b) }

func (n wOr) eval(r Resolver) (string, error)  { return logic(n.l, n.r, r, true) }
func (n wAnd) eval(r Resolver) (string, error) { return logic(n.l, n.r, r, false) }

// logic evaluates "||" (or is true) or "&&" with short-circuit.
func logic(l, rn wnode, r Resolver, or bool) (string, error) {
	lv, err := evalBool(l, r)
	if err != nil || lv == or {
		return fromBool(lv), err
	}
	rv, err := evalBool(rn, r)
	return fromBool(rv), err
}

func evalBool(n wnode, r Resolver) (bool, error) {
	v, err := n.eval(r)
	if err != nil {
		return false, err
	}
	return toBool(v)
}

func (n wNot) eval(r Resolver) (string, error) {
	b, err := evalBool(n.x, r)
	return fromBool(!b), err
}

func (n wCompare) eval(r Resolver) (string, error) {
	l, err := n.l.eval(r)
	if err != nil {
		return "", err
	}
	rv, err := n.r.eval(r)
	if err != nil {
		return "", err
	}
	c := strings.Compare(l, rv)
	lf, lerr := strconv.ParseFloat(l, 64)
	rf, rerr := strconv.ParseFloat(rv, 64)
	if lerr == nil && rerr == nil {
		c = compareFloat(lf, rf)
	}
	ok := map[string]bool{"==": c == 0, "!=": c != 0, "<": c < 0, "<=": c <= 0, ">": c > 0, ">=": c >= 0}[n.op]
	return fromBool(ok), nil
}

func compareFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func (n wText) eval(r Resolver) (string, error) { return Substitute(n.s, r) }

func (n wPlaceholder) eval(r Resolver) (string, error) {
	v, ok := r(n.name)
	if !ok {
		return "", fmt.Errorf("unknown placeholder %q", n.name)
	}
	return v, nil
}

func (n wOr) placeholders(o *[]string)      { n.l.placeholders(o); n.r.placeholders(o) }
func (n wAnd) placeholders(o *[]string)     { n.l.placeholders(o); n.r.placeholders(o) }
func (n wNot) placeholders(o *[]string)     { n.x.placeholders(o) }
func (n wCompare) placeholders(o *[]string) { n.l.placeholders(o); n.r.placeholders(o) }
func (n wPlaceholder) placeholders(o *[]string) {
	*o = append(*o, n.name)
}
func (n wText) placeholders(o *[]string) {
	names, _ := Placeholders(n.s)
	*o = append(*o, names...)
}
