package cli

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/gen/chronicle/v1/chroniclev1connect"
)

func (a *app) logsCommand() *cobra.Command {
	var node string
	var follow bool
	var tail int64
	cmd := &cobra.Command{
		Use:     "logs <name>",
		Short:   "Print the logs of a run or of one node",
		Example: "  chronicle logs build-x7k2p -f\n  chronicle logs build-x7k2p --node build-x7k2p.test --tail 100",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, err := a.ns()
			if err != nil {
				return err
			}
			url, err := a.serverURL()
			if err != nil {
				return err
			}
			hc, err := a.httpClient(cmd.Context())
			if err != nil {
				return err
			}
			client := chroniclev1connect.NewLogServiceClient(hc, url)
			req := &chroniclev1.StreamLogsRequest{Namespace: ns, Workflow: args[0], Node: node, Follow: follow, TailLines: tail}
			// printed counts the lines of each node. After a reconnect the log comes again from the start, so the
			// command skips the lines that it printed before.
			printed := map[string]int{}
			for {
				err := a.streamLogs(cmd.Context(), client, req, node != "", printed)
				// With --tail, a new stream starts at another line, so the command cannot skip the printed lines.
				if err == nil || !follow || tail > 0 || !healable(err) {
					return err
				}
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "The log stream stopped. Connecting again…")
				select {
				case <-cmd.Context().Done():
					return cmd.Context().Err()
				case <-time.After(reconnectWait):
				}
			}
		},
	}
	cmd.Flags().StringVar(&node, "node", "", "The ID or the name of one node. The default is all Pod nodes.")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Keep printing new lines.")
	cmd.Flags().Int64Var(&tail, "tail", 0, "Print only the last lines of each Pod.")
	return cmd
}

// reconnectWait is the wait before a followed log connects again. Tests make it short.
var reconnectWait = 2 * time.Second

// streamLogs prints one log stream. It skips the lines of each node that printed already has.
func (a *app) streamLogs(ctx context.Context, client chroniclev1connect.LogServiceClient, req *chroniclev1.StreamLogsRequest, oneNode bool, printed map[string]int) error {
	stream, err := client.StreamLogs(ctx, connect.NewRequest(req))
	if err != nil {
		return err
	}
	seen := map[string]int{}
	for stream.Receive() {
		m := stream.Msg()
		seen[m.NodeId+m.NodeName]++
		if seen[m.NodeId+m.NodeName] <= printed[m.NodeId+m.NodeName] {
			continue
		}
		printed[m.NodeId+m.NodeName]++
		if oneNode {
			_, _ = fmt.Fprintln(a.out, m.Line)
			continue
		}
		_, _ = fmt.Fprintf(a.out, "%s: %s\n", m.NodeName, m.Line)
	}
	return stream.Err()
}

// healable tells if a stream error can go away by itself, such as a lost connection or a server that restarts.
func healable(err error) bool {
	switch connect.CodeOf(err) {
	case connect.CodeUnavailable, connect.CodeUnknown, connect.CodeInternal, connect.CodeDeadlineExceeded:
		return true
	}
	return false
}
