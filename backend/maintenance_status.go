package main

import "time"

// This file is the maintenance feature's pure decision engine (ADR 0138):
// given a rule's own intervals/baseline and the item's current hours
// reading, what status does the rule show right now. It is deliberately
// free of any store or SignalK dependency - maintenance_hours.go resolves
// the live hours figure, maintenance_store.go loads the rule rows, and
// maintenance_handlers.go composes the two before calling
// computeMaintenanceRuleStatus - so the rule "whichever comes first, hours
// or months" can be table-tested with plain values, per AGENTS.md's
// test-first policy.

// maintenanceDefaultDueSoonHours/Months are the fallback due-soon window
// (spec: "default 50 hours / 1 month") used whenever a rule doesn't
// override it. Named constants rather than inline literals so nothing else
// in this feature (the API's own defaults response, a future settings UI)
// has to guess what the fallback actually is - and so no Pikorua-specific
// number ever gets seeded here (AGENTS.md: vessel-agnostic).
const (
	maintenanceDefaultDueSoonHours  = 50.0
	maintenanceDefaultDueSoonMonths = 1
)

// maintenanceStatus is one of the six states a rule can show (spec §4).
// "stored" (excluding an item from the list entirely) and "acknowledged"
// (a rule keeps its computed status and only changes how it sorts and
// displays) are NOT states this type carries - both are listing-level
// concerns applied on top of a status this engine already computed, never
// an input to computing it, which is why neither ever appears as a
// constant here.
type maintenanceStatus string

const (
	maintenanceStatusOverdue        maintenanceStatus = "overdue"
	maintenanceStatusDueSoon        maintenanceStatus = "due_soon"
	maintenanceStatusOK             maintenanceStatus = "ok"
	maintenanceStatusNeverRecorded  maintenanceStatus = "never_recorded"
	maintenanceStatusIntervalNotSet maintenanceStatus = "interval_not_set"
	maintenanceStatusHoursUnknown   maintenanceStatus = "hours_unknown"
)

// maintenanceStatusRank orders the three "real due" states for the
// whichever-comes-first comparison below - higher is more urgent. The other
// three states never reach this function; they are returned directly by
// computeMaintenanceRuleStatus before either axis is ever evaluated.
func maintenanceStatusRank(s maintenanceStatus) int {
	switch s {
	case maintenanceStatusOverdue:
		return 2
	case maintenanceStatusDueSoon:
		return 1
	default:
		return 0
	}
}

// maintenanceHourReading is the item-level current-hours figure every rule
// on that item shares - computed once per item (currentEquipmentHours,
// maintenance_hours.go), never once per rule, so two rules on the same
// engine can never disagree about what "now" reads.
//
// Known is false only when there is nothing to read at all: no
// hour_meter_path bound, or the bound path has never carried a value in
// this snapshot. Age plays no part in Known - see currentEquipmentHours'
// own doc comment (maintenance_hours.go) for why an hour meter's last
// received value is always the current reading, however old. AGENTS.md's
// fallback policy still applies to the case Known IS false: a rule must
// never have its hour-based due status guessed at when nothing has ever
// been received for its path - see computeMaintenanceRuleStatus's own
// hours branch.
type maintenanceHourReading struct {
	Known bool
	// Hours is TRUE hours - Gauge plus whatever meter-reset offset is in
	// force right now (maintenance_hours.go's offsetInForceAt) - and is
	// what every status/remaining computation below actually compares
	// against a rule's own last_done_hours.
	Hours float64
	// Gauge is the RAW meter reading the operator would see by physically
	// looking at the gauge, with no offset applied - what the frontend
	// prefills its own "Hours" field with (2026-09-27 amendment,
	// gaugeToTrueHours): the operator always works in gauge readings, never
	// true hours, so the one figure surfaced to them is this one.
	Gauge float64
	// AsOf is set whenever Known is true and the snapshot carries usable
	// timestamp evidence for the reading (almost always - see
	// currentEquipmentHours) - the wall-clock instant the value was last
	// received, so the UI can show "as of <time>" rather than implying a
	// live-second reading for a meter that stopped changing the moment its
	// engine went quiet.
	AsOf *time.Time
}

// maintenanceRuleStatusInput is computeMaintenanceRuleStatus's whole
// contract. FixedDueDate/LastDoneAt are compared as calendar dates (time of
// day is ignored - dateOnlyDaysUntil below), which is what lets "due today"
// read the same regardless of what time of day the list happens to be
// viewed.
type maintenanceRuleStatusInput struct {
	IntervalHours  *float64
	IntervalMonths *int
	// DueSoonHours/DueSoonMonths override the default due-soon window
	// (maintenanceDefaultDueSoonHours/Months) when set - nil means "use the
	// default", not "no window at all".
	DueSoonHours  *float64
	DueSoonMonths *int
	// FixedDueDate is the calendar-only alternative to IntervalMonths (spec
	// §7): a certificate with a hard expiry rather than a recurring
	// interval computed from LastDoneAt. When set, it IS the due date -
	// IntervalMonths/LastDoneAt are not consulted for the calendar axis at
	// all, even if also present.
	FixedDueDate  *time.Time
	LastDoneAt    *time.Time
	LastDoneHours *float64
	Hours         maintenanceHourReading
	// Today is the OPERATOR'S OWN LOCAL calendar date - never the server's
	// wall clock, and never an instant. A boat well east of UTC reading a
	// server-side time.Now().UTC() before its own local morning would have
	// every due/overdue decision computed against yesterday's date; the
	// caller (maintenance_handlers.go's requireTodayParam) takes this as a
	// required, validated ?today=YYYY-MM-DD from the frontend (which reads
	// the browser's own local clock, lib/local-date.ts's todayISO) rather
	// than ever defaulting to time.Now() itself. Only ever compared as a
	// bare calendar date (civilDateUTC/dateOnlyDaysUntil below), never as
	// an instant - the time-of-day component, if any, is discarded.
	Today time.Time
}

// maintenanceRuleStatusResult is what the API and the list actually show.
// RemainingHours/RemainingDays are nil whenever that axis wasn't
// computable at all (the interval isn't set, or its own baseline is
// missing) - never a zero standing in for "unknown". HoursUnknown is set
// whenever IntervalHours is configured but Hours.Known is false, EVEN WHEN
// the overall Status was still resolved from the months axis - the list
// needs to show "hours unknown" as an annotation on a rule that is,
// separately, correctly overdue by calendar.
type maintenanceRuleStatusResult struct {
	Status         maintenanceStatus
	RemainingHours *float64
	RemainingDays  *int
	HoursUnknown   bool
}

// civilDateUTC truncates t to its own calendar date at UTC midnight, so two
// timestamps on the same day compare equal regardless of time-of-day - the
// comparison a due date is always meant to have ("due today" is due today
// whether checked at 00:05 or 23:55, never a fraction of a day off).
func civilDateUTC(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// dateOnlyDaysUntil is how many whole calendar days from now until due -
// negative once due has passed. Both arguments are truncated to a bare date
// first (civilDateUTC), so this is never off by a fraction of a day from
// whatever time of day "now" happens to be.
func dateOnlyDaysUntil(due, now time.Time) int {
	d := civilDateUTC(due).Sub(civilDateUTC(now))
	return int(d.Hours() / 24)
}

// computeMaintenanceRuleStatus is the whole engine: one rule's own
// intervals and baseline, plus its item's current hours, in; one status
// (plus how much is left, in whichever units apply) out. See this file's
// own tests (maintenance_status_test.go) for the full case table this
// implements - hours only, months only, both (whichever comes first), the
// due-soon window and its override, never recorded, interval not set, and
// stale/missing hours.
func computeMaintenanceRuleStatus(in maintenanceRuleStatusInput) maintenanceRuleStatusResult {
	hasHoursInterval := in.IntervalHours != nil
	hasMonthsInterval := in.IntervalMonths != nil
	hasFixedDate := in.FixedDueDate != nil

	// Neither an hours nor a months interval nor a fixed date: this rule
	// names a job with no frequency attached yet (a profile service entry
	// copied with both slots empty - spec §1). Shown explicitly, never
	// hidden and never treated as due, regardless of any baseline recorded.
	if !hasHoursInterval && !hasMonthsInterval && !hasFixedDate {
		return maintenanceRuleStatusResult{Status: maintenanceStatusIntervalNotSet}
	}

	// A fixed due date needs no baseline at all - it IS the due date, not
	// something computed from a "last done" - so it never counts toward
	// "never recorded" the way an interval that needs LastDoneAt/Hours does.
	neverRecorded := !hasFixedDate && in.LastDoneAt == nil && in.LastDoneHours == nil
	if neverRecorded {
		return maintenanceRuleStatusResult{Status: maintenanceStatusNeverRecorded}
	}

	var candidates []maintenanceStatus
	var remainingHours *float64
	var remainingDays *int
	hoursUnknown := false

	if hasHoursInterval {
		switch {
		case !in.Hours.Known:
			// Bound path missing/stale, or no path at all: the hours axis
			// cannot be evaluated at all - flagged, never guessed at.
			hoursUnknown = true
		case in.LastDoneHours != nil:
			remaining := *in.LastDoneHours + *in.IntervalHours - in.Hours.Hours
			remainingHours = &remaining
			window := maintenanceDefaultDueSoonHours
			if in.DueSoonHours != nil {
				window = *in.DueSoonHours
			}
			switch {
			case remaining <= 0:
				candidates = append(candidates, maintenanceStatusOverdue)
			case remaining <= window:
				candidates = append(candidates, maintenanceStatusDueSoon)
			default:
				candidates = append(candidates, maintenanceStatusOK)
			}
		}
		// LastDoneHours == nil with Hours.Known true: this rule has never
		// had an hours baseline recorded (only a date), so the hours axis
		// simply has nothing to compare against - excluded, not flagged
		// unknown (that word is reserved for a live-reading problem, not a
		// baseline gap).
	}

	if hasFixedDate || (hasMonthsInterval && in.LastDoneAt != nil) {
		var due time.Time
		if hasFixedDate {
			due = *in.FixedDueDate
		} else {
			due = in.LastDoneAt.AddDate(0, *in.IntervalMonths, 0)
		}
		days := dateOnlyDaysUntil(due, in.Today)
		remainingDays = &days

		windowMonths := maintenanceDefaultDueSoonMonths
		if in.DueSoonMonths != nil {
			windowMonths = *in.DueSoonMonths
		}
		dueSoonAt := due.AddDate(0, -windowMonths, 0)
		nowDate := civilDateUTC(in.Today)
		switch {
		case !nowDate.Before(civilDateUTC(due)):
			candidates = append(candidates, maintenanceStatusOverdue)
		case !nowDate.Before(civilDateUTC(dueSoonAt)):
			candidates = append(candidates, maintenanceStatusDueSoon)
		default:
			candidates = append(candidates, maintenanceStatusOK)
		}
	}

	if len(candidates) == 0 {
		if hoursUnknown {
			return maintenanceRuleStatusResult{Status: maintenanceStatusHoursUnknown, HoursUnknown: true}
		}
		// Every configured axis had an interval but no usable baseline to
		// measure it from (e.g. months-only rule whose only recorded
		// baseline was an hours reading). Nothing on record says whether
		// this is due, which is exactly what "never recorded" means.
		return maintenanceRuleStatusResult{Status: maintenanceStatusNeverRecorded}
	}

	worst := candidates[0]
	for _, c := range candidates[1:] {
		if maintenanceStatusRank(c) > maintenanceStatusRank(worst) {
			worst = c
		}
	}
	return maintenanceRuleStatusResult{
		Status:         worst,
		RemainingHours: remainingHours,
		RemainingDays:  remainingDays,
		HoursUnknown:   hoursUnknown,
	}
}
