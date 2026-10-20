package cli

import (
	"fmt"
	"slices"
	"strings"
	"text/tabwriter"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/gen/chronicle/v1/chroniclev1connect"
)

// taskStates are the states of a task in the queue, for tasks --state.
var taskStates = []string{"pending", "running", "succeeded", "failed", "error", "cancelled"}

func (a *app) tasksCommand() *cobra.Command {
	var states []string
	cmd := &cobra.Command{
		Use:     "tasks",
		Short:   "List the tasks of the task workers in a namespace, newest first",
		Example: "  chronicle tasks --state pending,running",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			for i, st := range states {
				states[i] = strings.ToLower(st)
				if !slices.Contains(taskStates, states[i]) {
					return fmt.Errorf("%q is not a task state: use %s", st, strings.Join(taskStates, ", "))
				}
			}
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
			resp, err := chroniclev1connect.NewTaskServiceClient(hc, url).ListTasks(cmd.Context(),
				connect.NewRequest(&chroniclev1.ListTasksRequest{Namespace: ns, States: states}))
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(a.out, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "TASK\tQUEUE\tSTATE\tSTEP\tWORKER\tCREATED\tMESSAGE")
			for _, t := range resp.Msg.Tasks {
				created := "-"
				if t.CreatedAt != nil {
					created = ago(t.CreatedAt.AsTime())
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", t.Name, t.Queue, t.State, t.NodeName, cmpOr(t.Worker, "-"), created, t.Message)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringSliceVar(&states, "state", nil, "Show only tasks in these states: "+strings.Join(taskStates, ", ")+".")
	return cmd
}
