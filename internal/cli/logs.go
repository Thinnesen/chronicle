package cli

import (
	"fmt"

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
			stream, err := chroniclev1connect.NewLogServiceClient(hc, url).StreamLogs(cmd.Context(), connect.NewRequest(&chroniclev1.StreamLogsRequest{
				Namespace: ns, Workflow: args[0], Node: node, Follow: follow, TailLines: tail}))
			if err != nil {
				return err
			}
			for stream.Receive() {
				m := stream.Msg()
				if node != "" {
					_, _ = fmt.Fprintln(a.out, m.Line)
					continue
				}
				_, _ = fmt.Fprintf(a.out, "%s: %s\n", m.NodeName, m.Line)
			}
			return stream.Err()
		},
	}
	cmd.Flags().StringVar(&node, "node", "", "The ID or the name of one node. The default is all Pod nodes.")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Keep printing new lines.")
	cmd.Flags().Int64Var(&tail, "tail", 0, "Print only the last lines of each Pod.")
	return cmd
}
