package engine

import (
	"cmp"
	"slices"
	"strings"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// waitingForSlot starts the message of a Pod node that waits for a free slot of a parallelism limit.
const waitingForSlot = "waiting for a free slot"

// noSlot tells if a new Pod for node n would exceed a parallelism limit: of the workflow, or of a DAG or steps node
// above n. It returns the first full limit, the workflow first and then from the inside out.
func (e *engine) noSlot(n v1alpha1.NodeStatus) (int32, bool) {
	type scope struct {
		root  string // the name of the node; "" is the whole workflow
		limit int32
	}
	var scopes []scope
	if p := e.in.Spec.Parallelism; p != nil {
		scopes = append(scopes, scope{"", *p})
	}
	var inner []scope
	for _, m := range e.nodes {
		if t := e.templates[m.TemplateName]; t != nil && t.Parallelism != nil && (t.DAG != nil || t.Steps != nil) && below(n.Name, m.Name) {
			inner = append(inner, scope{m.Name, *t.Parallelism})
		}
	}
	slices.SortFunc(inner, func(a, b scope) int { return cmp.Compare(len(b.root), len(a.root)) })
	for _, sc := range append(scopes, inner...) {
		if e.slotHolders(sc.root, n.ID) >= int(sc.limit) {
			return sc.limit, true
		}
	}
	return 0, false
}

// slotHolders counts the unfinished Pod nodes below root, other than except, that hold a slot: their Pod exists,
// or the engine asked for it in an earlier step. A Pod that the cache does not show yet thus still counts.
// For the whole workflow, an unfinished Pod without a node in the status counts too: the status can be older than the
// Pods, for example after a failed status update.
func (e *engine) slotHolders(root, except string) int {
	count := 0
	known := make(map[string]bool, len(e.nodes))
	for _, m := range e.nodes {
		known[m.PodName] = true
		if m.Type != v1alpha1.NodePod || m.ID == except || isTerminal(m.Phase) || !below(m.Name, root) {
			continue
		}
		if _, observed := e.in.Pods[m.PodName]; observed || !strings.HasPrefix(m.Message, waitingForSlot) {
			count++
		}
	}
	if root == "" {
		for name, p := range e.in.Pods {
			if !known[name] && !isTerminal(p.Phase) {
				count++
			}
		}
	}
	return count
}

// below tells if the node with the name is below the node root. Node names grow from their parent: a DAG task is
// "<root>.<task>", a step "<root>[<group>].<step>", an item or an attempt "<root>(...)". Every node is below "".
func below(name, root string) bool {
	if root == "" {
		return true
	}
	return len(name) > len(root) && strings.HasPrefix(name, root) && strings.ContainsRune(".[(", rune(name[len(root)]))
}
