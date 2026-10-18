// Package schedule decides when a CronWorkflow starts a run. It is pure: the caller gives the time.
package schedule

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// maxMissed limits the search for missed times. A controller that was down for longer starts only future runs.
const maxMissed = 1000

// Schedule is a set of parsed cron expressions in one time zone.
type Schedule struct {
	specs []cron.Schedule
}

// Parse parses cron expressions with five fields in a time zone. An empty time zone is UTC.
func Parse(exprs []string, timezone string) (*Schedule, error) {
	loc := time.UTC
	if timezone != "" {
		var err error
		if loc, err = time.LoadLocation(timezone); err != nil {
			return nil, fmt.Errorf("unknown time zone %q: %w", timezone, err)
		}
	}
	s := &Schedule{}
	for _, e := range exprs {
		spec, err := cron.ParseStandard(e)
		if err != nil {
			return nil, fmt.Errorf("bad schedule %q: %w", e, err)
		}
		if sp, ok := spec.(*cron.SpecSchedule); ok {
			sp.Location = loc
		}
		s.specs = append(s.specs, spec)
	}
	return s, nil
}

// Next returns the first schedule time after t.
func (s *Schedule) Next(t time.Time) time.Time {
	var next time.Time
	for _, spec := range s.specs {
		if n := spec.Next(t); next.IsZero() || n.Before(next) {
			next = n
		}
	}
	return next
}

// LastDue returns the latest schedule time in (since, now], or false when there is none.
func (s *Schedule) LastDue(since, now time.Time) (time.Time, bool) {
	var due time.Time
	t := since
	for range maxMissed {
		n := s.Next(t)
		if n.After(now) {
			return due, !due.IsZero()
		}
		due, t = n, n
	}
	return time.Time{}, false
}
