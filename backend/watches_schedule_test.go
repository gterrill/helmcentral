package main

import (
	"strings"
	"testing"
	"time"
)

// Watch schedules are five-field cron read on the boat's clock (ADR 0227).

func TestWatchSchedulePresetsAreCron(t *testing.T) {
	for name, tc := range map[string]struct {
		got  string
		want string
	}{
		"hourly":              {watchPresetHourly(), "0 * * * *"},
		"every 2 hours":       {mustPreset(t, 2), "0 */2 * * *"},
		"every 12 hours":      {mustPreset(t, 12), "0 */12 * * *"},
		"daily at 06:00":      {watchPresetDaily(6, 0), "0 6 * * *"},
		"daily at 07:05":      {watchPresetDaily(7, 5), "5 7 * * *"},
		"weekdays at 07:30":   {watchPresetWeekdays(7, 30, 1, 3, 5), "30 7 * * 1,3,5"},
		"hours window":        {watchPresetWindow(22, 6), "0 22-23,0-6 * * *"},
		"window inside a day": {watchPresetWindow(9, 17), "0 9-17 * * *"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: %q, want %q", name, tc.got, tc.want)
		}
	}
	if _, err := watchPresetEveryHours(5); err == nil {
		t.Error("5 hours is not one of the presets")
	}
}

func mustPreset(t *testing.T, n int) string {
	t.Helper()
	s, err := watchPresetEveryHours(n)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestWatchScheduleSentence(t *testing.T) {
	for cron, want := range map[string]string{
		"0 * * * *":         "Every hour",
		"0 */2 * * *":       "Every 2 hours",
		"0 */6 * * *":       "Every 6 hours",
		"0 6 * * *":         "Every day at 06:00 boat time",
		"5 7 * * *":         "Every day at 07:05 boat time",
		"30 7 * * 1,3,5":    "Mondays, Wednesdays and Fridays at 07:30",
		"0 8 * * 1-5":       "Weekdays at 08:00",
		"0 8 * * 0":         "Sundays at 08:00",
		"0 22-23,0-6 * * *": "Every hour from 22:00 to 06:00",
		"0 9-17 * * *":      "Every hour from 09:00 to 17:00",
		"15 3 1 * *":        "Custom: 15 3 1 * *",
		"*/20 * * * *":      "Custom: */20 * * * *",
		"0 */5 * * *":       "Custom: 0 */5 * * *",
	} {
		got, err := watchScheduleSentence(cron)
		if err != nil {
			t.Errorf("%q: %v", cron, err)
			continue
		}
		if got != want {
			t.Errorf("%q: %q, want %q", cron, got, want)
		}
	}
}

func TestValidateWatchSchedule(t *testing.T) {
	for _, bad := range []string{
		"", "* * * *", "0 6 * * * *", "@hourly", "@every 5m", "TZ=UTC 0 6 * * *", "CRON_TZ=UTC 0 6 * * *",
		"61 * * * *", "0 25 * * *", "abc", "0 6 32 * *",
		"* * * * *",    // every minute
		"*/5 * * * *",  // every five minutes
		"0,10 * * * *", // ten minutes apart
	} {
		if err := validateWatchSchedule(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
	for _, good := range []string{"0 * * * *", "*/15 * * * *", "0 6 * * *", "0 22-23,0-6 * * *", "30 7 * * 1,3,5", "0 0 1 * *"} {
		if err := validateWatchSchedule(good); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}
}

func TestWatchNextRunsAreOnTheBoatsClock(t *testing.T) {
	loc := time.FixedZone("AEST", 10*3600)
	now := time.Date(2026, 10, 28, 18, 0, 0, 0, time.UTC) // 04:00 on the boat
	next, err := watchNextRuns("0 6 * * *", now, loc, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != 3 {
		t.Fatalf("next %v", next)
	}
	want := time.Date(2026, 10, 29, 6, 0, 0, 0, loc)
	if !next[0].Equal(want) || next[0].In(loc).Hour() != 6 || !next[1].Equal(want.AddDate(0, 0, 1)) {
		t.Fatalf("next %v, want 06:00 boat time on the 29th first", next)
	}
}

func TestWatchDue(t *testing.T) {
	loc := time.FixedZone("UTC+10", 10*3600)
	at := func(day, h, m int) time.Time { return time.Date(2026, 10, day, h, m, 0, 0, loc) }
	ptr := func(t time.Time) *time.Time { return &t }
	base := savedWatch{Kind: watchKindScheduled, CreatedAt: at(20, 12, 0), ScheduleChangedAt: at(20, 12, 0)}
	with := func(f func(*savedWatch)) savedWatch { w := base; f(&w); return w }

	for name, tc := range map[string]struct {
		w    savedWatch
		now  time.Time
		want bool
	}{
		"daily before its time":          {with(func(w *savedWatch) { w.Schedule = "0 6 * * *"; w.LastRunAt = ptr(at(28, 6, 0)) }), at(29, 5, 59), false},
		"daily at its time":              {with(func(w *savedWatch) { w.Schedule = "0 6 * * *"; w.LastRunAt = ptr(at(28, 6, 0)) }), at(29, 6, 0), true},
		"daily already ran at that time": {with(func(w *savedWatch) { w.Schedule = "0 6 * * *"; w.LastRunAt = ptr(at(29, 6, 1)) }), at(29, 9, 0), false},
		"after downtime it is due once":  {with(func(w *savedWatch) { w.Schedule = "0 6 * * *"; w.LastRunAt = ptr(at(24, 6, 0)) }), at(29, 12, 0), true},
		"new watch waits for its next time": {with(func(w *savedWatch) {
			w.Schedule = "0 6 * * *"
			w.CreatedAt = at(29, 7, 0)
			w.ScheduleChangedAt = at(29, 7, 0)
		}), at(29, 9, 0), false},
		"new watch runs at its next time": {with(func(w *savedWatch) {
			w.Schedule = "0 6 * * *"
			w.CreatedAt = at(29, 7, 0)
			w.ScheduleChangedAt = at(29, 7, 0)
		}), at(30, 6, 0), true},
		"schedule change resets the clock": {with(func(w *savedWatch) {
			w.Schedule = "0 6 * * *"
			w.LastRunAt = ptr(at(27, 6, 0))
			w.ScheduleChangedAt = at(29, 7, 0)
		}), at(29, 9, 0), false},
		"hourly just after the hour":  {with(func(w *savedWatch) { w.Schedule = "0 * * * *"; w.LastRunAt = ptr(at(29, 9, 0)) }), at(29, 10, 0), true},
		"hourly before the next hour": {with(func(w *savedWatch) { w.Schedule = "0 * * * *"; w.LastRunAt = ptr(at(29, 9, 0)) }), at(29, 9, 59), false},
		"window outside its hours":    {with(func(w *savedWatch) { w.Schedule = "0 22-23,0-6 * * *"; w.LastRunAt = ptr(at(29, 6, 0)) }), at(29, 15, 0), false},
		"window inside its hours":     {with(func(w *savedWatch) { w.Schedule = "0 22-23,0-6 * * *"; w.LastRunAt = ptr(at(28, 23, 0)) }), at(29, 0, 1), true},
	} {
		got, err := watchDue(tc.w, tc.now, loc)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != tc.want {
			t.Errorf("%s: due = %v, want %v", name, got, tc.want)
		}
	}
}

func TestWatchDueWithoutAPositionNeedsAnInstantMissedInEveryZone(t *testing.T) {
	ptr := func(t time.Time) *time.Time { return &t }
	last := time.Date(2026, 10, 28, 6, 0, 0, 0, time.UTC)
	w := savedWatch{Kind: watchKindScheduled, Schedule: "0 6 * * *", CreatedAt: last.AddDate(0, 0, -9), ScheduleChangedAt: last.AddDate(0, 0, -9), LastRunAt: ptr(last)}
	// 30 hours later every zone has passed 06:00 once.
	if !watchMissedInEveryZone(w, last.Add(30*time.Hour)) {
		t.Error("30 hours on, 06:00 has passed in every time zone")
	}
	// 22 hours later a zone 12 hours behind has not reached 06:00 yet.
	if watchMissedInEveryZone(w, last.Add(22*time.Hour)) {
		t.Error("an instant that only some zones have passed is not a missed one")
	}
	// A skip note counts from when it was left.
	skip := last.Add(30 * time.Hour)
	w.LastSkipAt = &skip
	if watchMissedInEveryZone(w, skip.Add(2*time.Hour)) {
		t.Error("a skip is left once per missed instant, not again two hours on")
	}
	if !strings.Contains(watchNoPositionReason, "boat's time is not known") {
		t.Errorf("reason %q", watchNoPositionReason)
	}
}
