package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/tabwriter"
	"time"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/gen/chronicle/v1/chroniclev1connect"
)

// connection returns the HTTP client with the token of the login, and the server URL.
func (a *app) connection(ctx context.Context) (*http.Client, string, error) {
	url, err := a.serverURL()
	if err != nil {
		return nil, "", err
	}
	hc, err := a.httpClient(ctx)
	return hc, url, err
}

func (a *app) templatesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "templates",
		Short: "List the templates of a namespace and the cluster templates",
		Long:  "List the templates of a namespace and the cluster templates. Start a run from one with chronicle submit --from <name>.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ns, err := a.ns()
			if err != nil {
				return err
			}
			hc, url, err := a.connection(cmd.Context())
			if err != nil {
				return err
			}
			resp, err := chroniclev1connect.NewTemplateServiceClient(hc, url).ListTemplates(cmd.Context(),
				connect.NewRequest(&chroniclev1.ListTemplatesRequest{Namespace: ns}))
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(a.out, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "NAME\tPARAMETERS\tMANAGED BY\tDESCRIPTION")
			for _, t := range resp.Msg.Templates {
				name := t.Name
				if t.ClusterScope {
					name = "cluster/" + name
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", name, cmpOr(parameterList(t.Parameters), "-"), cmpOr(t.ManagedBy, "-"), t.Description)
			}
			if err := w.Flush(); err != nil {
				return err
			}
			if resp.Msg.ClusterTemplatesHidden {
				_, _ = fmt.Fprintln(a.errOut, "You may not list the cluster templates, so the list leaves them out. Ask for get and list on clusterworkflowtemplates.")
			}
			return nil
		},
	}
}

// parameterList returns the parameter names, each with its default value if it has one.
func parameterList(params []*chroniclev1.TemplateParameter) string {
	parts := make([]string, 0, len(params))
	for _, p := range params {
		if p.HasDefault {
			parts = append(parts, p.Name+"="+p.Default)
		} else {
			parts = append(parts, p.Name)
		}
	}
	return strings.Join(parts, ", ")
}

func (a *app) cronCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cron",
		Short: "List, suspend, resume and backfill the schedules (CronWorkflows)",
	}
	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the schedules of a namespace",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.onCron(cmd, func(ctx context.Context, c chroniclev1connect.CronServiceClient, ns string) error {
				resp, err := c.ListCronWorkflows(ctx, connect.NewRequest(&chroniclev1.ListCronWorkflowsRequest{Namespace: ns}))
				if err != nil {
					return err
				}
				printCrons(a.out, resp.Msg.CronWorkflows)
				return nil
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "suspend <name>",
		Short: "Pause a schedule: no new run starts. Active runs continue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.onCron(cmd, func(ctx context.Context, c chroniclev1connect.CronServiceClient, ns string) error {
				if _, err := c.SuspendCronWorkflow(ctx, connect.NewRequest(&chroniclev1.SuspendCronWorkflowRequest{Namespace: ns, Name: args[0]})); err != nil {
					return err
				}
				return done(a.out, "Suspended", ns, args[0])
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "resume <name>",
		Short: "Start the runs of a suspended schedule again",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.onCron(cmd, func(ctx context.Context, c chroniclev1connect.CronServiceClient, ns string) error {
				if _, err := c.ResumeCronWorkflow(ctx, connect.NewRequest(&chroniclev1.ResumeCronWorkflowRequest{Namespace: ns, Name: args[0]})); err != nil {
					return err
				}
				return done(a.out, "Resumed", ns, args[0])
			})
		},
	})
	cmd.AddCommand(a.backfillCommand())
	return cmd
}

func (a *app) onCron(cmd *cobra.Command, f func(context.Context, chroniclev1connect.CronServiceClient, string) error) error {
	ns, err := a.ns()
	if err != nil {
		return err
	}
	ctx, cancel := timeout(cmd)
	defer cancel()
	hc, url, err := a.connection(ctx)
	if err != nil {
		return err
	}
	return f(ctx, chroniclev1connect.NewCronServiceClient(hc, url), ns)
}

func printCrons(out io.Writer, crons []*chroniclev1.CronSummary) {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tSCHEDULE\tNEXT\tLAST\tACTIVE\tTEMPLATE")
	for _, c := range crons {
		schedule := strings.Join(c.Schedules, ", ")
		if c.Timezone != "" {
			schedule += " (" + c.Timezone + ")"
		}
		next, last := "-", "-"
		if c.Suspend {
			next = "suspended"
		} else if c.NextScheduledAt != nil {
			next = "now" // the time has passed, and the controller starts the run
			if d := time.Until(c.NextScheduledAt.AsTime()).Round(time.Minute); d > 0 {
				next = "in " + strings.TrimSuffix(d.String(), "0s")
			}
		}
		if c.LastScheduledAt != nil {
			last = ago(c.LastScheduledAt.AsTime())
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n", c.Name, schedule, next, last, c.Active, cmpOr(c.Template, "-"))
	}
	_ = w.Flush()
}
