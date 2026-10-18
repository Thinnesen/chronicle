// Package expr parses the small expression languages of a workflow spec:
// "depends" expressions and "{{...}}" placeholders.
package expr

import (
	"fmt"
	"strings"
)

// Result is a task result that a depends expression can test.
type Result string

// The task results. An empty Result means Succeeded or Skipped.
const (
	ResultAny          Result = ""
	ResultSucceeded    Result = "Succeeded"
	ResultFailed       Result = "Failed"
	ResultErrored      Result = "Errored"
	ResultSkipped      Result = "Skipped"
	ResultOmitted      Result = "Omitted"
	ResultAnySucceeded Result = "AnySucceeded"
	ResultAllFailed    Result = "AllFailed"
)

var knownResults = map[Result]bool{
	ResultSucceeded: true, ResultFailed: true, ResultErrored: true, ResultSkipped: true,
	ResultOmitted: true, ResultAnySucceeded: true, ResultAllFailed: true,
}

// Ref is a reference to a task result in a depends expression.
type Ref struct {
	Task   string
	Result Result
}

// Lookup tells if a task has a result.
type Lookup func(task string, result Result) bool

// Depends is a parsed depends expression.
type Depends struct {
	root node
}

// Refs returns the task references in the order of the expression.
func (d Depends) Refs() []Ref {
	var refs []Ref
	if d.root != nil {
		d.root.refs(&refs)
	}
	return refs
}

// Eval evaluates the expression. A bare task name is true when the task Succeeded or was Skipped.
// The zero Depends is always true.
func (d Depends) Eval(lookup Lookup) bool {
	return d.root == nil || d.root.eval(lookup)
}

type node interface {
	eval(Lookup) bool
	refs(*[]Ref)
}

type orNode struct{ l, r node }
type andNode struct{ l, r node }
type notNode struct{ x node }
type refNode struct{ ref Ref }

func (n orNode) eval(f Lookup) bool  { return n.l.eval(f) || n.r.eval(f) }
func (n andNode) eval(f Lookup) bool { return n.l.eval(f) && n.r.eval(f) }
func (n notNode) eval(f Lookup) bool { return !n.x.eval(f) }
func (n refNode) eval(f Lookup) bool {
	if n.ref.Result == ResultAny {
		return f(n.ref.Task, ResultSucceeded) || f(n.ref.Task, ResultSkipped)
	}
	return f(n.ref.Task, n.ref.Result)
}

func (n orNode) refs(r *[]Ref)  { n.l.refs(r); n.r.refs(r) }
func (n andNode) refs(r *[]Ref) { n.l.refs(r); n.r.refs(r) }
func (n notNode) refs(r *[]Ref) { n.x.refs(r) }
func (n refNode) refs(r *[]Ref) { *r = append(*r, n.ref) }

// ParseDepends parses a depends expression such as "a.Succeeded && !(b || c.Failed)".
func ParseDepends(s string) (Depends, error) {
	toks, err := tokenize(s)
	if err != nil {
		return Depends{}, err
	}
	p := &parser{toks: toks}
	root, err := p.parseOr()
	if err != nil {
		return Depends{}, err
	}
	if p.pos != len(p.toks) {
		return Depends{}, fmt.Errorf("unexpected %q in depends expression", p.toks[p.pos])
	}
	return Depends{root: root}, nil
}

func tokenize(s string) ([]string, error) {
	var toks []string
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n':
			i++
		case c == '(' || c == ')' || c == '!':
			toks = append(toks, string(c))
			i++
		case strings.HasPrefix(s[i:], "&&") || strings.HasPrefix(s[i:], "||"):
			toks = append(toks, s[i:i+2])
			i += 2
		case isRefChar(c):
			j := i
			for j < len(s) && (isRefChar(s[j]) || s[j] == '.') {
				j++
			}
			toks = append(toks, s[i:j])
			i = j
		default:
			return nil, fmt.Errorf("unexpected character %q in depends expression", c)
		}
	}
	if len(toks) == 0 {
		return nil, fmt.Errorf("empty depends expression")
	}
	return toks, nil
}

func isRefChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
}

type parser struct {
	toks []string
	pos  int
}

func (p *parser) peek() string {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return ""
}

func (p *parser) parseOr() (node, error) {
	l, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek() == "||" {
		p.pos++
		r, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		l = orNode{l, r}
	}
	return l, nil
}

func (p *parser) parseAnd() (node, error) {
	l, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.peek() == "&&" {
		p.pos++
		r, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		l = andNode{l, r}
	}
	return l, nil
}

func (p *parser) parseUnary() (node, error) {
	switch tok := p.peek(); tok {
	case "!":
		p.pos++
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return notNode{x}, nil
	case "(":
		p.pos++
		x, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.peek() != ")" {
			return nil, fmt.Errorf("missing ')' in depends expression")
		}
		p.pos++
		return x, nil
	case "", ")", "&&", "||":
		return nil, fmt.Errorf("expected a task name in depends expression, got %q", tok)
	default:
		p.pos++
		return parseRef(tok)
	}
}

func parseRef(tok string) (node, error) {
	task, result, hasResult := strings.Cut(tok, ".")
	if task == "" {
		return nil, fmt.Errorf("missing task name in %q", tok)
	}
	if !hasResult {
		return refNode{Ref{Task: task}}, nil
	}
	if !knownResults[Result(result)] {
		return nil, fmt.Errorf("unknown task result %q in %q", result, tok)
	}
	return refNode{Ref{Task: task, Result: Result(result)}}, nil
}
