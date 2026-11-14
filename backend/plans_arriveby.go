package main

import (
	"fmt"
	"regexp"
	"slices"
	"time"
)

// Arrive-by and slack (ADR 0288). An arrive-by is the latest date and time the
// operator wants to be at a stop, "YYYY-MM-DDTHH:MM" at that stop. Slack is the
// whole calendar days between the stop's planned arrival and the arrive-by's
// day: the days the plan can slip before that stop is reached late. The time of
// day is kept for the operator to read; the days are what slack counts.

const planArriveByLayout = "2006-01-02T15:04"

var planArriveByPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T([01]\d|2[0-3]):[0-5]\d$`)

// checkArriveBy accepts "" or a real date and time. n is the stop's number for
// the message.
func checkArriveBy(v string, n int) *planError {
	if v == "" {
		return nil
	}
	if !planArriveByPattern.MatchString(v) {
		return perr("arrive_by", "stop %d: arrive-by must be a date and time, YYYY-MM-DDTHH:MM", n)
	}
	if _, err := time.Parse(planArriveByLayout, v); err != nil {
		return perr("arrive_by", "stop %d: arrive-by is not a real date", n)
	}
	return nil
}

// planSlack is the room a plan has: Days is negative when the stop is already
// planned to be reached after its arrive-by, and Stop is the stop that sets it.
// BehindDays is how far the plan has fallen behind its stored dates: the days
// since the first leg still to do should have left. Days is counted on the plan
// moved forward by them.
type planSlack struct {
	Days       int `json:"days"`
	Stop       int `json:"stop"`
	BehindDays int `json:"behind_days"`
}

// planBehindDays is how many days the plan has fallen behind: today, in the zone
// of the stop the first leg still to do leaves, less that leg's planned
// departure day, and 0 when the plan is on time, ahead, or has nothing left to
// do. The stored dates are not moved; the projection is made from this.
func planBehindDays(p plan, progress []string, now time.Time) (int, error) {
	first := slices.Index(progress, "")
	if first < 0 {
		if progress != nil {
			return 0, nil // every leg is done or skipped
		}
		first = 0
	}
	if first >= len(p.Stops)-1 {
		return 0, nil
	}
	stop := p.Stops[first]
	zone, err := stayZoneAt(stop.Lat, stop.Lon)
	if err != nil {
		return 0, fmt.Errorf("the time zone at %s is not known: %w", stop.Name, err)
	}
	planned, err := time.Parse(planDateLayout, stop.Depart)
	if err != nil {
		return 0, fmt.Errorf("the departure from %s, %q, is not a date: %w", stop.Name, stop.Depart, err)
	}
	today, err := time.Parse(planDateLayout, dayAt(zone, now))
	if err != nil {
		return 0, err
	}
	return max(0, planDaysBetween(planned, today)), nil
}

// planSlackOf is the tightest slack among the stops that have an arrive-by and
// have not yet been reached. progress is each leg's progress ("" to do, done or
// skipped), nil when none is known: a stop the boat has reached or passed no
// longer binds. behind is how many days the plan has fallen behind: every stop
// after the first leg still to do arrives that many days later than stored. ok
// is false when no such stop has an arrive-by. A stored arrive-by or arrival
// that cannot be read is an error: it was checked on the way in, so it says the
// stored plan is damaged.
func planSlackOf(p plan, progress []string, behind int) (s planSlack, ok bool, err error) {
	first := max(slices.Index(progress, ""), 0)
	for i, stop := range p.Stops {
		if stop.ArriveBy == "" || stopReached(progress, i) {
			continue
		}
		byAt, err := time.Parse(planArriveByLayout, stop.ArriveBy)
		if err != nil {
			return planSlack{}, false, fmt.Errorf("the arrive-by at %s, %q, is not a date and time: %w", stop.Name, stop.ArriveBy, err)
		}
		by := time.Date(byAt.Year(), byAt.Month(), byAt.Day(), 0, 0, 0, 0, time.UTC)
		arrive, err := time.Parse(planDateLayout, stop.Arrive)
		if err != nil {
			return planSlack{}, false, fmt.Errorf("the arrival at %s, %q, is not a date: %w", stop.Name, stop.Arrive, err)
		}
		if i > first {
			arrive = arrive.AddDate(0, 0, behind)
		}
		days := planDaysBetween(arrive, by)
		if !ok || days < s.Days {
			s, ok = planSlack{Days: days, Stop: i, BehindDays: behind}, true
		}
	}
	return s, ok, nil
}

// planOutlook is what the plan has left to do: its tightest slack on the plan
// moved forward to today, when a stop still ahead has an arrive-by, and the first
// leg not done or skipped, -1 when there is none.
func planOutlook(p plan, progress []string, now time.Time) (slack planSlack, haveSlack bool, first int, err error) {
	behind, err := planBehindDays(p, progress, now)
	if err != nil {
		return slack, false, -1, err
	}
	slack, haveSlack, err = planSlackOf(p, progress, behind)
	if err != nil {
		return slack, false, -1, err
	}
	first = slices.Index(progress, "")
	return slack, haveSlack, first, nil
}

// stopReached says whether the boat has arrived at or passed stop i: the leg
// into it is done or skipped.
func stopReached(progress []string, i int) bool {
	return i > 0 && i-1 < len(progress) && progress[i-1] != ""
}
