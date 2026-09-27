package main

import (
	"strconv"
	"testing"
	"time"
)

func mustParseDate(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return tm
}

func f(v float64) *float64 { return &v }
func i(v int) *int         { return &v }

func TestComputeMaintenanceRuleStatus(t *testing.T) {
	now := mustParseDate(t, "2026-06-15")

	cases := []struct {
		name         string
		in           maintenanceRuleStatusInput
		wantStatus   maintenanceStatus
		wantHours    *float64
		wantDays     *int
		wantHoursUnk bool
	}{
		{
			name:       "no interval at all is interval_not_set",
			in:         maintenanceRuleStatusInput{Today: now},
			wantStatus: maintenanceStatusIntervalNotSet,
		},
		{
			name: "interval set but never done is never_recorded",
			in: maintenanceRuleStatusInput{
				IntervalHours: f(250),
				Today:         now,
			},
			wantStatus: maintenanceStatusNeverRecorded,
		},
		{
			name: "hours only: well within interval is ok",
			in: maintenanceRuleStatusInput{
				IntervalHours: f(250),
				LastDoneHours: f(1000),
				Hours:         maintenanceHourReading{Known: true, Hours: 1100},
				Today:         now,
			},
			wantStatus: maintenanceStatusOK,
			wantHours:  f(150), // 1000+250-1100
		},
		{
			name: "hours only: inside default due-soon window",
			in: maintenanceRuleStatusInput{
				IntervalHours: f(250),
				LastDoneHours: f(1000),
				Hours:         maintenanceHourReading{Known: true, Hours: 1220},
				Today:         now,
			},
			wantStatus: maintenanceStatusDueSoon,
			wantHours:  f(30), // <= default 50
		},
		{
			name: "hours only: past interval is overdue",
			in: maintenanceRuleStatusInput{
				IntervalHours: f(250),
				LastDoneHours: f(1000),
				Hours:         maintenanceHourReading{Known: true, Hours: 1400},
				Today:         now,
			},
			wantStatus: maintenanceStatusOverdue,
			wantHours:  f(-150),
		},
		{
			name: "hours only: due-soon override narrows the window",
			in: maintenanceRuleStatusInput{
				IntervalHours: f(250),
				DueSoonHours:  f(10),
				LastDoneHours: f(1000),
				Hours:         maintenanceHourReading{Known: true, Hours: 1220}, // 30 remaining
				Today:         now,
			},
			// 30 remaining > overridden 10-hour window, so this is OK even
			// though it would have been due-soon under the default window.
			wantStatus: maintenanceStatusOK,
			wantHours:  f(30),
		},
		{
			name: "hours only: missing hours is hours_unknown, never guessed",
			in: maintenanceRuleStatusInput{
				IntervalHours: f(250),
				LastDoneHours: f(1000),
				Hours:         maintenanceHourReading{Known: false},
				Today:         now,
			},
			wantStatus:   maintenanceStatusHoursUnknown,
			wantHoursUnk: true,
		},
		{
			name: "months only: well within interval is ok",
			in: maintenanceRuleStatusInput{
				IntervalMonths: i(12),
				LastDoneAt:     ptrTime(mustParseDate(t, "2026-01-01")),
				Today:          now,
			},
			wantStatus: maintenanceStatusOK,
			wantDays:   i(200), // 2027-01-01 minus 2026-06-15
		},
		{
			name: "months only: inside default one-month due-soon window",
			in: maintenanceRuleStatusInput{
				IntervalMonths: i(6),
				LastDoneAt:     ptrTime(mustParseDate(t, "2025-12-20")), // due 2026-06-20
				Today:          now,                                     // 5 days out
			},
			wantStatus: maintenanceStatusDueSoon,
			wantDays:   i(5),
		},
		{
			name: "months only: past interval is overdue",
			in: maintenanceRuleStatusInput{
				IntervalMonths: i(6),
				LastDoneAt:     ptrTime(mustParseDate(t, "2025-06-01")), // due 2025-12-01
				Today:          now,
			},
			wantStatus: maintenanceStatusOverdue,
		},
		{
			// Default 1-month window would still say "ok" here (due
			// 2027-01-01, default due-soon-at 2026-12-01, now 2026-06-15
			// is well before that) - the override is what pulls it into
			// due-soon, so this case actually exercises the override
			// rather than a window either value would have satisfied.
			name: "months only: due-soon override widens the window",
			in: maintenanceRuleStatusInput{
				IntervalMonths: i(12),
				DueSoonMonths:  i(8),
				LastDoneAt:     ptrTime(mustParseDate(t, "2026-01-01")), // due 2027-01-01
				Today:          now,
			},
			wantStatus: maintenanceStatusDueSoon,
		},
		{
			name: "fixed due date needs no baseline at all",
			in: maintenanceRuleStatusInput{
				FixedDueDate: ptrTime(mustParseDate(t, "2026-07-01")),
				Today:        now,
			},
			wantStatus: maintenanceStatusDueSoon, // 16 days out, within 1-month default window
			wantDays:   i(16),
		},
		{
			name: "fixed due date already passed is overdue with no last-done",
			in: maintenanceRuleStatusInput{
				FixedDueDate: ptrTime(mustParseDate(t, "2026-01-01")),
				Today:        now,
			},
			wantStatus: maintenanceStatusOverdue,
		},
		{
			name: "whichever comes first: hours overdue outranks months ok",
			in: maintenanceRuleStatusInput{
				IntervalHours:  f(250),
				IntervalMonths: i(12),
				LastDoneAt:     ptrTime(mustParseDate(t, "2026-01-01")), // months: ok, 200 days out
				LastDoneHours:  f(1000),
				Hours:          maintenanceHourReading{Known: true, Hours: 1400}, // hours: overdue
				Today:          now,
			},
			wantStatus: maintenanceStatusOverdue,
		},
		{
			name: "whichever comes first: months overdue outranks hours ok",
			in: maintenanceRuleStatusInput{
				IntervalHours:  f(500),
				IntervalMonths: i(6),
				LastDoneAt:     ptrTime(mustParseDate(t, "2025-06-01")), // months: overdue
				LastDoneHours:  f(1000),
				Hours:          maintenanceHourReading{Known: true, Hours: 1100}, // hours: ok
				Today:          now,
			},
			wantStatus: maintenanceStatusOverdue,
		},
		{
			name: "whichever comes first: hours unknown does not mask a calendar overdue",
			in: maintenanceRuleStatusInput{
				IntervalHours:  f(250),
				IntervalMonths: i(6),
				LastDoneAt:     ptrTime(mustParseDate(t, "2025-06-01")), // months: overdue
				LastDoneHours:  f(1000),
				Hours:          maintenanceHourReading{Known: false}, // hours: unknown
				Today:          now,
			},
			wantStatus:   maintenanceStatusOverdue,
			wantHoursUnk: true,
		},
		{
			name: "recorded by hours only, months interval set but no date baseline: never_recorded",
			in: maintenanceRuleStatusInput{
				IntervalMonths: i(12),
				LastDoneHours:  f(1000), // no LastDoneAt at all
				Hours:          maintenanceHourReading{Known: true, Hours: 1050},
				Today:          now,
			},
			wantStatus: maintenanceStatusNeverRecorded,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeMaintenanceRuleStatus(tc.in)
			if got.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", got.Status, tc.wantStatus)
			}
			if got.HoursUnknown != tc.wantHoursUnk {
				t.Errorf("HoursUnknown = %v, want %v", got.HoursUnknown, tc.wantHoursUnk)
			}
			if tc.wantHours != nil {
				if got.RemainingHours == nil || *got.RemainingHours != *tc.wantHours {
					t.Errorf("RemainingHours = %v, want %v", ptrFloatString(got.RemainingHours), *tc.wantHours)
				}
			}
			if tc.wantDays != nil {
				if got.RemainingDays == nil || *got.RemainingDays != *tc.wantDays {
					t.Errorf("RemainingDays = %v, want %v", ptrIntString(got.RemainingDays), *tc.wantDays)
				}
			}
		})
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

func ptrFloatString(p *float64) string {
	if p == nil {
		return "<nil>"
	}
	return strconv.FormatFloat(*p, 'g', -1, 64)
}

func ptrIntString(p *int) string {
	if p == nil {
		return "<nil>"
	}
	return strconv.Itoa(*p)
}

func TestComputeMaintenanceRuleStatus_AcknowledgedIsNotAnInput(t *testing.T) {
	// The engine has no acknowledged field at all - a listing applies ack
	// as a display/sort concern on top of whatever status this returns
	// (spec §5: "it stays overdue/due-soon and still counts"). This test
	// documents that guarantee: the same input always produces the same
	// status regardless of anything the caller might otherwise know about
	// acknowledgement.
	now := mustParseDate(t, "2026-06-15")
	in := maintenanceRuleStatusInput{
		IntervalHours: f(250),
		LastDoneHours: f(1000),
		Hours:         maintenanceHourReading{Known: true, Hours: 1400},
		Today:         now,
	}
	first := computeMaintenanceRuleStatus(in)
	second := computeMaintenanceRuleStatus(in)
	if first.Status != maintenanceStatusOverdue || second.Status != maintenanceStatusOverdue {
		t.Fatalf("expected a stable overdue status, got %v / %v", first.Status, second.Status)
	}
}
