package main

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

/*
Watch schedules (ADR 0227).

A scheduled watch carries a standard five-field cron string (minute, hour,
day of month, month, day of week). It is read on the boat's clock, never UTC.
Helmcentral uses the cron library as a parser only: the scheduler's own
one-minute ticker decides what is due (watches_scheduler.go).

The Watches page offers presets and writes the cron they stand for. The
backend turns any cron string back into a plain sentence and the next run
times so the page can show what a schedule means before it is saved.
*/

// watchMinGap is the shortest gap between two runs a schedule may have. Mate
// reviews every run, so a schedule that fires every minute is a cost nobody
// wants and the operator is told so when they save it.
const watchMinGap = 15 * time.Minute

var watchCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// watchNoPositionReason is why a run is skipped when the boat's clock is not
// known. Time is never read as UTC.
const watchNoPositionReason = "No boat position, so the boat's time is not known"

func watchPresetHourly() string { return "0 * * * *" }

// watchPresetEveryHours is the "every N hours" preset: 2, 3, 4, 6 or 12.
func watchPresetEveryHours(n int) (string, error) {
	switch n {
	case 2, 3, 4, 6, 12:
		return fmt.Sprintf("0 */%d * * *", n), nil
	}
	return "", fmt.Errorf("every %d hours is not a preset; use 2, 3, 4, 6 or 12", n)
}

func watchPresetDaily(hour, minute int) string { return fmt.Sprintf("%d %d * * *", minute, hour) }

// watchPresetWeekdays is chosen days of the week (0 Sunday to 6 Saturday) at a time.
func watchPresetWeekdays(hour, minute int, days ...int) string {
	parts := make([]string, len(days))
	for i, d := range days {
		parts[i] = strconv.Itoa(d)
	}
	return fmt.Sprintf("%d %d * * %s", minute, hour, strings.Join(parts, ","))
}

// watchPresetWindow is hourly between two hours, inclusive, wrapping past
// midnight: 22 to 6 is "22-23,0-6".
func watchPresetWindow(fromHour, toHour int) string {
	if fromHour <= toHour {
		return fmt.Sprintf("0 %d-%d * * *", fromHour, toHour)
	}
	return fmt.Sprintf("0 %d-23,0-%d * * *", fromHour, toHour)
}

// parseWatchSchedule parses a five-field cron string and nothing else: no
// descriptors (@hourly), no @every, no embedded time zone.
func parseWatchSchedule(spec string) (cron.Schedule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, errors.New("a scheduled watch needs a schedule, five cron fields such as \"0 6 * * *\"")
	}
	if strings.HasPrefix(spec, "TZ=") || strings.HasPrefix(spec, "CRON_TZ=") || strings.HasPrefix(spec, "@") {
		return nil, errors.New("give five cron fields (minute hour day month weekday); a time zone or @ shortcut is not allowed, the schedule always runs on the boat's clock")
	}
	if len(strings.Fields(spec)) != 5 {
		return nil, errors.New("a schedule is exactly five cron fields: minute hour day-of-month month day-of-week")
	}
	sched, err := watchCronParser.Parse(spec)
	if err != nil {
		return nil, fmt.Errorf("%q is not a valid schedule: %w", spec, err)
	}
	return sched, nil
}

// validateWatchSchedule checks a schedule parses, runs at all, and never runs
// closer together than watchMinGap.
func validateWatchSchedule(spec string) error {
	sched, err := parseWatchSchedule(spec)
	if err != nil {
		return err
	}
	t := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var prev time.Time
	for i := 0; i < 60; i++ {
		t = sched.Next(t)
		if t.IsZero() {
			if i == 0 {
				return fmt.Errorf("%q never runs", strings.TrimSpace(spec))
			}
			return nil
		}
		if !prev.IsZero() && t.Sub(prev) < watchMinGap {
			return fmt.Errorf("%q runs more often than every %d minutes; Mate reviews each run, so pick a slower schedule", strings.TrimSpace(spec), int(watchMinGap/time.Minute))
		}
		prev = t
	}
	return nil
}

// watchNextRuns is the next n run times after now, read on the boat's clock.
func watchNextRuns(spec string, now time.Time, loc *time.Location, n int) ([]time.Time, error) {
	sched, err := parseWatchSchedule(spec)
	if err != nil {
		return nil, err
	}
	var out []time.Time
	t := now.In(loc)
	for len(out) < n {
		t = sched.Next(t)
		if t.IsZero() {
			break
		}
		out = append(out, t)
	}
	return out, nil
}

// watchDue says whether a run is owed at now: the first cron instant after the
// watch was made, its schedule changed, or it last ran, whichever is latest,
// has passed. After downtime that is one run, never a catch-up.
func watchDue(w savedWatch, now time.Time, loc *time.Location) (bool, error) {
	sched, err := parseWatchSchedule(w.Schedule)
	if err != nil {
		return false, fmt.Errorf("watch %q: %w", w.Name, err)
	}
	next := sched.Next(w.scheduleSince().In(loc))
	return !next.IsZero() && !next.After(now), nil
}

// scheduleSince is the moment after which the next cron instant is owed.
func (w savedWatch) scheduleSince() time.Time {
	since := w.CreatedAt
	if w.ScheduleChangedAt.After(since) {
		since = w.ScheduleChangedAt
	}
	if w.LastRunAt != nil && w.LastRunAt.After(since) {
		since = *w.LastRunAt
	}
	return since
}

var watchZoneOffsets = func() []*time.Location {
	var zones []*time.Location
	for secs := -12 * 3600; secs <= 14*3600; secs += 15 * 60 {
		zones = append(zones, time.FixedZone("", secs))
	}
	return zones
}()

// watchMissedInEveryZone is the test used while the boat's position (and so
// its clock) is unknown: a run is owed only if the schedule's next instant has
// passed in every time zone from UTC-12 to UTC+14, so no time zone could
// explain the wait. It counts from the latest of the watch's last run,
// schedule change, creation and last skip note, so a note is left once per
// missed instant. It never reads the schedule as UTC.
func watchMissedInEveryZone(w savedWatch, now time.Time) bool {
	sched, err := parseWatchSchedule(w.Schedule)
	if err != nil {
		return false
	}
	since := w.scheduleSince()
	if w.LastSkipAt != nil && w.LastSkipAt.After(since) {
		since = *w.LastSkipAt
	}
	for _, zone := range watchZoneOffsets {
		next := sched.Next(since.In(zone))
		if next.IsZero() || next.After(now) {
			return false
		}
	}
	return true
}

// ── the sentence ────────────────────────────────────────────────────────

var watchWeekdayPlural = []string{"Sundays", "Mondays", "Tuesdays", "Wednesdays", "Thursdays", "Fridays", "Saturdays"}

// watchScheduleSentence says a cron string in an operator's words. Anything
// that is not one of the shapes the presets write is "Custom: <cron>".
func watchScheduleSentence(spec string) (string, error) {
	if _, err := parseWatchSchedule(spec); err != nil {
		return "", err
	}
	spec = strings.Join(strings.Fields(spec), " ")
	custom := "Custom: " + spec
	f := strings.Fields(spec)
	minute, hour, dom, month, dow := f[0], f[1], f[2], f[3], f[4]
	if dom != "*" || month != "*" {
		return custom, nil
	}
	m, err := strconv.Atoi(minute)
	if err != nil {
		return custom, nil
	}
	if dow == "*" {
		if m == 0 && hour == "*" {
			return "Every hour", nil
		}
		if n, ok := watchStepHours(hour); ok && m == 0 {
			return fmt.Sprintf("Every %d hours", n), nil
		}
	}
	hours, ok := watchIntSet(hour, 0, 23)
	if !ok {
		return custom, nil
	}
	if dow == "*" {
		if len(hours) == 1 {
			return fmt.Sprintf("Every day at %02d:%02d boat time", hours[0], m), nil
		}
		if m == 0 {
			if from, to, ok := watchCyclicRange(hours); ok {
				return fmt.Sprintf("Every hour from %02d:00 to %02d:00", from, to), nil
			}
		}
		return custom, nil
	}
	if len(hours) != 1 {
		return custom, nil
	}
	days, ok := watchIntSet(dow, 0, 7)
	if !ok {
		return custom, nil
	}
	days = watchNormalDays(days)
	at := fmt.Sprintf("%02d:%02d", hours[0], m)
	if len(days) == 5 && days[0] == 1 && days[4] == 5 {
		return "Weekdays at " + at, nil
	}
	names := make([]string, len(days))
	for i, d := range days {
		names[i] = watchWeekdayPlural[d]
	}
	return watchJoinAnd(names) + " at " + at, nil
}

// watchStepHours reads "*/N" for the N that divide the day evenly and are
// presets.
func watchStepHours(field string) (int, bool) {
	rest, ok := strings.CutPrefix(field, "*/")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	switch n {
	case 2, 3, 4, 6, 12:
		return n, true
	}
	return 0, false
}

// watchIntSet reads a comma list of numbers and a-b ranges, sorted and
// without repeats. A step or a star is not a set.
func watchIntSet(field string, lo, hi int) ([]int, bool) {
	seen := map[int]bool{}
	for _, part := range strings.Split(field, ",") {
		if a, b, isRange := strings.Cut(part, "-"); isRange {
			from, err1 := strconv.Atoi(a)
			to, err2 := strconv.Atoi(b)
			if err1 != nil || err2 != nil || from > to || from < lo || to > hi {
				return nil, false
			}
			for v := from; v <= to; v++ {
				seen[v] = true
			}
			continue
		}
		v, err := strconv.Atoi(part)
		if err != nil || v < lo || v > hi {
			return nil, false
		}
		seen[v] = true
	}
	out := make([]int, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Ints(out)
	return out, len(out) > 0
}

// watchNormalDays folds 7 (Sunday) into 0 and sorts.
func watchNormalDays(days []int) []int {
	seen := map[int]bool{}
	for _, d := range days {
		seen[d%7] = true
	}
	out := make([]int, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Ints(out)
	return out
}

// watchCyclicRange says whether hours is one unbroken run of hours, wrapping
// past midnight, and where it starts and ends.
func watchCyclicRange(hours []int) (from, to int, ok bool) {
	if len(hours) == 24 {
		return 0, 23, true
	}
	in := map[int]bool{}
	for _, h := range hours {
		in[h] = true
	}
	starts := 0
	for _, h := range hours {
		if !in[(h+23)%24] {
			from = h
			starts++
		}
	}
	if starts != 1 {
		return 0, 0, false
	}
	return from, (from + len(hours) - 1) % 24, true
}

func watchJoinAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
