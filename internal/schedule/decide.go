package schedule

import (
	"cmp"
	"slices"
	"time"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// Plan is what the controller must do for a CronWorkflow now.
type Plan struct {
	// Run is the schedule time of a new run, or nil.
	Run *time.Time
	// Due is the schedule time that this plan handles (started or skipped), or nil.
	Due *time.Time
	// Skipped tells why a due run did not start, or "".
	Skipped string
	// Terminate are the names of the active runs to terminate (Replace policy).
	Terminate []string
	// Delete are the names of the finished runs to delete (history limits).
	Delete []string
	// Active are the runs that did not finish.
	Active []v1alpha1.Workflow
	// Next is the next schedule time.
	Next time.Time
}

// Decide returns the plan for a CronWorkflow and its runs at the time now.
func Decide(cw *v1alpha1.CronWorkflow, runs []v1alpha1.Workflow, now time.Time) (Plan, error) {
	s, err := Parse(cw.Spec.Schedules, cw.Spec.Timezone)
	if err != nil {
		return Plan{}, err
	}
	p := Plan{Next: s.Next(now)}
	var finished []v1alpha1.Workflow
	for _, wf := range runs {
		if isFinished(wf.Status.Phase) {
			finished = append(finished, wf)
		} else {
			p.Active = append(p.Active, wf)
		}
	}
	p.Delete = overLimit(finished, cw.Spec)
	since := cw.CreationTimestamp.Time
	if cw.Status.LastScheduledTime != nil {
		since = cw.Status.LastScheduledTime.Time
	}
	due, ok := s.LastDue(since, now)
	if !ok {
		return p, nil
	}
	p.Due = &due
	switch d := cw.Spec.StartingDeadlineSeconds; {
	case cw.Spec.Suspend:
		p.Skipped = "the CronWorkflow is suspended"
	case d != nil && now.Sub(due) > time.Duration(*d)*time.Second:
		p.Skipped = "the starting deadline was missed"
	case len(p.Active) > 0 && cw.Spec.ConcurrencyPolicy == v1alpha1.ConcurrencyForbid:
		p.Skipped = "an earlier run is still active"
	default:
		p.Run = &due
		if cw.Spec.ConcurrencyPolicy == v1alpha1.ConcurrencyReplace {
			for _, wf := range p.Active {
				p.Terminate = append(p.Terminate, wf.Name)
			}
		}
	}
	return p, nil
}

func isFinished(p v1alpha1.WorkflowPhase) bool {
	return p == v1alpha1.WorkflowSucceeded || p == v1alpha1.WorkflowFailed || p == v1alpha1.WorkflowError
}

// overLimit returns the finished runs beyond the history limits, oldest first.
func overLimit(finished []v1alpha1.Workflow, spec v1alpha1.CronWorkflowSpec) []string {
	keepOK, keepFailed := int32(3), int32(1)
	if spec.SuccessfulJobsHistoryLimit != nil {
		keepOK = *spec.SuccessfulJobsHistoryLimit
	}
	if spec.FailedJobsHistoryLimit != nil {
		keepFailed = *spec.FailedJobsHistoryLimit
	}
	slices.SortFunc(finished, func(a, b v1alpha1.Workflow) int {
		return cmp.Or(b.CreationTimestamp.Compare(a.CreationTimestamp.Time), cmp.Compare(a.Name, b.Name))
	})
	var del []string
	var ok, failed int32
	for _, wf := range finished {
		if wf.Status.Phase == v1alpha1.WorkflowSucceeded {
			if ok++; ok > keepOK {
				del = append(del, wf.Name)
			}
		} else if failed++; failed > keepFailed {
			del = append(del, wf.Name)
		}
	}
	slices.Reverse(del)
	return del
}
