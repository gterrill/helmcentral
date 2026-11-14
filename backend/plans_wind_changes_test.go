package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The assessment works out each row's wind changes (ADR 0287): a leg's between
// its exact departure and ETA, a lay-day night's between 18:00 and 06:00 in the
// stop's zone. The clock is Saturday 14 November 2026, 4.30 AM at Brisbane, as
// in the other assessment tests; every leg's course is 190 degrees true.

// pwcStep is a forecast whose wind blows from fromDeg at fromKn until at, and
// from toDeg at toKn from then on, every hour of the 14th to the 24th.
func pwcStep(at time.Time, fromDeg, fromKn, toDeg, toKn float64) forecastSource {
	return pwcStepBetween(paAt(14, 0, 0), paAt(24, 23, 0), at, fromDeg, fromKn, toDeg, toKn)
}

func pwcStepBetween(first, last, at time.Time, fromDeg, fromKn, toDeg, toKn float64) forecastSource {
	hours := map[int64]passageHour{}
	for h := first; !h.After(last); h = h.Add(time.Hour) {
		deg, w := fromDeg, fromKn
		if !h.Before(at) {
			deg, w = toDeg, toKn
		}
		hours[h.Unix()] = passageHour{WindMS: kn(w), GustMS: kn(w), WindDirDeg: deg}
	}
	return forecastSource{Provider: "wx", Fetch: func(context.Context, float64, float64) (passageForecast, error) {
		return passageForecast{Hours: hours}, nil
	}}
}

// pwcLeg assesses a one-leg plan leaving at departTime on the 14th over
// distanceNm (6 kn, so 6 nm an hour).
func pwcLeg(t *testing.T, departTime string, distanceNm float64, f *paFake) planLegAssessment {
	t.Helper()
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	p.Stops[0].DepartTime = departTime
	legs[0].DistanceNm = distanceNm
	return paAssess(t, p, legs, f).Legs[0]
}

func TestAssessPlan_AnOvernightLegCarriesTheChangeAtTwoInTheMorning(t *testing.T) {
	// Depart Sat 8 PM, 60 nm at 6 kn, ETA Sun 6 AM.
	at := paAt(15, 2, 0)
	l := pwcLeg(t, "20:00", 60, &paFake{fc: pwcStep(at, 0, 12, 135, 18)})
	if !paParse(t, l.Arrive).Equal(paAt(15, 6, 0)) {
		t.Fatalf("arrive = %s", l.Arrive)
	}
	got := l.WindChanges.Changes
	if len(got) != 1 {
		t.Fatalf("changes = %+v, want the one at Sun 2 AM", l.WindChanges)
	}
	c := got[0]
	if c.Label != "Sun 2 AM" || c.From != "N" || c.To != "SE" || c.SpeedMinKn != 18 || c.SpeedMaxKn != 18 {
		t.Errorf("change = %+v", c)
	}
	// From the north is 170 degrees off a 190 degree course (astern); from the
	// south-east is 55 off (ahead), and 18 kn is past the 14 kn ahead marginal.
	if c.FromSector != "astern" || c.Sector != "ahead" || !c.Prominent {
		t.Errorf("sectors %q to %q, prominent %v", c.FromSector, c.Sector, c.Prominent)
	}
	if l.WindChanges.NotChecked != "" {
		t.Errorf("flags = %+v", l.WindChanges)
	}
}

func TestAssessPlan_ALegKeepsOnlyTheChangesBetweenItsExactInstants(t *testing.T) {
	cases := []struct {
		name   string
		depart string
		at     time.Time
		want   int
	}{
		{"an hour stamp before a 10.45 departure is out", "10:45", paAt(14, 10, 0), 0},
		{"the change at the departure instant is in", "10:00", paAt(14, 10, 0), 1},
		{"the change after a 9.45 departure is in", "09:45", paAt(14, 10, 0), 1},
		{"a change after the ETA (10.45 plus 5 h) is out", "10:45", paAt(14, 16, 0), 0},
		{"a change at the ETA instant is in", "11:00", paAt(14, 16, 0), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := pwcLeg(t, tc.depart, 30, &paFake{fc: pwcStep(tc.at, 0, 12, 135, 18)})
			if got := len(l.WindChanges.Changes); got != tc.want {
				t.Fatalf("changes = %+v, want %d", l.WindChanges, tc.want)
			}
		})
	}
}

func TestLegChangeWindow(t *testing.T) {
	day := paAt(14, 0, 0)
	cases := []struct {
		name               string
		depart, arrive     time.Time
		wantFrom, wantTo   time.Time
		wantClosedAtTheEnd bool
	}{
		{"departure to ETA", paAt(14, 20, 0), paAt(15, 6, 0), paAt(14, 20, 0), paAt(15, 6, 0), true},
		{"no ETA: departure to the end of that day", paAt(14, 10, 45), time.Time{}, paAt(14, 10, 45), paAt(15, 0, 0), false},
		{"no departure: the whole day", time.Time{}, time.Time{}, paAt(14, 0, 0), paAt(15, 0, 0), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := legChangeWindow(day, tc.depart, tc.arrive, paLoc)
			if !w.from.Equal(tc.wantFrom) || !w.to.Equal(tc.wantTo) || w.closed != tc.wantClosedAtTheEnd {
				t.Fatalf("window = %+v", w)
			}
		})
	}
}

func TestAssessPlan_ALegWithNoDepartureTimeReadsTheWholeDay(t *testing.T) {
	// The ranking fails, so the leg has no departure and no ETA: the day is the
	// window. A change at 11 PM is on it, one at 1 AM the next day is not.
	for _, tc := range []struct {
		at   time.Time
		want int
	}{{paAt(14, 23, 0), 1}, {paAt(14, 8, 0), 1}, {paAt(15, 1, 0), 0}} {
		f := &paFake{
			fc: pwcStep(tc.at, 0, 12, 135, 18),
			departures: func(q planDepartureQuery) ([]tidalDeparture, error) {
				return nil, context.DeadlineExceeded
			},
		}
		p, legs := paPlan(t, "2026-11-14", 0, 0)
		l := paAssess(t, p, legs, f).Legs[0]
		if l.Depart != "" {
			t.Fatalf("leg = %+v, want no departure", l)
		}
		if got := len(l.WindChanges.Changes); got != tc.want {
			t.Errorf("change at %s: %d changes %+v, want %d", tc.at.Format(time.RFC3339), got, l.WindChanges, tc.want)
		}
	}
}

func TestAssessPlan_ALegThatIsNotAssessedSaysItsChangesWereNotChecked(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	p.Stops[0].DepartTime = "10:00"
	f := &paFake{fc: pwcStep(paAt(14, 12, 0), 0, 12, 135, 18), speedErr: context.DeadlineExceeded}
	got := paAssess(t, p, legs, f).Legs[0]
	if !strings.Contains(got.WindChanges.NotChecked, "No ETA") || len(got.WindChanges.Changes) != 0 {
		t.Fatalf("wind changes = %+v, want not checked, with the reason", got.WindChanges)
	}
}

func TestAssessPlan_TheSectorIsTheLegsCourseAgainstTheWindAfter(t *testing.T) {
	cases := []struct {
		name       string
		toDeg      float64
		toKn       float64
		limits     passageLimits
		wantSector string
		prominent  bool
	}{
		{"exactly 120 degrees off is astern, and 25 kn reaches the 24.5 astern marginal", 70, 25, pwLimitsAll(), "astern", true},
		{"24 kn is short of the astern marginal", 70, 24, pwLimitsAll(), "astern", false},
		{"119 degrees off is abeam, marginal 21", 71, 21, pwLimitsAll(), "abeam", true},
		{"exactly 60 degrees off is abeam, not ahead", 130, 20, pwLimitsAll(), "abeam", false},
		{"under 60 is ahead, marginal 14", 160, 14, pwLimitsAll(), "ahead", true},
		{"a sector with no wind no-go is never prominent", 70, 40, passageLimits{Ahead: passageSectorLimits{WindKnNoGo: pwF(20)}}, "astern", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Before the change the wind is from 250 (60 off the course, abeam).
			f := &paFake{fc: pwcStep(paAt(14, 12, 0), 250, 12, tc.toDeg, tc.toKn), limits: tc.limits}
			l := pwcLeg(t, "09:00", 30, f)
			if len(l.WindChanges.Changes) != 1 {
				t.Fatalf("changes = %+v", l.WindChanges)
			}
			c := l.WindChanges.Changes[0]
			if c.Sector != tc.wantSector || c.FromSector != "abeam" || c.Prominent != tc.prominent {
				t.Fatalf("change = %+v, want sector %s (from abeam) prominent %v", c, tc.wantSector, tc.prominent)
			}
		})
	}
}

func TestAssessPlan_AScanThatStopsBeforeTheETAIsNotChecked(t *testing.T) {
	// Depart Sat 8 PM, ETA Sun 6 AM, and the forecast has no 3 AM: the scan reads to 2 AM.
	hours := pwcStep(paAt(15, 0, 0), 0, 12, 135, 18)
	full, err := hours.Fetch(context.Background(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	delete(full.Hours, paAt(15, 3, 0).Unix())
	fc := forecastSource{Provider: "wx", Fetch: func(context.Context, float64, float64) (passageForecast, error) { return full, nil }}
	l := pwcLeg(t, "20:00", 60, &paFake{fc: fc})
	nc := l.WindChanges.NotChecked
	if !strings.Contains(nc, "Sun 15 Nov 2.00 AM") || !strings.Contains(nc, windChangeCutSkipsHour) {
		t.Fatalf("not checked = %q, want it to say it read to Sun 15 Nov 2.00 AM and why it stopped", nc)
	}
}

func TestAssessPlan_AForecastThatEndsBeforeTheETAIsNotChecked(t *testing.T) {
	fc := pwcStepBetween(paAt(14, 0, 0), paAt(15, 4, 0), paAt(15, 0, 0), 0, 12, 135, 18)
	l := pwcLeg(t, "20:00", 60, &paFake{fc: fc})
	if l.WindChanges.NotChecked == "" {
		t.Fatalf("wind changes = %+v: the forecast stops at 4 AM and the ETA is 6 AM", l.WindChanges)
	}
}

func TestAssessPlan_ALegHasAnEmptyListNotNull(t *testing.T) {
	l := pwcLeg(t, "09:00", 30, &paFake{})
	raw, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"wind_changes":{"changes":[],"not_checked":""}`) {
		t.Fatalf("wire = %s", raw)
	}
}

func TestAssessPlan_WithNoLimitsALegAndANightSayTheirChangesWereNotChecked(t *testing.T) {
	for name, tc := range map[string]struct {
		limits func() (passageLimits, error)
		want   string
	}{
		"unset":      {func() (passageLimits, error) { return passageLimits{}, nil }, "your passage limits aren't set (Settings → Mate → Passage limits)"},
		"unreadable": {func() (passageLimits, error) { return passageLimits{}, errors.New("settings.yaml is unreadable") }, "settings.yaml is unreadable"},
	} {
		t.Run(name, func(t *testing.T) {
			p, legs := paPlan(t, "2026-11-14", 0, 2, 0)
			p.Stops[0].DepartTime = "08:00"
			f := &paFake{}
			d := paDeps(f)
			d.limits = tc.limits
			got := assessPlan(context.Background(), p, legs, d, planAssessOptions{})
			if w := got.Legs[0].WindChanges; !strings.Contains(w.NotChecked, tc.want) || len(w.Changes) != 0 {
				t.Fatalf("leg wind changes = %+v, want not checked: %s", w, tc.want)
			}
			// The lay-day night, and the arrival night at the middle stop, say the same.
			if len(got.NightWinds) == 0 {
				t.Fatalf("night winds = %+v", got.NightWinds)
			}
			for _, n := range got.NightWinds {
				if !strings.Contains(n.NotChecked, tc.want) || len(n.Changes) != 0 {
					t.Fatalf("night = %+v, want not checked: %s", n, tc.want)
				}
			}
			if f.fetches != 0 {
				t.Fatal("no forecast is read when there are no limits to judge it by")
			}
		})
	}
}

// ── findings from the third review ──────────────────────────────────────────

// pwcNightAt is the entry for a night at a stop on a date, or false.
func pwcNightAt(got planAssessment, stop int, date string) (planNightWinds, bool) {
	for _, n := range got.NightWinds {
		if n.Stop == stop && n.Date == date {
			return n, true
		}
	}
	return planNightWinds{}, false
}

func TestAssessPlan_TheArrivalNightAtTheLastStopCarriesItsChanges(t *testing.T) {
	// Depart 9 AM, 36 nm at 6 kn, ETA 3 PM. The night of arrival runs 6 PM to 6 AM.
	for _, tc := range []struct {
		name string
		at   time.Time
		want int
	}{
		{"2 AM that night is on it", paAt(15, 2, 0), 1},
		{"7 PM that evening is on it", paAt(14, 19, 0), 1},
		{"5 PM is before the night", paAt(14, 17, 0), 0},
		{"7 AM is after it", paAt(15, 7, 0), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, legs := paPlan(t, "2026-11-14", 0, 0)
			p.Stops[0].DepartTime = "09:00"
			legs[0].DistanceNm = 36
			got := paAssess(t, p, legs, &paFake{fc: pwcStep(tc.at, 0, 12, 135, 18)})
			if !paParse(t, got.Legs[0].Arrive).Equal(paAt(14, 15, 0)) {
				t.Fatalf("arrive = %s", got.Legs[0].Arrive)
			}
			n, ok := pwcNightAt(got, 1, p.Stops[1].Arrive)
			if !ok {
				t.Fatalf("night winds = %+v, want the arrival night at stop 1", got.NightWinds)
			}
			if len(n.Changes) != tc.want || n.NotChecked != "" {
				t.Fatalf("arrival night = %+v, want %d changes", n, tc.want)
			}
		})
	}
}

func TestAssessPlan_AnArrivalAfterSixInTheEveningStartsTheNightAtTheETA(t *testing.T) {
	// ETA 8 PM. A change at 7 PM is on the leg, not on the night; one at 9 PM is on the night.
	for _, tc := range []struct {
		at              time.Time
		wantLeg, wantNt int
	}{{paAt(14, 19, 0), 1, 0}, {paAt(14, 21, 0), 0, 1}, {paAt(14, 20, 0), 1, 0}} {
		p, legs := paPlan(t, "2026-11-14", 0, 0)
		p.Stops[0].DepartTime = "14:00"
		legs[0].DistanceNm = 36
		got := paAssess(t, p, legs, &paFake{fc: pwcStep(tc.at, 0, 12, 135, 18)})
		n, _ := pwcNightAt(got, 1, p.Stops[1].Arrive)
		if len(got.Legs[0].WindChanges.Changes) != tc.wantLeg || len(n.Changes) != tc.wantNt {
			t.Errorf("change at %s: leg %+v, night %+v", tc.at.Format("15:04"), got.Legs[0].WindChanges, n)
		}
	}
}

func TestAssessPlan_OneStopThatCannotBeReadDoesNotMarkTheOthers(t *testing.T) {
	// A (0 nights) to B (2 nights); C (2 nights) is no leg's end here, so it is
	// fetched on its own and that fetch fails. B was read with the leg.
	p, legs := paPlan(t, "2026-11-14", 0, 2, 2)
	legs = legs[:1]
	b, err := time.ParseInLocation("2006-01-02", p.Stops[1].Arrive, paLoc)
	if err != nil {
		t.Fatal(err)
	}
	step := pwcStep(b.AddDate(0, 0, 1).Add(22*time.Hour), 0, 12, 135, 18)
	inner := step.Fetch
	step.Fetch = func(ctx context.Context, lat, lon float64) (passageForecast, error) {
		if lat < -26.4 {
			return passageForecast{}, errors.New("provider is down")
		}
		return inner(ctx, lat, lon)
	}
	got := paAssess(t, p, legs, &paFake{fc: step})
	nb, ok := pwcNightAt(got, 1, b.AddDate(0, 0, 1).Format("2006-01-02"))
	if !ok || nb.NotChecked != "" || len(nb.Changes) != 1 {
		t.Fatalf("B's night = %+v (found %v), want its change and no flag", nb, ok)
	}
	c, err := time.ParseInLocation("2006-01-02", p.Stops[2].Arrive, paLoc)
	if err != nil {
		t.Fatal(err)
	}
	nc, ok := pwcNightAt(got, 2, c.AddDate(0, 0, 1).Format("2006-01-02"))
	if !ok || !strings.Contains(nc.NotChecked, "Mooloolaba") || !strings.Contains(nc.NotChecked, "provider is down") {
		t.Fatalf("C's night = %+v (found %v), want it not checked, naming Mooloolaba", nc, ok)
	}
}

func TestAssessPlan_TheForecastTimeIsTheLegsNotTheStops(t *testing.T) {
	legsAt := paAt(14, 4, 20)
	older := paAt(14, 1, 0)
	p, legs := paPlan(t, "2026-11-14", 0, 2, 2)
	legs = legs[:1]
	inner := pwcStep(paAt(20, 0, 0), 0, 12, 135, 18).Fetch
	fc := forecastSource{Provider: "wx", Fetch: func(ctx context.Context, lat, lon float64) (passageForecast, error) {
		f, err := inner(ctx, lat, lon)
		f.FetchedAt = legsAt
		if lat < -26.4 {
			f.FetchedAt = older // the stop that no leg ends at
		}
		return f, err
	}}
	got := paAssess(t, p, legs, &paFake{fc: fc})
	if want := legsAt.UTC().Format(time.RFC3339); got.ForecastAsOf != want {
		t.Fatalf("forecast as of %q, want the legs' %q", got.ForecastAsOf, want)
	}
}

func TestAssessPlan_AnETAWithinTheHourAfterTheLastStampIsChecked(t *testing.T) {
	// The forecast's last stamp is 4 PM; the leg arrives at 4.30 PM.
	fc := pwcStepBetween(paAt(14, 0, 0), paAt(14, 16, 0), paAt(14, 10, 0), 0, 12, 135, 18)
	l := pwcLeg(t, "09:30", 42, &paFake{fc: fc})
	if !paParse(t, l.Arrive).Equal(paAt(14, 16, 30)) {
		t.Fatalf("arrive = %s", l.Arrive)
	}
	if l.WindChanges.NotChecked != "" || len(l.WindChanges.Changes) != 1 {
		t.Fatalf("wind changes = %+v, want the 10 AM change and no flag", l.WindChanges)
	}
}

func TestAssessPlan_TheLegUnderWayReadsFromNowToItsETA(t *testing.T) {
	// Left at 4 AM (30 nm, ETA 9 AM); now is 4.30 AM and the boat is at the first stop's
	// start of the recording.
	for _, tc := range []struct {
		name string
		at   time.Time
		want int
	}{
		{"a change before now is behind the boat", paAt(14, 3, 0), 0},
		{"a change at 7 AM is ahead", paAt(14, 7, 0), 1},
		{"a change after the ETA is not on the leg", paAt(14, 10, 0), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, legs := paPlan(t, "2026-11-14", 0, 0)
			p.Stops[0].DepartTime = "04:00"
			f := &paFake{fc: pwcStep(tc.at, 0, 12, 135, 18), underway: &planUnderway{Lat: p.Stops[0].Lat, Lon: p.Stops[0].Lon}}
			l := paAssess(t, p, legs, f).Legs[0]
			if len(l.WindChanges.Changes) != tc.want || l.WindChanges.NotChecked != "" {
				t.Fatalf("wind changes = %+v, want %d", l.WindChanges, tc.want)
			}
			if tc.want == 1 && (l.WindChanges.Changes[0].Sector != "ahead" || !l.WindChanges.Changes[0].Prominent) {
				t.Fatalf("change = %+v, want it judged as any leg's", l.WindChanges.Changes[0])
			}
		})
	}
}

func TestAssessPlan_TheLegUnderWayWithNoDepartureTimeSaysItsETAIsUnknown(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	f := &paFake{fc: pwcStep(paAt(14, 7, 0), 0, 12, 135, 18), underway: &planUnderway{Lat: p.Stops[0].Lat, Lon: p.Stops[0].Lon}}
	l := paAssess(t, p, legs, f).Legs[0]
	if !strings.Contains(l.WindChanges.NotChecked, "no departure time") || len(l.WindChanges.Changes) != 0 {
		t.Fatalf("wind changes = %+v", l.WindChanges)
	}
}

func TestAssessPlan_ALegDoneOrSkippedShowsNoChanges(t *testing.T) {
	for _, progress := range []string{"done", "skipped"} {
		p, legs := paPlan(t, "2026-11-14", 0, 0)
		p.Stops[0].DepartTime = "09:00"
		f := &paFake{fc: pwcStep(paAt(14, 12, 0), 0, 12, 135, 18), progress: []string{progress}}
		l := paAssess(t, p, legs, f).Legs[0]
		if len(l.WindChanges.Changes) != 0 || l.WindChanges.NotChecked != "" {
			t.Errorf("%s leg: wind changes = %+v, want nothing", progress, l.WindChanges)
		}
	}
}

// ── lay-day nights ──────────────────────────────────────────────────────────

// pwcLayDay is midnight starting the lay day at the middle stop of the
// three-stop plan pwcNight assesses: two nights there, so the plan table has one
// lay-day row, the day after arrival. The night of arrival sits under the
// passage row and has no entry.
func pwcLayDay(t *testing.T) time.Time {
	t.Helper()
	p, _ := paPlan(t, "2026-11-14", 0, 2, 0)
	day, err := time.ParseInLocation("2006-01-02", p.Stops[1].Arrive, paLoc)
	if err != nil {
		t.Fatal(err)
	}
	return day.AddDate(0, 0, 1)
}

// pwcNight assesses that plan and returns the lay-day night's entry.
func pwcNight(t *testing.T, fc forecastSource) planNightWinds {
	t.Helper()
	p, legs := paPlan(t, "2026-11-14", 0, 2, 0)
	got := paAssess(t, p, legs, &paFake{fc: fc})
	date := pwcLayDay(t).Format("2006-01-02")
	// Beside it are the nights of arrival, which sit under the passage rows.
	n, ok := pwcNightAt(got, 1, date)
	if !ok {
		t.Fatalf("night winds = %+v, want stop 1 on %s", got.NightWinds, date)
	}
	return n
}

func TestAssessPlan_ALayDayNightIncludesAFourAMChangeTheNextMorning(t *testing.T) {
	day := pwcLayDay(t)
	at := day.Add(28 * time.Hour) // 4 AM the next day
	n := pwcNight(t, pwcStep(at, 0, 12, 135, 18))
	if len(n.Changes) != 1 {
		t.Fatalf("night = %+v, want the 4 AM change", n)
	}
	c := n.Changes[0]
	if c.From != "N" || c.To != "SE" || c.Sector != "" || c.FromSector != "" {
		t.Errorf("change = %+v: a night has no course, so no sector", c)
	}
	if want := at.In(paLoc).Format("Mon") + " 4 AM"; c.Label != want {
		t.Errorf("label = %q, want %q", c.Label, want)
	}
	// 18 kn is past the lowest marginal wind, 14.
	if !c.Prominent {
		t.Errorf("change = %+v, want prominent at the lowest marginal wind", c)
	}
}

func TestAssessPlan_ALayDayNightExcludesSevenInTheMorningAndSixInTheEvening(t *testing.T) {
	day := pwcLayDay(t)
	for name, at := range map[string]time.Time{
		"7 AM the next day":       day.Add(31 * time.Hour),
		"6 AM the next day":       day.Add(30 * time.Hour),
		"5 PM on the lay day":     day.Add(17 * time.Hour),
		"noon the day after next": day.Add(60 * time.Hour),
	} {
		n := pwcNight(t, pwcStep(at, 0, 12, 135, 18))
		if len(n.Changes) != 0 {
			t.Errorf("%s: night = %+v, want no change", name, n)
		}
	}
	n := pwcNight(t, pwcStep(day.Add(18*time.Hour), 0, 12, 135, 18))
	if len(n.Changes) != 1 || n.Changes[0].Label != "6 PM" {
		t.Errorf("a change at 18:00 is the night's first: %+v", n)
	}
}

func TestAssessPlan_ANightChangeBelowTheLowestMarginalWindIsNotProminent(t *testing.T) {
	day := pwcLayDay(t)
	n := pwcNight(t, pwcStep(day.Add(22*time.Hour), 0, 12, 135, 13))
	if len(n.Changes) != 1 || n.Changes[0].Prominent {
		t.Fatalf("night = %+v, want 13 kn not prominent against a 14 kn marginal", n)
	}
}

func TestAssessPlan_ANightThePlansForecastDoesNotReachIsNotChecked(t *testing.T) {
	day := pwcLayDay(t)
	// The forecast stops at 2 AM, four hours before the night ends.
	fc := pwcStepBetween(paAt(14, 0, 0), day.Add(26*time.Hour), day.Add(21*time.Hour), 0, 12, 135, 18)
	n := pwcNight(t, fc)
	if !strings.Contains(n.NotChecked, "2.00 AM") {
		t.Fatalf("night = %+v, want not checked, naming 2.00 AM", n)
	}
}

// The stop's night is read at the stop, once, and through the same forecast
// source the legs use, so the memo that backs it keeps one read per place.
func TestAssessPlan_TheNightsForecastIsReadOncePerStopThroughTheLegsSource(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 2, 0)
	verdictsOnly := &paFake{}
	assessPlan(context.Background(), p, legs, paDeps(verdictsOnly), planAssessOptions{VerdictsOnly: true})
	f := &paFake{}
	got := paAssess(t, p, legs, f)
	if len(got.NightWinds) != 3 {
		t.Fatalf("night winds = %+v", got.NightWinds)
	}
	// The middle stop is the end of both legs, so it is in the assessor already
	// and the night asks for no read of its own.
	if f.fetches != verdictsOnly.fetches || f.fetches == 0 {
		t.Fatalf("forecast reads = %d, want the %d the legs alone make", f.fetches, verdictsOnly.fetches)
	}
}

func TestAssessPlan_ANightAtAStopNoLegReadsIsReadOnceAtTheStop(t *testing.T) {
	// With no leg routed, nothing else reads the forecast, so only the night asks.
	p, _ := paPlan(t, "2026-11-14", 0, 2, 0)
	f := &paFake{}
	got := assessPlan(context.Background(), p, nil, paDeps(f), planAssessOptions{})
	if len(got.NightWinds) != 1 || f.fetches != 1 {
		t.Fatalf("night winds %+v after %d reads, want the one night from one read at its stop", got.NightWinds, f.fetches)
	}
}

// The table has a row for every lay day of the first stop, and at a later stop for
// those after the night of arrival: the assessment sends the nights of those rows.
func TestAssessPlan_NightsAreSentForTheRowsTheTableHas(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 2, 1, 0) // two nights at the first stop, one at the second
	got := paAssess(t, p, legs, &paFake{})
	var dates []string
	for _, n := range got.NightWinds {
		dates = append(dates, fmt.Sprintf("%d|%s", n.Stop, n.Date))
	}
	// The first stop's nights are the 14th and 15th; the second stop's one night is
	// the night of arrival, which the passage row carries, and the last stop's is the
	// night it arrives. Both are sent after the lay-day nights.
	if want := []string{"0|2026-11-14", "0|2026-11-15", "1|2026-11-16", "2|2026-11-17"};!sameStrings(dates, want) {
		t.Fatalf("nights = %v, want %v", dates, want)
	}
}

func TestAssessPlan_VerdictsOnlyLeavesTheWindChangesOut(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 2, 0)
	p.Stops[0].DepartTime = "08:00"
	got := assessPlan(context.Background(), p, legs, paDeps(&paFake{fc: pwcStep(paAt(14, 10, 0), 0, 12, 135, 18)}), planAssessOptions{VerdictsOnly: true})
	if len(got.Legs[0].WindChanges.Changes) != 0 || len(got.NightWinds) != 0 {
		t.Fatalf("a slip's verdicts are priced without the wind changes: %+v", got)
	}
}
