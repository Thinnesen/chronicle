package cli

import (
	"context"
	"fmt"
	"io"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/gen/chronicle/v1/chroniclev1connect"
)

// controlCommands returns suspend, resume, stop and terminate.
func (a *app) controlCommands() []*cobra.Command {
	type call func(context.Context, chroniclev1connect.WorkflowServiceClient, string, string) error
	controls := []struct {
		use, short, done string
		do               call
	}{
		{"suspend", "Pause a run: no new node starts", "Suspended", func(ctx context.Context, c chroniclev1connect.WorkflowServiceClient, ns, n string) error {
			_, err := c.SuspendWorkflow(ctx, connect.NewRequest(&chroniclev1.SuspendWorkflowRequest{Namespace: ns, Name: n}))
			return err
		}},
		{"resume", "Continue a paused run", "Resumed", func(ctx context.Context, c chroniclev1connect.WorkflowServiceClient, ns, n string) error {
			_, err := c.ResumeWorkflow(ctx, connect.NewRequest(&chroniclev1.ResumeWorkflowRequest{Namespace: ns, Name: n}))
			return err
		}},
		{"stop", "Stop a run and run its exit handler", "Stopped", func(ctx context.Context, c chroniclev1connect.WorkflowServiceClient, ns, n string) error {
			_, err := c.StopWorkflow(ctx, connect.NewRequest(&chroniclev1.StopWorkflowRequest{Namespace: ns, Name: n}))
			return err
		}},
		{"terminate", "Stop a run at once, without its exit handler", "Terminated", func(ctx context.Context, c chroniclev1connect.WorkflowServiceClient, ns, n string) error {
			_, err := c.TerminateWorkflow(ctx, connect.NewRequest(&chroniclev1.TerminateWorkflowRequest{Namespace: ns, Name: n}))
			return err
		}},
	}
	cmds := make([]*cobra.Command, 0, len(controls))
	for _, ctl := range controls {
		cmds = append(cmds, &cobra.Command{
			Use:   ctl.use + " <name>",
			Short: ctl.short,
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return a.onWorkflow(cmd, args[0], func(ctx context.Context, c chroniclev1connect.WorkflowServiceClient, ns string) error {
					if err := ctl.do(ctx, c, ns, args[0]); err != nil {
						return err
					}
					return done(a.out, ctl.done, ns, args[0])
				})
			},
		})
	}
	return cmds
}

func (a *app) onWorkflow(cmd *cobra.Command, _ string, f func(context.Context, chroniclev1connect.WorkflowServiceClient, string) error) error {
	ns, err := a.ns()
	if err != nil {
		return err
	}
	ctx, cancel := timeout(cmd)
	defer cancel()
	c, err := a.workflows(ctx)
	if err != nil {
		return err
	}
	return f(ctx, c, ns)
}

func done(out io.Writer, verb, ns, name string) error {
	_, err := fmt.Fprintf(out, "%s %s/%s\n", verb, ns, name)
	return err
}

func (a *app) deleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a run and its Pods",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.onWorkflow(cmd, args[0], func(ctx context.Context, c chroniclev1connect.WorkflowServiceClient, ns string) error {
				if _, err := c.DeleteWorkflow(ctx, connect.NewRequest(&chroniclev1.DeleteWorkflowRequest{Namespace: ns, Name: args[0]})); err != nil {
					return err
				}
				return done(a.out, "Deleted", ns, args[0])
			})
		},
	}
}

func (a *app) approveCommand() *cobra.Command {
	var reject bool
	var params []string
	var message string
	cmd := &cobra.Command{
		Use:     "approve <name> <node>",
		Short:   "Approve or reject a suspend node",
		Example: "  chronicle approve deploy-x7k2p deploy-x7k2p[0].gate -p env=prod -m 'checked by ops'",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := keyValues(params)
			if err != nil {
				return err
			}
			return a.onWorkflow(cmd, args[0], func(ctx context.Context, c chroniclev1connect.WorkflowServiceClient, ns string) error {
				_, err := c.ApproveNode(ctx, connect.NewRequest(&chroniclev1.ApproveNodeRequest{
					Namespace: ns, Name: args[0], Node: args[1], Approve: !reject, Parameters: p, Message: message}))
				if err != nil {
					return err
				}
				verb := "Approved"
				if reject {
					verb = "Rejected"
				}
				return done(a.out, verb+" "+args[1]+" in", ns, args[0])
			})
		},
	}
	cmd.Flags().BoolVar(&reject, "reject", false, "Reject the node. The node fails.")
	cmd.Flags().StringArrayVarP(&params, "parameter", "p", nil, "An output parameter of the suspend node as name=value.")
	cmd.Flags().StringVarP(&message, "message", "m", "", "A comment for the run history.")
	return cmd
}

func (a *app) namespacesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "namespaces",
		Short: "List the namespaces that you can use",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			url, err := a.serverURL()
			if err != nil {
				return err
			}
			hc, err := a.httpClient(cmd.Context())
			if err != nil {
				return err
			}
			resp, err := chroniclev1connect.NewNamespaceServiceClient(hc, url).ListNamespaces(cmd.Context(), connect.NewRequest(&chroniclev1.ListNamespacesRequest{}))
			if err != nil {
				return err
			}
			for _, n := range resp.Msg.Namespaces {
				access := "view"
				if n.CanSubmit {
					access = "submit"
				}
				_, _ = fmt.Fprintf(a.out, "%s\t%s\n", n.Name, access)
			}
			return nil
		},
	}
}
