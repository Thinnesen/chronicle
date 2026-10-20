package engine

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/expr"
)

// maxItems is the largest fan-out. It is also the CRD limit of withItems.
const maxItems = expr.MaxItems

// runGroup runs a task with withItems or withParam. The task group node has one child for each item.
func (b *boundary) runGroup(t *task) {
	id := b.e.nodeID(t.nodeName)
	g, ok := b.e.nodes[id]
	if !ok {
		g = b.e.newNode(call{template: t.call.Template, nodeName: t.nodeName}, v1alpha1.NodeTaskGroup)
		g.StartedAt = &b.e.in.Now
		b.e.setPhase(&g, v1alpha1.NodeRunning, "")
		b.e.save(g)
		b.link(t, b.nodeID, id)
	}
	items, err := b.items(t)
	if err != nil {
		b.e.setPhase(&g, v1alpha1.NodeError, err.Error())
		b.e.save(g)
		return
	}
	for i := range items {
		b.runOne(t, itemNodeName(t.nodeName, i, items[i]), &items[i], id)
	}
	g = b.e.nodes[id]
	if p, done := b.groupPhase(g); done {
		b.e.setPhase(&g, p, "")
		b.e.save(g)
	}
}

// items returns the withItems list, the numbers of withSequence, or the parsed JSON list of withParam.
func (b *boundary) items(t *task) ([]apiextensionsv1.JSON, error) {
	if len(t.call.WithItems) > 0 {
		return t.call.WithItems, nil
	}
	if seq := t.call.WithSequence; seq != nil {
		return sequenceItems(seq, b.scope(nil))
	}
	s, err := expr.Substitute(t.call.WithParam, b.scope(nil))
	if err != nil {
		return nil, fmt.Errorf("withParam: %w", err)
	}
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, fmt.Errorf("withParam is not a JSON list: %w", err)
	}
	if len(raw) > maxItems {
		return nil, fmt.Errorf("withParam has %d items, more than the limit of %d", len(raw), maxItems)
	}
	items := make([]apiextensionsv1.JSON, len(raw))
	for i := range raw {
		items[i] = apiextensionsv1.JSON{Raw: raw[i]}
	}
	return items, nil
}

// sequenceItems returns the items of a withSequence, with its placeholders resolved.
func sequenceItems(seq *v1alpha1.Sequence, resolve expr.Resolver) ([]apiextensionsv1.JSON, error) {
	number := func(field string, v *intstr.IntOrString) (int64, error) {
		if v == nil {
			return 0, nil
		}
		if v.Type == intstr.Int {
			return int64(v.IntVal), nil
		}
		text, err := expr.Substitute(v.StrVal, resolve)
		if err != nil {
			return 0, fmt.Errorf("withSequence.%s: %w", field, err)
		}
		n, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("withSequence.%s: %q is not a whole number", field, text)
		}
		return n, nil
	}
	start, err := number("start", seq.Start)
	if err != nil {
		return nil, err
	}
	end, err := number("end", seq.End)
	if seq.Count != nil {
		var count int64
		count, err = number("count", seq.Count)
		end = start + count - 1
	}
	if err != nil {
		return nil, err
	}
	texts, err := expr.Sequence(start, end, seq.Format)
	if err != nil {
		return nil, fmt.Errorf("withSequence: %w", err)
	}
	items := make([]apiextensionsv1.JSON, len(texts))
	for i, text := range texts {
		raw, _ := json.Marshal(text) // A string always encodes.
		items[i] = apiextensionsv1.JSON{Raw: raw}
	}
	return items, nil
}

// itemNodeName returns the node name of an item, for example "wf.a(0:x)".
func itemNodeName(base string, i int, item apiextensionsv1.JSON) string {
	v, _ := itemValue(&item, "item")
	v = strings.Map(func(r rune) rune {
		if r < ' ' || strings.ContainsRune(".[]()", r) {
			return '_'
		}
		return r
	}, v)
	if len(v) > 32 {
		v = v[:32]
	}
	return fmt.Sprintf("%s(%d:%s)", base, i, v)
}

// groupPhase returns the phase of a task group when all items finished.
// An empty group or a group where all items were skipped is Skipped.
func (b *boundary) groupPhase(g v1alpha1.NodeStatus) (v1alpha1.NodePhase, bool) {
	phase, skipped := v1alpha1.NodeSucceeded, true
	for _, id := range g.Children {
		p := b.e.nodes[id].Phase
		if !isTerminal(p) {
			return v1alpha1.NodeRunning, false
		}
		skipped = skipped && p == v1alpha1.NodeSkipped
		phase = worse(phase, p)
	}
	if skipped {
		return v1alpha1.NodeSkipped, true
	}
	return phase, true
}

func (b *boundary) anyChild(n v1alpha1.NodeStatus, p v1alpha1.NodePhase) bool {
	if n.Type != v1alpha1.NodeTaskGroup {
		return n.Phase == p
	}
	for _, id := range n.Children {
		if b.e.nodes[id].Phase == p {
			return true
		}
	}
	return false
}

func (b *boundary) allChildren(n v1alpha1.NodeStatus, p v1alpha1.NodePhase) bool {
	if n.Type != v1alpha1.NodeTaskGroup {
		return n.Phase == p
	}
	for _, id := range n.Children {
		if b.e.nodes[id].Phase != p {
			return false
		}
	}
	return len(n.Children) > 0
}

// scope returns a resolver for the placeholders of a task: globals, the boundary inputs,
// the outputs of finished tasks and the item.
func (b *boundary) scope(item *apiextensionsv1.JSON) expr.Resolver {
	base := b.e.inputScope(b.inputs)
	return func(name string) (string, bool) {
		if item != nil && (name == "item" || strings.HasPrefix(name, "item.")) {
			return itemValue(item, name)
		}
		if rest, ok := strings.CutPrefix(name, b.prefix+"."); ok {
			task, key, _ := strings.Cut(rest, ".")
			if _, known := b.byName[task]; known {
				n, ok := b.node(task)
				if !ok || !isTerminal(n.Phase) {
					return "", false
				}
				return b.e.nodeOutput(n, key)
			}
		}
		return base(name)
	}
}
