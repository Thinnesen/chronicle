package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

// pollInterval is the wait between two reads of a watched run. Tests make it short.
var pollInterval = 2 * time.Second

// errRunFailed makes the CLI exit with an error when a watched run fails, for use in scripts and CI.
var errRunFailed = errors.New("the run did not succeed")

func (a *app) watchCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "watch <name>",
		Short: "Show the node changes of a run until it ends",
		Long:  "Show the node changes of a run until it ends. The exit code is 1 when the run does not succeed.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, err := a.ns()
			if err != nil {
				return err
			}
			return a.watch(cmd.Context(), ns, args[0])
		},
	}
}

// watch prints each node phase change and returns when the run has finished.
func (a *app) watch(ctx context.Context, ns, name string) error {
	c, err := a.workflows(ctx)
	if err != nil {
		return err
	}
	tracker := newChangeTracker()
	for {
		resp, err := c.GetWorkflow(ctx, connect.NewRequest(&chroniclev1.GetWorkflowRequest{Namespace: ns, Name: name}))
		if err != nil && !healable(err) {
			return err
		}
		if err != nil {
			// The server restarts, for example: the run goes on, so look again after the next interval.
			_, _ = fmt.Fprintln(a.errOut, "Could not read the run, trying again:", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(pollInterval):
			}
			continue
		}
		changed := tracker.changes(nodes(resp.Msg))
		slices.SortStableFunc(changed, changeOrder)
		for _, n := range changed {
			_, _ = fmt.Fprintf(a.out, "%s  %-10s %s%s\n", time.Now().Format(time.TimeOnly), n.Phase, n.Name, suffix(n.Message))
		}
		s := resp.Msg.Summary
		if finished(s.Phase) {
			_, _ = fmt.Fprintf(a.out, "%s %s%s\n", s.Name, s.Phase, suffix(s.Message))
			if s.Phase != string(v1alpha1.WorkflowSucceeded) {
				return errRunFailed
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// changeTracker finds the node changes that are news for the user: a new phase, or a new reason why a pending node
// waits, for example ErrImagePull. Kubernetes switches between some reasons on each retry, so each one comes once.
type changeTracker struct {
	phase   map[string]v1alpha1.NodePhase
	reasons map[string]bool
}

func newChangeTracker() *changeTracker {
	return &changeTracker{phase: map[string]v1alpha1.NodePhase{}, reasons: map[string]bool{}}
}

func (t *changeTracker) changes(all []v1alpha1.NodeStatus) []v1alpha1.NodeStatus {
	var out []v1alpha1.NodeStatus
	for _, n := range all {
		news := n.Phase != "" && t.phase[n.ID] != n.Phase
		if reason, _, ok := strings.Cut(n.Message, ": "); ok && n.Phase == v1alpha1.NodePending && !t.reasons[n.ID+"/"+reason] {
			t.reasons[n.ID+"/"+reason] = true
			news = true
		}
		if news {
			t.phase[n.ID] = n.Phase
			out = append(out, n)
		}
	}
	return out
}

// changeOrder sorts the node changes of one poll by the time of the change. At the same time, an end comes
// before a start, a child ends before its parent, and a parent starts before its child.
func changeOrder(a, b v1alpha1.NodeStatus) int {
	at, aEnd := changeTime(a)
	bt, bEnd := changeTime(b)
	if c := at.Compare(bt); c != 0 {
		return c
	}
	switch {
	case aEnd && !bEnd:
		return -1
	case !aEnd && bEnd:
		return 1
	case aEnd:
		return cmp.Compare(len(b.Name), len(a.Name))
	}
	return cmp.Compare(len(a.Name), len(b.Name))
}

func changeTime(n v1alpha1.NodeStatus) (time.Time, bool) {
	if n.FinishedAt != nil {
		return n.FinishedAt.Time, true
	}
	if n.StartedAt != nil {
		return n.StartedAt.Time, false
	}
	return time.Time{}, false
}

func finished(phase string) bool {
	return phase == "Succeeded" || phase == "Failed" || phase == "Error"
}

func suffix(msg string) string {
	if msg == "" {
		return ""
	}
	return ": " + msg
}

func readAllLimited(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, 4<<20))
}
