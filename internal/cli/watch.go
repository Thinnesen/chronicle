package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

// pollInterval is the wait between two reads of a watched run.
const pollInterval = 2 * time.Second

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
	seen := map[string]v1alpha1.NodePhase{}
	for {
		resp, err := c.GetWorkflow(ctx, connect.NewRequest(&chroniclev1.GetWorkflowRequest{Namespace: ns, Name: name}))
		if err != nil {
			return err
		}
		for _, n := range nodes(resp.Msg) {
			if n.Phase != "" && seen[n.ID] != n.Phase {
				seen[n.ID] = n.Phase
				_, _ = fmt.Fprintf(a.out, "%s  %-10s %s%s\n", time.Now().Format(time.TimeOnly), n.Phase, n.Name, suffix(n.Message))
			}
		}
		s := resp.Msg.Summary
		if finished(s.Phase) {
			_, _ = fmt.Fprintf(a.out, "%s %s%s\n", s.Name, s.Phase, suffix(s.Message))
			if s.Phase != "Succeeded" {
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
