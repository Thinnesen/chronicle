// Package schedule decides when a CronWorkflow starts a run. It is pure: the caller gives the time.
package schedule

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

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

// Next returns the first schedule time after t, or the zero time when no schedule has a time in the next five years.
func (s *Schedule) Next(t time.Time) time.Time {
	var next time.Time
	for _, spec := range s.specs {
		if n := spec.Next(t); !n.IsZero() && (next.IsZero() || n.Before(next)) {
			next = n
		}
	}
	return next
}

// LastDue returns the latest schedule time in (since, now], or false when there is none. The search starts near now
// and doubles its window back to since, so a long outage costs little.
func (s *Schedule) LastDue(since, now time.Time) (time.Time, bool) {
	for window := time.Minute; ; window *= 2 {
		from := now.Add(-window)
		if !from.After(since) {
			return s.lastIn(since, now)
		}
		if due, ok := s.lastIn(from, now); ok {
			return due, true
		}
	}
}

// lastIn returns the latest schedule time in (from, to]. The earlier half of the window was empty, so the loop is short.
func (s *Schedule) lastIn(from, to time.Time) (time.Time, bool) {
	var due time.Time
	for t := from; ; {
		n := s.Next(t)
		// A schedule without a time in the next five years gives the zero time.
		if n.IsZero() || n.After(to) {
			return due, !due.IsZero()
		}
		due, t = n, n
	}
}
