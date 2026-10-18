package schedule

import (
	"reflect"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

var t0 = time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)

func run(name string, phase v1alpha1.WorkflowPhase, created time.Time) v1alpha1.Workflow {
	return v1alpha1.Workflow{Name: name, CreationTimestamp: metav1.NewTime(created), Status: v1alpha1.WorkflowStatus{Phase: phase}}
}

func TestDecide(t *testing.T) {
	hourly := v1alpha1.CronWorkflowSpec{Schedules: []string{"0 * * * *"}}
	tests := []struct {
		name    string
		spec    func(*v1alpha1.CronWorkflowSpec)
		last    *time.Time
		runs    []v1alpha1.Workflow
		now     time.Time
		run     *time.Time
		skipped string
		term    []string
		del     []string
		next    time.Time
	}{
		{name: "not due yet", now: t0.Add(30 * time.Minute), next: t0.Add(time.Hour)},
		{name: "due", now: t0.Add(61 * time.Minute), run: new(t0.Add(time.Hour)), next: t0.Add(2 * time.Hour)},
		{name: "only the latest missed run starts", now: t0.Add(5*time.Hour + time.Minute), run: new(t0.Add(5 * time.Hour)), next: t0.Add(6 * time.Hour)},
		{name: "already handled", last: new(t0.Add(time.Hour)), now: t0.Add(90 * time.Minute), next: t0.Add(2 * time.Hour)},
		{name: "deadline missed", spec: func(s *v1alpha1.CronWorkflowSpec) { s.StartingDeadlineSeconds = ptr.To[int64](60) },
			now: t0.Add(65 * time.Minute), skipped: "deadline", next: t0.Add(2 * time.Hour)},
		{name: "suspended", spec: func(s *v1alpha1.CronWorkflowSpec) { s.Suspend = true },
			now: t0.Add(61 * time.Minute), skipped: "suspended", next: t0.Add(2 * time.Hour)},
		{name: "forbid", spec: func(s *v1alpha1.CronWorkflowSpec) { s.ConcurrencyPolicy = v1alpha1.ConcurrencyForbid },
			runs: []v1alpha1.Workflow{run("a", v1alpha1.WorkflowRunning, t0)}, now: t0.Add(61 * time.Minute), skipped: "still active", next: t0.Add(2 * time.Hour)},
		{name: "replace", spec: func(s *v1alpha1.CronWorkflowSpec) { s.ConcurrencyPolicy = v1alpha1.ConcurrencyReplace },
			runs: []v1alpha1.Workflow{run("a", v1alpha1.WorkflowRunning, t0)}, now: t0.Add(61 * time.Minute),
			run: new(t0.Add(time.Hour)), term: []string{"a"}, next: t0.Add(2 * time.Hour)},
		{name: "history", spec: func(s *v1alpha1.CronWorkflowSpec) { s.SuccessfulJobsHistoryLimit = ptr.To[int32](1) },
			runs: []v1alpha1.Workflow{
				run("ok1", v1alpha1.WorkflowSucceeded, t0.Add(-3*time.Hour)), run("ok2", v1alpha1.WorkflowSucceeded, t0.Add(-2*time.Hour)),
				run("ok3", v1alpha1.WorkflowSucceeded, t0.Add(-time.Hour)), run("bad1", v1alpha1.WorkflowFailed, t0.Add(-4*time.Hour)),
				run("bad2", v1alpha1.WorkflowError, t0.Add(-2*time.Hour)),
			},
			now: t0.Add(30 * time.Minute), del: []string{"bad1", "ok1", "ok2"}, next: t0.Add(time.Hour)},
		{name: "two schedules and a time zone", spec: func(s *v1alpha1.CronWorkflowSpec) {
			s.Schedules, s.Timezone = []string{"0 3 * * *", "30 1 * * *"}, "Europe/Berlin"
			// 03:00 in Berlin (summer time) is 01:00 UTC. The next run is at 01:30 in Berlin, 23:30 UTC.
		}, now: t0.Add(time.Hour), run: new(t0.Add(time.Hour)), next: time.Date(2026, 10, 18, 23, 30, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cw := &v1alpha1.CronWorkflow{CreationTimestamp: metav1.NewTime(t0), Spec: hourly}
			if tt.spec != nil {
				tt.spec(&cw.Spec)
			}
			if tt.last != nil {
				cw.Status.LastScheduledTime = &metav1.Time{Time: *tt.last}
			}
			p, err := Decide(cw, tt.runs, tt.now)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(p.Run, tt.run) || !strings.Contains(p.Skipped, tt.skipped) || tt.skipped == "" && p.Skipped != "" {
				t.Errorf("run %v, skipped %q", p.Run, p.Skipped)
			}
			if !reflect.DeepEqual(p.Terminate, tt.term) || !reflect.DeepEqual(p.Delete, tt.del) {
				t.Errorf("terminate %v, delete %v", p.Terminate, p.Delete)
			}
			if !p.Next.Equal(tt.next) {
				t.Errorf("next = %v, want %v", p.Next, tt.next)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	if _, err := Parse([]string{"61 * * * *"}, ""); err == nil {
		t.Error("a bad schedule was accepted")
	}
	if _, err := Parse([]string{"* * * * *"}, "Mars/Base"); err == nil {
		t.Error("a bad time zone was accepted")
	}
	s, _ := Parse([]string{"* * * * *"}, "")
	if _, ok := s.LastDue(t0.Add(-30*24*time.Hour), t0); ok {
		t.Error("a very old start time must not start a run")
	}
}
