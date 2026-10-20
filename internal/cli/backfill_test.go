package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

func (s *stub) BackfillCronWorkflow(_ context.Context, r *connect.Request[chroniclev1.BackfillCronWorkflowRequest]) (*connect.Response[chroniclev1.BackfillCronWorkflowResponse], error) {
	s.requests = append(s.requests, r.Msg)
	return connect.NewResponse(&chroniclev1.BackfillCronWorkflowResponse{Created: []string{"nightly-1"}, Existing: []string{"nightly-2"}}), nil
}

func TestCronBackfill(t *testing.T) {
	s := &stub{}
	out, err := run(t, s, "cron", "backfill", "nightly", "--from", "2026-10-01", "--to", "2026-10-03")
	if err != nil || !strings.Contains(out, "Started team-a/nightly-1") || !strings.Contains(out, "nightly-2 already exists") {
		t.Fatalf("backfill: %v\n%s", err, out)
	}
	req := s.requests[0].(*chroniclev1.BackfillCronWorkflowRequest)
	// A date covers the whole day, in UTC.
	if !req.From.AsTime().Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) || !req.To.AsTime().Equal(time.Date(2026, 10, 3, 23, 59, 59, 0, time.UTC)) {
		t.Errorf("range %s to %s", req.From.AsTime(), req.To.AsTime())
	}
	if _, err := run(t, &stub{}, "cron", "backfill", "nightly", "--from", "yesterday", "--to", "2026-10-03"); err == nil {
		t.Error("a bad date gave no error")
	}
}
