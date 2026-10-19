package cli

import (
	"context"
	"fmt"
	"io"
	"time"

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

func (a *app) retryCommand() *cobra.Command {
	var node string
	var watch bool
	cmd := &cobra.Command{
		Use:     "retry <name>",
		Short:   "Run the failed nodes of a finished run again, or a node and all nodes after it",
		Example: "  chronicle retry build-x7k2p --watch\n  chronicle retry build-x7k2p --node build-x7k2p.test",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, err := a.ns()
			if err != nil {
				return err
			}
			c, err := a.workflows(cmd.Context())
			if err != nil {
				return err
			}
			if _, err := c.RetryWorkflow(cmd.Context(), connect.NewRequest(&chroniclev1.RetryWorkflowRequest{Namespace: ns, Name: args[0], Node: node})); err != nil {
				return err
			}
			if err := done(a.out, "Retrying", ns, args[0]); err != nil || !watch {
				return err
			}
			return a.watchAfterRetry(cmd, c, ns, args[0])
		},
	}
	cmd.Flags().StringVar(&node, "node", "", "The ID or the name of the node to retry from.")
	cmd.Flags().BoolVarP(&watch, "watch", "w", false, "Watch the run until it ends.")
	return cmd
}

// watchAfterRetry waits until the controller has applied the retry, then watches the run.
func (a *app) watchAfterRetry(cmd *cobra.Command, c chroniclev1connect.WorkflowServiceClient, ns, name string) error {
	for {
		resp, err := c.GetWorkflow(cmd.Context(), connect.NewRequest(&chroniclev1.GetWorkflowRequest{Namespace: ns, Name: name}))
		if err != nil {
			return err
		}
		if !finished(resp.Msg.Summary.Phase) {
			return a.watch(cmd.Context(), ns, name)
		}
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		case <-time.After(pollInterval):
		}
	}
}

func (a *app) rerunCommand() *cobra.Command {
	var params []string
	var watch bool
	cmd := &cobra.Command{
		Use:     "rerun <name>",
		Short:   "Start a new run with the spec of a run, optionally with changed parameters",
		Example: "  chronicle rerun build-x7k2p -p version=1.4.3 --watch",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := keyValues(params)
			if err != nil {
				return err
			}
			ns, err := a.ns()
			if err != nil {
				return err
			}
			c, err := a.workflows(cmd.Context())
			if err != nil {
				return err
			}
			resp, err := c.RerunWorkflow(cmd.Context(), connect.NewRequest(&chroniclev1.RerunWorkflowRequest{Namespace: ns, Name: args[0], Parameters: p}))
			if err != nil {
				return err
			}
			name := resp.Msg.Summary.Name
			_, _ = fmt.Fprintf(a.out, "Started %s/%s\n", ns, name)
			if watch {
				return a.watch(cmd.Context(), ns, name)
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVarP(&params, "parameter", "p", nil, "A changed workflow parameter as name=value.")
	cmd.Flags().BoolVarP(&watch, "watch", "w", false, "Watch the new run until it ends.")
	return cmd
}
