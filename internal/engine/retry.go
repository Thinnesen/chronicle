package engine

import (
	"fmt"
	"time"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// runRetry runs the attempts of a retry node. A new attempt starts when the last one failed and the policy permits a
// retry. Also, the limit must not be reached, and the backoff must be over.
func (e *engine) runRetry(c call, t *v1alpha1.Template, n v1alpha1.NodeStatus, depth int) v1alpha1.NodeStatus {
	rs := t.RetryStrategy
	var last v1alpha1.NodeStatus
	if len(n.Children) == 0 {
		last = e.runAttempt(c, n, 0, depth)
	} else if last = e.nodes[n.Children[len(n.Children)-1]]; !IsTerminal(last.Phase) {
		last = e.runAttempt(c, n, len(n.Children)-1, depth)
	}
	n = e.nodes[n.ID]
	if retryable(rs, last.Phase, len(n.Children)) && !e.in.Suspend && e.reached(last.FinishedAt.Add(backoff(rs, len(n.Children)))) {
		last = e.runAttempt(c, n, len(n.Children), depth)
	}
	n = e.nodes[n.ID]
	if IsTerminal(last.Phase) && !retryable(rs, last.Phase, len(n.Children)) {
		n.Outputs = last.Outputs
		msg := last.Message
		switch {
		case len(n.Children) > 1 && msg == "":
			msg = fmt.Sprintf("after %d attempts", len(n.Children))
		case len(n.Children) > 1:
			msg = fmt.Sprintf("%s (after %d attempts)", msg, len(n.Children))
		}
		e.setPhase(&n, last.Phase, msg)
	}
	return e.save(n)
}

func (e *engine) runAttempt(c call, n v1alpha1.NodeStatus, i, depth int) v1alpha1.NodeStatus {
	args, artifacts := map[string]string{}, map[string]v1alpha1.Artifact{}
	if n.Inputs != nil {
		for _, p := range n.Inputs.Parameters {
			args[p.Name] = *p.Value
		}
		for _, a := range n.Inputs.Artifacts {
			artifacts[a.Name] = a
		}
	}
	a := e.execute(call{template: c.template, nodeName: fmt.Sprintf("%s(%d)", c.nodeName, i), args: args, artifacts: artifacts, attempt: true, retry: i}, depth)
	e.addChild(n.ID, a.ID)
	return a
}

// retryable tells if a node with this phase may start another attempt after the given number of attempts.
func retryable(rs *v1alpha1.RetryStrategy, p v1alpha1.NodePhase, attempts int) bool {
	if attempts > int(rs.Limit) {
		return false
	}
	switch rs.RetryPolicy {
	case v1alpha1.RetryAlways:
		return isFailure(p)
	case v1alpha1.RetryOnError:
		return p == v1alpha1.NodeError
	}
	return p == v1alpha1.NodeFailed
}

// backoff returns the wait before the next retry: duration * factor^(retry-1), at most maxDuration.
func backoff(rs *v1alpha1.RetryStrategy, retry int) time.Duration {
	if rs.Backoff == nil {
		return 0
	}
	d := rs.Backoff.Duration.Duration
	factor := time.Duration(1)
	if rs.Backoff.Factor != nil {
		factor = time.Duration(*rs.Backoff.Factor)
	}
	limit := 365 * 24 * time.Hour
	if rs.Backoff.MaxDuration != nil {
		limit = rs.Backoff.MaxDuration.Duration
	}
	for range retry - 1 {
		if d >= limit {
			break
		}
		d *= factor
	}
	return min(d, limit)
}
