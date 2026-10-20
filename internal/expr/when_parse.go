package expr

import (
	"fmt"
	"strings"
)

type wkind int

const (
	wkOp wkind = iota
	wkText
	wkPlaceholder
)

type wtok struct {
	kind wkind
	text string
}

var whenOps = []string{"&&", "||", "==", "!=", "=~", "!~", "<=", ">=", "<", ">", "!", "(", ")"}

func whenTokens(s string) ([]wtok, error) {
	var toks []wtok
	for i := 0; i < len(s); {
		switch c := s[i]; {
		case c == ' ' || c == '\t' || c == '\n':
			i++
		case strings.HasPrefix(s[i:], "{{"):
			end := strings.Index(s[i:], "}}")
			if end < 0 {
				return nil, fmt.Errorf("missing '}}' in when condition")
			}
			name := strings.TrimSpace(s[i+2 : i+end])
			if name == "" || strings.Contains(name, "{{") {
				return nil, fmt.Errorf("bad placeholder in when condition")
			}
			toks = append(toks, wtok{wkPlaceholder, name})
			i += end + 2
		case c == '\'' || c == '"':
			end := strings.IndexByte(s[i+1:], c)
			if end < 0 {
				return nil, fmt.Errorf("missing closing quote in when condition")
			}
			if _, err := Placeholders(s[i+1 : i+1+end]); err != nil {
				return nil, err
			}
			toks = append(toks, wtok{wkText, s[i+1 : i+1+end]})
			i += end + 2
		case isWordChar(c):
			j := i
			for j < len(s) && isWordChar(s[j]) {
				j++
			}
			toks = append(toks, wtok{wkText, s[i:j]})
			i = j
		default:
			op := matchOp(s[i:])
			if op == "" {
				return nil, fmt.Errorf("unexpected character %q in when condition", c)
			}
			toks = append(toks, wtok{wkOp, op})
			i += len(op)
		}
	}
	if len(toks) == 0 {
		return nil, fmt.Errorf("empty when condition")
	}
	return toks, nil
}

func matchOp(s string) string {
	for _, op := range whenOps {
		if strings.HasPrefix(s, op) {
			return op
		}
	}
	return ""
}

func isWordChar(c byte) bool {
	return isRefChar(c) || c == '.' || c == ':' || c == '/' || c == '+'
}

type wparser struct {
	toks []wtok
	pos  int
}

func (p *wparser) peekOp(ops ...string) string {
	if p.pos < len(p.toks) && p.toks[p.pos].kind == wkOp {
		for _, op := range ops {
			if p.toks[p.pos].text == op {
				return op
			}
		}
	}
	return ""
}

func (p *wparser) or() (wnode, error) {
	l, err := p.and()
	for err == nil && p.peekOp("||") != "" {
		p.pos++
		var r wnode
		if r, err = p.and(); err == nil {
			l = wOr{l, r}
		}
	}
	return l, err
}

func (p *wparser) and() (wnode, error) {
	l, err := p.not()
	for err == nil && p.peekOp("&&") != "" {
		p.pos++
		var r wnode
		if r, err = p.not(); err == nil {
			l = wAnd{l, r}
		}
	}
	return l, err
}

func (p *wparser) not() (wnode, error) {
	if p.peekOp("!") != "" {
		p.pos++
		x, err := p.not()
		return wNot{x}, err
	}
	return p.compare()
}

func (p *wparser) compare() (wnode, error) {
	l, err := p.primary()
	if err != nil {
		return nil, err
	}
	if op := p.peekOp("==", "!=", "=~", "!~", "<=", ">=", "<", ">"); op != "" {
		p.pos++
		r, err := p.primary()
		return wCompare{op, l, r}, err
	}
	return l, nil
}

func (p *wparser) primary() (wnode, error) {
	if p.pos >= len(p.toks) {
		return nil, fmt.Errorf("unexpected end of when condition")
	}
	t := p.toks[p.pos]
	p.pos++
	switch {
	case t.kind == wkPlaceholder:
		return wPlaceholder{t.text}, nil
	case t.kind == wkText:
		return wText{t.text}, nil
	case t.text == "(":
		x, err := p.or()
		if err != nil {
			return nil, err
		}
		if p.peekOp(")") == "" {
			return nil, fmt.Errorf("missing ')' in when condition")
		}
		p.pos++
		return x, nil
	}
	return nil, fmt.Errorf("unexpected %q in when condition", t.text)
}
