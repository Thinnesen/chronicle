package cli

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/timestamppb"

	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/gen/chronicle/v1/chroniclev1connect"
)

func (a *app) backfillCommand() *cobra.Command {
	var from, to string
	cmd := &cobra.Command{
		Use:   "backfill <name> --from <time> --to <time>",
		Short: "Start the runs of the schedule times in a range, for example for days that the schedule missed",
		Long: "Start the runs of the schedule times from --from to --to, both included. A time is RFC 3339 or a date " +
			"(a date means the whole day in UTC). A time that already has its run is skipped. A range can have at most 100 times.",
		Example: "  chronicle cron backfill nightly --from 2026-10-01 --to 2026-10-07",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			start, err := parseTime(from, false)
			if err != nil {
				return fmt.Errorf("--from: %w", err)
			}
			end, err := parseTime(to, true)
			if err != nil {
				return fmt.Errorf("--to: %w", err)
			}
			return a.onCron(cmd, func(ctx context.Context, c chroniclev1connect.CronServiceClient, ns string) error {
				resp, err := c.BackfillCronWorkflow(ctx, connect.NewRequest(&chroniclev1.BackfillCronWorkflowRequest{
					Namespace: ns, Name: args[0], From: timestamppb.New(start), To: timestamppb.New(end)}))
				if err != nil {
					return err
				}
				for _, name := range resp.Msg.Created {
					_, _ = fmt.Fprintf(a.out, "Started %s/%s\n", ns, name)
				}
				for _, name := range resp.Msg.Existing {
					_, _ = fmt.Fprintf(a.out, "%s already exists\n", name)
				}
				if len(resp.Msg.Created)+len(resp.Msg.Existing) == 0 {
					_, _ = fmt.Fprintln(a.out, "The range has no schedule time.")
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "The first time of the range.")
	cmd.Flags().StringVar(&to, "to", "", "The last time of the range.")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

// parseTime reads an RFC 3339 time or a date. A date is the start of the day in UTC, or its end when end is true.
func parseTime(s string, end bool) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q is not a date (2026-10-01) or an RFC 3339 time", s)
	}
	if end {
		return d.Add(24*time.Hour - time.Second), nil
	}
	return d, nil
}
