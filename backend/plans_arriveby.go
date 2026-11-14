package main

import (
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
type planSlack struct {
	Days int `json:"days"`
	Stop int `json:"stop"`
}

// planSlackOf is the tightest slack among the stops that have an arrive-by and
// have not yet been reached. progress is each leg's progress ("" to do, done or
// skipped), nil when none is known: a stop the boat has reached or passed no
// longer binds. ok is false when no such stop has an arrive-by.
func planSlackOf(p plan, progress []string) (s planSlack, ok bool) {
	for i, stop := range p.Stops {
		if stop.ArriveBy == "" || stopReached(progress, i) {
			continue
		}
		by, err := time.Parse(planDateLayout, stop.ArriveBy[:len(planDateLayout)])
		if err != nil {
			continue // checkArriveBy refused it on the way in
		}
		arrive, err := time.Parse(planDateLayout, stop.Arrive)
		if err != nil {
			continue
		}
		days := planDaysBetween(arrive, by)
		if !ok || days < s.Days {
			s, ok = planSlack{Days: days, Stop: i}, true
		}
	}
	return s, ok
}

// planOutlook is what the plan has left to do: its tightest slack, when a stop
// still ahead has an arrive-by, and the first leg not done or skipped, -1 when
// there is none.
func planOutlook(p plan, progress []string) (slack planSlack, haveSlack bool, first int) {
	slack, haveSlack = planSlackOf(p, progress)
	first = slices.Index(progress, "")
	return slack, haveSlack, first
}

// stopReached says whether the boat has arrived at or passed stop i: the leg
// into it is done or skipped.
func stopReached(progress []string, i int) bool {
	return i > 0 && i-1 < len(progress) && progress[i-1] != ""
}
