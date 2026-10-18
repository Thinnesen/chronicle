package engine

import (
	"fmt"
	"hash/fnv"
	"slices"
	"strings"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// transitions lists the permitted node phase changes. Terminal phases have no outbound transitions.
var transitions = map[v1alpha1.NodePhase][]v1alpha1.NodePhase{
	"": {v1alpha1.NodePending, v1alpha1.NodeRunning, v1alpha1.NodeSucceeded, v1alpha1.NodeFailed,
		v1alpha1.NodeError, v1alpha1.NodeSkipped, v1alpha1.NodeOmitted},
	v1alpha1.NodePending: {v1alpha1.NodeRunning, v1alpha1.NodeSucceeded, v1alpha1.NodeFailed, v1alpha1.NodeError},
	// A step group is Omitted when all its steps are omitted. A task group is Skipped when all its items are skipped.
	v1alpha1.NodeRunning: {v1alpha1.NodeSucceeded, v1alpha1.NodeFailed, v1alpha1.NodeError, v1alpha1.NodeOmitted, v1alpha1.NodeSkipped},
}

func isTerminal(p v1alpha1.NodePhase) bool {
	switch p {
	case v1alpha1.NodeSucceeded, v1alpha1.NodeFailed, v1alpha1.NodeError, v1alpha1.NodeSkipped, v1alpha1.NodeOmitted:
		return true
	}
	return false
}

func isFailure(p v1alpha1.NodePhase) bool {
	return p == v1alpha1.NodeFailed || p == v1alpha1.NodeError
}

// worse returns the worse of two outcomes: Error, then Failed, then Succeeded.
func worse(a, b v1alpha1.NodePhase) v1alpha1.NodePhase {
	rank := func(p v1alpha1.NodePhase) int {
		switch p {
		case v1alpha1.NodeError:
			return 2
		case v1alpha1.NodeFailed:
			return 1
		}
		return 0
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
}

// setPhase changes the phase of a node if the transition is permitted, and sets the finish time.
func (e *engine) setPhase(n *v1alpha1.NodeStatus, p v1alpha1.NodePhase, msg string) {
	if n.Phase == p || !slices.Contains(transitions[n.Phase], p) {
		return
	}
	n.Phase = p
	n.Message = msg
	if isTerminal(p) && n.FinishedAt == nil {
		n.FinishedAt = &e.in.Now
	}
}

// nodeID returns a stable ID for a node name. The root node has the name of the workflow as its ID.
// Other IDs are the workflow name and a hash of the node name, so that they are valid Pod names.
func (e *engine) nodeID(name string) string {
	if name == e.in.Name {
		return name
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	prefix := e.in.Name
	if len(prefix) > 40 {
		prefix = strings.TrimRight(prefix[:40], "-.")
	}
	return fmt.Sprintf("%s-%d", prefix, h.Sum32())
}

func (e *engine) newNode(c call, typ v1alpha1.NodeType) v1alpha1.NodeStatus {
	name := c.nodeName
	display := name
	if i := strings.LastIndexAny(name, ".]"); i >= 0 && name != e.in.Name {
		display = name[i+1:]
	}
	return v1alpha1.NodeStatus{
		ID: e.nodeID(name), Name: name, DisplayName: display, Type: typ, TemplateName: c.template,
	}
}

func (e *engine) save(n v1alpha1.NodeStatus) v1alpha1.NodeStatus {
	e.nodes[n.ID] = n
	return n
}

func (e *engine) addChild(parentID, childID string) {
	p := e.nodes[parentID]
	if !slices.Contains(p.Children, childID) {
		p.Children = append(p.Children, childID)
		e.nodes[parentID] = p
	}
}
