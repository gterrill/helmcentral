package main

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func sydneyBoat(t *testing.T) boatTime {
	t.Helper()
	z, err := zoneAtName("Australia/Sydney")
	if err != nil {
		t.Fatal(err)
	}
	return boatTime{placeZone: z}
}

// 23:30 UTC on 15 June is 09:30 on 16 June in Sydney: the boat's day is the
// next one, whatever day the server's own clock is on.
func TestBoatDay_FollowsTheBoatsZone(t *testing.T) {
	now := time.Date(2026, 6, 15, 23, 30, 0, 0, time.UTC)
	got, err := boatCalendarDay(sydneyBoat(t), now)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("boat day = %v, want %v", got, want)
	}
}

func TestBoatDay_LastFixZoneStillCounts(t *testing.T) {
	bt := sydneyBoat(t)
	bt.LastFix = true
	now := time.Date(2026, 6, 15, 23, 30, 0, 0, time.UTC)
	got, err := boatCalendarDay(bt, now)
	if err != nil || got.Day() != 16 {
		t.Fatalf("last-fix boat day = %v, %v; want 16 June", got, err)
	}
}

func TestBoatDay_NoPositionYetIsNotGuessed(t *testing.T) {
	_, err := boatCalendarDay(boatTime{placeZone: utcZone, NoFix: true}, time.Now())
	if !errors.Is(err, errBoatDayUnknown) {
		t.Fatalf("err = %v, want errBoatDayUnknown", err)
	}
}

// A stored last-fix zone that cannot be read leaves the boat on UTC with
// NoFix; the day is just as unknown, and the reason is kept for the log.
func TestBoatDay_UnreadableStoredZoneIsUnknownAndSaysWhy(t *testing.T) {
	cause := errors.New("boat_zone.json: bad json")
	_, err := boatCalendarDay(boatTime{placeZone: utcZone, NoFix: true, Err: cause}, time.Now())
	if !errors.Is(err, errBoatDayUnknown) || !errors.Is(err, cause) {
		t.Fatalf("err = %v, want errBoatDayUnknown wrapping the cause", err)
	}
	// The boat may well have had fixes; the zone is what failed, so the words
	// say that and do not send the operator after the GNSS.
	msg := err.Error()
	if strings.Contains(msg, "has not reported a position") {
		t.Fatalf("err = %q blames the position when the zone is the problem", msg)
	}
	if want := "the boat's time zone can't be worked out (boat_zone.json: bad json), so today's date on the boat is not known"; msg != want {
		t.Fatalf("err = %q, want %q", msg, want)
	}
}

// The scheduler's zone reading (watchBoatLocation) gives the same day and the
// same reasons as boatCalendarDay, so an alarm review and the Maintenance page
// say the same thing.
func TestBoatDayAt_MatchesTheZoneReading(t *testing.T) {
	now := time.Date(2026, 6, 15, 23, 30, 0, 0, time.UTC)
	if got, err := boatDayAt(now, sydneyBoat(t).Loc, nil); err != nil || !got.Equal(time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("day = %v, %v; want 16 June", got, err)
	}
	if _, err := boatDayAt(now, nil, errors.New("no position fix has ever been seen")); !errors.Is(err, errBoatDayUnknown) || err.Error() != errBoatDayUnknown.Error() {
		t.Fatalf("no fix: err = %v, want errBoatDayUnknown", err)
	}
	cause := errors.New("boat_zone.json: bad json")
	_, err := boatDayAt(now, nil, &boatZoneProblem{cause})
	if !errors.Is(err, errBoatDayUnknown) || !errors.Is(err, cause) || !strings.Contains(err.Error(), "time zone can't be worked out (boat_zone.json: bad json)") {
		t.Fatalf("unreadable zone: err = %v, want its reason", err)
	}
}

// Every endpoint that took ?today= answers 503 when the boat's day is unknown,
// whatever the request supplies.
func TestEveryDayDependentEndpoint_UnknownBoatDayAnswers503(t *testing.T) {
	useNoBoatDay(t)

	cases := []struct {
		name    string
		method  string
		url     string
		body    string
		param   string
		handler func(echo.Context) error
	}{
		{"list rules", http.MethodGet, "/api/inventory/maintenance/rules?today=2026-06-15", "", "", listMaintenanceRulesHandler},
		{"get rule", http.MethodGet, "/api/inventory/maintenance/rules/r1?today=2026-06-15", "", "r1", getMaintenanceRuleHandler},
		{"create rule", http.MethodPost, "/api/inventory/maintenance/rules?today=2026-06-15", `{}`, "", createMaintenanceRuleHandler},
		{"complete rule", http.MethodPost, "/api/inventory/maintenance/rules/r1/complete?today=2026-06-15", `{}`, "r1", completeMaintenanceRuleHandler},
		{"hand hours", http.MethodPost, "/api/inventory/equipment/e1/maintenance/hour-readings?today=2026-06-15", `{}`, "e1", recordHandHourReadingHandler},
		{"open a job", http.MethodPost, "/api/inventory/open-jobs?today=2026-06-15", `{"title":"x"}`, "", createOpenJobHandler},
		{"drop a job", http.MethodPost, "/api/inventory/open-jobs/j1/drop?today=2026-06-15", `{}`, "j1", dropOpenJobHandler},
		{"apply proposal", http.MethodPost, "/api/assistant/proposals/p1/apply?today=2026-06-15", "", "p1", applyAssistantProposalHandler},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := newDocumentEchoContext(tc.method, tc.url, tc.body, tc.param)
			if err := tc.handler(c); err != nil {
				t.Fatal(err)
			}
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), errBoatDayUnknown.Error()) {
				t.Fatalf("body does not give the reason: %s", rec.Body.String())
			}
		})
	}
}
