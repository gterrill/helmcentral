package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The passage plan's verdict column judges each leg with the same rules as
// Mate's assess_passage_window (assessItinerary's cell), at the operator's
// departure time or the best departure the ranking finds. These tests pin the
// clock to Saturday 14 November 2026, 4.30 AM at Brisbane (UTC+10), before first
// light, and give every leg 30 nm of route heading about 190 degrees true.

var paLoc = pwLoc

func paAt(day, hour, min int) time.Time {
	return time.Date(2026, time.November, day, hour, min, 0, 0, paLoc)
}

var paNow = paAt(14, 4, 30)

// paPlan is three stops on one coast: leave A on the 14th, a night at B, C on
// the 15th. legs are decorated as the handler decorates them.
func paPlan(t *testing.T, first string, nights ...int) (plan, []planLeg) {
	t.Helper()
	p := plan{ID: 1, Name: "Run", Stops: stopsOf(first, nights...)}
	p.Stops[0].Arrive = first
	d, err := decoratePlan(p, paNow)
	if err != nil {
		t.Fatal(err)
	}
	legs := make([]planLeg, len(d.Stops)-1)
	for i := range legs {
		a, b := d.Stops[i], d.Stops[i+1]
		legs[i] = planLeg{From: i, To: i + 1, DistanceNm: 30, CourseDeg: 190, Route: [][2]float64{{a.Lat, a.Lon}, {b.Lat, b.Lon}}}
	}
	return d, legs
}

// paFake records what the plan assessment asked of the ranking, the current and
// the warnings.
type paFake struct {
	mu       sync.Mutex
	rankings []planDepartureQuery
	// departures answers a ranking; the default is one departure an hour into the
	// window with a helping current.
	departures    func(q planDepartureQuery) ([]tidalDeparture, error)
	currentCalls  []time.Time
	current       func(q planDepartureQuery, dep time.Time) (planCurrentReading, error)
	fetches       int
	warnings      func(lat, lon float64) (forecastWarningsBundle, error)
	warningsOpen  int
	warningsReads int
	limits        passageLimits
	speed         float64
	speedErr      error
	fc            forecastSource
}

func paDeps(f *paFake) planAssessDeps {
	if f.speed == 0 && f.speedErr == nil {
		f.speed = 6
	}
	if f.fc.Fetch == nil {
		f.fc = pwUniform(10, 190, 12, nil, 0, paAt(14, 0, 0), paAt(24, 23, 0))
	}
	inner := f.fc.Fetch
	f.fc.Fetch = func(ctx context.Context, lat, lon float64) (passageForecast, error) {
		f.mu.Lock()
		f.fetches++
		f.mu.Unlock()
		return inner(ctx, lat, lon)
	}
	if f.limits == (passageLimits{}) {
		f.limits = pwLimitsAll()
	}
	return planAssessDeps{
		now:    func() time.Time { return paNow },
		limits: func() (passageLimits, error) { return f.limits, nil },
		sources: func(zone func() (string, error), now time.Time) (passageSources, error) {
			return passageSources{fc: f.fc, tides: pwNoTides, fallbackModel: func() string { return "" }}, nil
		},
		speed: func(context.Context) (float64, error) { return f.speed, f.speedErr },
		sun:   pwSun,
		dawn: func(date time.Time, lat, lon float64) (time.Time, bool) {
			y, m, d := date.Date()
			return time.Date(y, m, d, 5, 15, 0, 0, date.Location()), true
		},
		departures: func(ctx context.Context, q planDepartureQuery) ([]tidalDeparture, error) {
			f.mu.Lock()
			f.rankings = append(f.rankings, q)
			f.mu.Unlock()
			if f.departures != nil {
				return f.departures(q)
			}
			return []tidalDeparture{{Depart: q.Earliest.Add(time.Hour), Current: planCurrentReading{Have: true, MeanKn: 0.4}}}, nil
		},
		currentAlong: func(ctx context.Context, q planDepartureQuery, dep time.Time) (planCurrentReading, error) {
			f.mu.Lock()
			f.currentCalls = append(f.currentCalls, dep)
			f.mu.Unlock()
			if f.current != nil {
				return f.current(q, dep)
			}
			return planCurrentReading{}, nil
		},
		warnings: func(now time.Time) (planWarningsReader, error) {
			f.mu.Lock()
			f.warningsOpen++
			f.mu.Unlock()
			return func(lat, lon float64) (forecastWarningsBundle, error) {
				f.mu.Lock()
				f.warningsReads++
				f.mu.Unlock()
				if f.warnings != nil {
					return f.warnings(lat, lon)
				}
				return forecastWarningsBundle{}, nil
			}, nil
		},
	}
}

func paAssess(t *testing.T, p plan, legs []planLeg, f *paFake) planAssessment {
	t.Helper()
	return assessPlan(context.Background(), p, legs, paDeps(f))
}

func paParse(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("%q is not a time: %v", s, err)
	}
	return v
}

// paWind is a forecast whose wind (and gusts equal to it) is windKn(hour) for
// every hour of the 14th to the 24th, from dead ahead of the 190 degree legs.
func paWind(windKn func(t time.Time) float64) forecastSource {
	hours := map[int64]passageHour{}
	for h := paAt(14, 0, 0); !h.After(paAt(24, 23, 0)); h = h.Add(time.Hour) {
		w := windKn(h)
		hours[h.Unix()] = passageHour{WindMS: kn(w), GustMS: kn(w), WindDirDeg: 190}
	}
	return forecastSource{Provider: "wx", Fetch: func(context.Context, float64, float64) (passageForecast, error) {
		return passageForecast{Hours: hours}, nil
	}}
}

// paDeparture is a ranked departure on the 14th with the current beside it.
func paDeparture(hour, min int, currentKn float64) tidalDeparture {
	return tidalDeparture{Depart: paAt(14, hour, min), Current: planCurrentReading{Have: true, MeanKn: currentKn}}
}

func TestAssessPlan_AGoLegAtTheOperatorsTime(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[0].DepartTime = "08:00"
	got := paAssess(t, p, legs, &paFake{})
	l := got.Legs[0]
	if l.Verdict != "go" || l.DepartSuggested || l.Note != "" {
		t.Fatalf("leg = %+v", l)
	}
	if !paParse(t, l.Depart).Equal(paAt(14, 8, 0)) || !paParse(t, l.Arrive).Equal(paAt(14, 13, 0)) {
		t.Fatalf("depart %s arrive %s: 30 nm at 6 kn is 5 hours", l.Depart, l.Arrive)
	}
	if l.Hours == nil || *l.Hours != 5 || l.SpeedKn == nil || *l.SpeedKn != 6 {
		t.Fatalf("hours %v speed %v", l.Hours, l.SpeedKn)
	}
	if got.Problem != "" {
		t.Fatalf("problem = %q", got.Problem)
	}
}

func TestAssessPlan_NoGoNamesTheLimitingFactor(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[0].DepartTime = "08:00"
	// Wind 18 kn is inside the 20 kn ahead limit; a 32 kn gust is past its 30.
	fc := pwUniform(18, 190, 32, nil, 0, paAt(14, 0, 0), paAt(24, 23, 0))
	l := paAssess(t, p, legs, &paFake{fc: fc}).Legs[0]
	if l.Verdict != "no_go" || l.Limiting != "gusts 32 kn ahead" {
		t.Fatalf("leg = %+v", l)
	}
}

func TestAssessPlan_MarginalIsFromSeventyPercentOfNoGo(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[0].DepartTime = "08:00"
	// 15 kn ahead is past 0.7 of 20 and short of 20.
	fc := pwUniform(15, 190, 15, nil, 0, paAt(14, 0, 0), paAt(24, 23, 0))
	l := paAssess(t, p, legs, &paFake{fc: fc}).Legs[0]
	if l.Verdict != "marginal" || l.Limiting != "wind 15 kn ahead" {
		t.Fatalf("leg = %+v", l)
	}
}

// ── the suggested departure: not a No-Go, Go before Marginal, then the current ──

func TestAssessPlan_ASuggestedDepartureIsMarkedAndAssessedAtItsTime(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	f := &paFake{departures: func(planDepartureQuery) ([]tidalDeparture, error) {
		return []tidalDeparture{paDeparture(9, 30, 0.6)}, nil
	}}
	l := paAssess(t, p, legs, f).Legs[0]
	if !l.DepartSuggested || !paParse(t, l.Depart).Equal(paAt(14, 9, 30)) || !paParse(t, l.Arrive).Equal(paAt(14, 14, 30)) {
		t.Fatalf("leg = %+v", l)
	}
	if l.Verdict != "go" {
		t.Fatalf("verdict = %q", l.Verdict)
	}
	if len(f.rankings) != 1 {
		t.Fatalf("ranking asked %d times", len(f.rankings))
	}
	q := f.rankings[0]
	if q.Hours != 5 || len(q.Route) != 2 {
		t.Fatalf("query = %+v", q)
	}
	// First light is 5.15 AM; with no daylight arrival limit the last departure
	// is still in daylight, at sunset 5.32 PM.
	if !q.Earliest.Equal(paAt(14, 5, 15)) || !q.Latest.Equal(paAt(14, 17, 30)) {
		t.Fatalf("window %s to %s", q.Earliest, q.Latest)
	}
}

func TestAssessPlan_TheSuggestionSkipsDeparturesThatAreNoGo(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	// 25 kn dead ahead until noon (past the 20 kn limit), 10 kn after.
	fc := paWind(func(h time.Time) float64 {
		if h.Before(paAt(14, 12, 0)) {
			return 25
		}
		return 10
	})
	// The fairest current is at 8.00, which runs into the 25 kn; 11.00 does too.
	f := &paFake{fc: fc, departures: func(planDepartureQuery) ([]tidalDeparture, error) {
		return []tidalDeparture{paDeparture(8, 0, 0.9), paDeparture(11, 0, 0.5), paDeparture(13, 0, 0.3)}, nil
	}}
	l := paAssess(t, p, legs, f).Legs[0]
	if !paParse(t, l.Depart).Equal(paAt(14, 13, 0)) || l.Verdict != "go" || !l.DepartSuggested {
		t.Fatalf("leg = %+v: 8.00 and 11.00 are No-Go, 1.00 PM is the first that is not", l)
	}
	if l.CurrentKn == nil || *l.CurrentKn != 0.3 {
		t.Fatalf("the current is the one read for the chosen departure: %v", l.CurrentKn)
	}
	if len(f.currentCalls) != 0 {
		t.Fatalf("a suggested leg reads its current with the ranking, not again: %v", f.currentCalls)
	}
}

func TestAssessPlan_TheSuggestionPrefersGoToMarginalEvenOnAWorseCurrent(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	// 16 kn ahead is marginal (from 14) until noon, then 8 kn is a Go.
	fc := paWind(func(h time.Time) float64 {
		if h.Before(paAt(14, 12, 0)) {
			return 16
		}
		return 8
	})
	f := &paFake{fc: fc, departures: func(planDepartureQuery) ([]tidalDeparture, error) {
		return []tidalDeparture{paDeparture(8, 0, 1.0), paDeparture(13, 0, 0.1)}, nil
	}}
	l := paAssess(t, p, legs, f).Legs[0]
	if !paParse(t, l.Depart).Equal(paAt(14, 13, 0)) || l.Verdict != "go" {
		t.Fatalf("leg = %+v: a Go at 1.00 PM beats a Marginal with the fairer current", l)
	}
}

func TestAssessPlan_TheSuggestionTakesTheFairestCurrentAmongEqualVerdicts(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	f := &paFake{departures: func(planDepartureQuery) ([]tidalDeparture, error) {
		return []tidalDeparture{paDeparture(9, 0, 0.9), paDeparture(13, 0, 0.1)}, nil
	}}
	l := paAssess(t, p, legs, f).Legs[0]
	if !paParse(t, l.Depart).Equal(paAt(14, 9, 0)) || l.Verdict != "go" {
		t.Fatalf("leg = %+v: both are Go, so the fairer current wins", l)
	}
}

func TestAssessPlan_AMarginalSuggestionBeatsANoGoOne(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	fc := paWind(func(h time.Time) float64 {
		if h.Before(paAt(14, 12, 0)) {
			return 25
		}
		return 16
	})
	f := &paFake{fc: fc, departures: func(planDepartureQuery) ([]tidalDeparture, error) {
		return []tidalDeparture{paDeparture(8, 0, 0.9), paDeparture(13, 0, 0.1)}, nil
	}}
	l := paAssess(t, p, legs, f).Legs[0]
	if !paParse(t, l.Depart).Equal(paAt(14, 13, 0)) || l.Verdict != "marginal" {
		t.Fatalf("leg = %+v", l)
	}
}

func TestAssessPlan_WhenEveryDepartureIsNoGoTheLeastBadIsShownAsNoGo(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	// 28 kn ahead until noon, 22 kn after: both are past the 20 kn limit, the
	// afternoon by less.
	fc := paWind(func(h time.Time) float64 {
		if h.Before(paAt(14, 12, 0)) {
			return 28
		}
		return 22
	})
	f := &paFake{fc: fc, departures: func(planDepartureQuery) ([]tidalDeparture, error) {
		return []tidalDeparture{paDeparture(8, 0, 0.9), paDeparture(13, 0, 0.1)}, nil
	}}
	l := paAssess(t, p, legs, f).Legs[0]
	if !paParse(t, l.Depart).Equal(paAt(14, 13, 0)) || !l.DepartSuggested {
		t.Fatalf("leg = %+v: 1.00 PM is the least bad", l)
	}
	if l.Verdict != "no_go" || l.Limiting != "wind 22 kn ahead" {
		t.Fatalf("leg = %+v: the No-Go is shown, never hidden", l)
	}
}

func TestAssessPlan_TheRankingsFirstWindowIsOneDeparturePerQuarterHourAndOneCall(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	f := &paFake{}
	paAssess(t, p, legs, f)
	if len(f.rankings) != 2 {
		t.Fatalf("two legs, two rankings, got %d", len(f.rankings))
	}
}

func TestAssessPlan_ADepartureInThePastIsNotSuggested(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	f := &paFake{}
	d := paDeps(f)
	d.now = func() time.Time { return paAt(14, 8, 7) }
	assessPlan(context.Background(), p, legs, d)
	if len(f.rankings) != 1 || !f.rankings[0].Earliest.Equal(paAt(14, 8, 15)) {
		t.Fatalf("window = %+v: no earlier than the next quarter hour", f.rankings)
	}
}

func TestAssessPlan_TheWindowEndsWhereArrivalStillMakesDaylight(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	f := &paFake{limits: func() passageLimits { l := pwLimitsAll(); l.DaylightArrival = true; return l }()}
	paAssess(t, p, legs, f)
	if len(f.rankings) != 1 {
		t.Fatalf("ranking asked %d times", len(f.rankings))
	}
	// Sunset 5.32 PM less 5 hours is 12.32 PM, taken down to the quarter hour.
	if want := paAt(14, 12, 30); !f.rankings[0].Latest.Equal(want) {
		t.Fatalf("latest = %s, want %s", f.rankings[0].Latest, want)
	}
}

func TestAssessPlan_NoDaylightWindowHasNoTimeAndNoVerdict(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	// 15 hours under way cannot arrive by sunset after leaving at first light.
	f := &paFake{speed: 2, limits: func() passageLimits { l := pwLimitsAll(); l.DaylightArrival = true; return l }()}
	l := paAssess(t, p, legs, f).Legs[0]
	if l.Note != "No daylight window" || l.NoteKind != planKindNoWindow || l.Depart != "" || l.Verdict != "" || l.DepartSuggested {
		t.Fatalf("leg = %+v", l)
	}
	if len(f.rankings) != 0 {
		t.Fatal("nothing to rank when no departure fits")
	}
}

func TestAssessPlan_AnOperatorTimeIsNeverReplaced(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	p.Stops[0].DepartTime = "14:00" // arrives 7 PM, after sunset: judged, never moved
	f := &paFake{limits: func() passageLimits { l := pwLimitsAll(); l.DaylightArrival = true; return l }()}
	l := paAssess(t, p, legs, f).Legs[0]
	if len(f.rankings) != 0 || l.DepartSuggested || !paParse(t, l.Depart).Equal(paAt(14, 14, 0)) {
		t.Fatalf("leg = %+v, asked %d", l, len(f.rankings))
	}
	if l.Verdict != "no_go" || !strings.Contains(l.Limiting, "sunset") {
		t.Fatalf("arriving at 3 AM is a no-go on daylight: %+v", l)
	}
}

func TestAssessPlan_ARankingThatFailsSaysSoAndGivesNoTime(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	f := &paFake{departures: func(planDepartureQuery) ([]tidalDeparture, error) {
		return nil, errors.New("no current and no flood set")
	}}
	l := paAssess(t, p, legs, f).Legs[0]
	if !strings.HasPrefix(l.Note, "No departure suggested") || !strings.Contains(l.Note, "no current and no flood set") || l.Depart != "" || l.Verdict != "" || l.NoteKind != planKindNoDeparture {
		t.Fatalf("leg = %+v", l)
	}
}

func TestAssessPlan_ARankingWithNoDepartureSaysSo(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	f := &paFake{departures: func(planDepartureQuery) ([]tidalDeparture, error) { return nil, nil }}
	l := paAssess(t, p, legs, f).Legs[0]
	if !strings.HasPrefix(l.Note, "No departure suggested") || l.Depart != "" || l.Verdict != "" {
		t.Fatalf("leg = %+v", l)
	}
}

func TestAssessPlan_ALegBeyondTheForecastHasNoForecastAndNoVerdict(t *testing.T) {
	p, legs := paPlan(t, "2026-11-30", 0, 1, 0)
	p.Stops[0].DepartTime = "08:00"
	f := &paFake{}
	got := paAssess(t, p, legs, f)
	l := got.Legs[0]
	if l.Note != "No forecast yet" || l.NoteKind != planKindNoForecast || l.Verdict != "" {
		t.Fatalf("leg = %+v", l)
	}
	if f.fetches != 0 || len(f.rankings) != 0 || len(f.currentCalls) != 0 {
		t.Fatalf("fetched %d, ranked %d: a day past the horizon buys nothing", f.fetches, len(f.rankings))
	}
	if !paParse(t, l.Depart).Equal(time.Date(2026, 11, 30, 8, 0, 0, 0, paLoc)) {
		t.Fatalf("the operator's time still shows: %s", l.Depart)
	}
}

func TestAssessPlan_ArrivalPastTheLastForecastHourIsNoForecastYet(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[0].DepartTime = "21:00"
	// The forecast ends at 11 PM; the leg runs to 2 AM.
	fc := pwUniform(10, 190, 12, nil, 0, paAt(14, 0, 0), paAt(14, 23, 0))
	l := paAssess(t, p, legs, &paFake{fc: fc}).Legs[0]
	if l.Note != "No forecast yet" || l.NoteKind != planKindNoForecast || l.Verdict != "" {
		t.Fatalf("leg = %+v", l)
	}
}

func TestAssessPlan_AHoleInsideTheForecastIsNoData(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[0].DepartTime = "08:00"
	hours := map[int64]passageHour{}
	for h := paAt(14, 0, 0); !h.After(paAt(24, 23, 0)); h = h.Add(time.Hour) {
		if h.Equal(paAt(14, 10, 0)) {
			continue
		}
		hours[h.Unix()] = passageHour{WindMS: kn(10), GustMS: kn(12), WindDirDeg: 190}
	}
	fc := forecastSource{Provider: "wx", Fetch: func(context.Context, float64, float64) (passageForecast, error) {
		return passageForecast{Hours: hours}, nil
	}}
	l := paAssess(t, p, legs, &paFake{fc: fc}).Legs[0]
	if l.Verdict != "no_data" || l.Note != "Forecast missing for part of the leg" || l.NoteKind != planKindGap {
		t.Fatalf("leg = %+v", l)
	}
}

// ── current ─────────────────────────────────────────────────────────────

func TestAssessPlan_CurrentIsTheReadingAtTheDepartureUsed(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	p.Stops[0].DepartTime = "08:00"
	var sawDep time.Time
	f := &paFake{current: func(q planDepartureQuery, dep time.Time) (planCurrentReading, error) {
		sawDep = dep
		return planCurrentReading{Have: true, MeanKn: -0.8}, nil
	}}
	l := paAssess(t, p, legs, f).Legs[0]
	if l.CurrentKn == nil || *l.CurrentKn != -0.8 || !sawDep.Equal(paAt(14, 8, 0)) {
		t.Fatalf("current %v at %s", l.CurrentKn, sawDep)
	}
}

func TestAssessPlan_NoCurrentReadingIsNilWithItsReasonNeverZero(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[0].DepartTime = "08:00"
	f := &paFake{current: func(planDepartureQuery, time.Time) (planCurrentReading, error) {
		return planCurrentReading{Why: "the model has no current near shore"}, nil
	}}
	l := paAssess(t, p, legs, f).Legs[0]
	if l.CurrentKn != nil || l.CurrentNote != "the model has no current near shore" {
		t.Fatalf("leg = %+v", l)
	}
	f = &paFake{current: func(planDepartureQuery, time.Time) (planCurrentReading, error) {
		return planCurrentReading{}, errors.New("the Open-Meteo current request failed")
	}}
	l = paAssess(t, p, legs, f).Legs[0]
	if l.CurrentKn != nil || !strings.Contains(l.CurrentNote, "request failed") {
		t.Fatalf("leg = %+v", l)
	}
	if l.Verdict != "go" {
		t.Fatalf("a current that cannot be read does not stop the verdict: %+v", l)
	}
}

func TestAssessPlan_ASuggestedLegWithNoModelCurrentHasNoCurrentAndTheReason(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	f := &paFake{departures: func(planDepartureQuery) ([]tidalDeparture, error) {
		return []tidalDeparture{{Depart: paAt(14, 9, 0), Current: planCurrentReading{Why: "the model has a current for only 30% of the leg"}}}, nil
	}}
	l := paAssess(t, p, legs, f).Legs[0]
	if l.CurrentKn != nil || l.CurrentNote != "the model has a current for only 30% of the leg" || l.Verdict != "go" {
		t.Fatalf("leg = %+v", l)
	}
}

// ── the ETA speed, limits and the forecast ──────────────────────────────

func TestAssessPlan_NoRecordedPassagesMeansNoEtaAndNoVerdictButTheTimeStays(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[0].DepartTime = "08:00"
	f := &paFake{speedErr: errors.New("no recorded passages yet")}
	got := paAssess(t, p, legs, f)
	l := got.Legs[0]
	if l.Verdict != "" || l.Arrive != "" || l.Hours != nil || l.Note != "No ETA: no recorded passages yet" || l.NoteKind != planKindProblem {
		t.Fatalf("leg = %+v", l)
	}
	if got.Problem != "No ETA: no recorded passages yet" {
		t.Fatalf("problem = %q: said once, above the table", got.Problem)
	}
	if !paParse(t, l.Depart).Equal(paAt(14, 8, 0)) {
		t.Fatalf("depart = %q", l.Depart)
	}
	if len(f.rankings) != 0 || len(f.currentCalls) != 0 || f.fetches != 0 {
		t.Fatal("nothing is asked for a leg with no ETA")
	}
}

func TestAssessPlan_LimitsNotSetIsAProblemNotAGuess(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[0].DepartTime = "08:00"
	f := &paFake{}
	d := paDeps(f)
	d.limits = func() (passageLimits, error) { return passageLimits{}, nil }
	got := assessPlan(context.Background(), p, legs, d)
	l := got.Legs[0]
	if !strings.Contains(got.Problem, "Passage limits") || l.Verdict != "" || !strings.Contains(l.Note, "Passage limits") || l.NoteKind != planKindProblem {
		t.Fatalf("got %+v", got)
	}
	if len(f.currentCalls) != 1 {
		t.Fatalf("the current is still read for the operator's time: %v", f.currentCalls)
	}
	if f.fetches != 0 {
		t.Fatal("no forecast is read when there are no limits to judge it by")
	}
}

func TestAssessPlan_AForecastThatFailsIsAProblemOnEveryLeg(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[0].DepartTime = "08:00"
	p.Stops[1].DepartTime = "08:00"
	fc := forecastSource{Provider: "wx", Fetch: func(context.Context, float64, float64) (passageForecast, error) {
		return passageForecast{}, errors.New("weather provider is down")
	}}
	got := paAssess(t, p, legs, &paFake{fc: fc})
	if !strings.Contains(got.Problem, "weather provider is down") {
		t.Fatalf("problem = %q", got.Problem)
	}
	for _, l := range got.Legs {
		if l.Verdict != "" || !strings.Contains(l.Note, "weather provider is down") || l.NoteKind != planKindProblem {
			t.Fatalf("leg = %+v", l)
		}
	}
}

func TestAssessPlan_AnUnroutedLegHasNoVerdictAndSaysWhy(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	legs[0] = planLeg{From: 0, To: 1, Error: "no passage distance for A to B: x", Reason: "x", Route: [][2]float64{}}
	got := paAssess(t, p, legs, &paFake{})
	if l := got.Legs[0]; l.Verdict != "" || l.Note != "Not routed" || l.NoteKind != planKindNotRouted || l.Depart != "" {
		t.Fatalf("leg = %+v", l)
	}
	if got.Legs[1].Verdict == "" {
		t.Fatalf("the next leg is judged on its own: %+v", got.Legs[1])
	}
}

func TestAssessPlan_ADayThatHasPassedIsNotJudged(t *testing.T) {
	p, legs := paPlan(t, "2026-11-10", 0, 1, 0)
	p.Stops[0].DepartTime = "08:00"
	f := &paFake{}
	l := paAssess(t, p, legs, f).Legs[0]
	if l.Note != "Date has passed" || l.NoteKind != planKindPast || l.Verdict != "" {
		t.Fatalf("leg = %+v", l)
	}
	if f.fetches != 0 {
		t.Fatal("nothing is fetched for a day that has gone")
	}
}

func TestAssessPlan_AnOvernightLegIsNotAssessedYet(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	legs[0].DistanceNm = 200
	p.Stops[0].DepartTime = "08:00"
	l := paAssess(t, p, legs, &paFake{}).Legs[0]
	if l.Note != "Overnight legs are not assessed yet" || l.NoteKind != planKindOvernight || l.Verdict != "" {
		t.Fatalf("leg = %+v", l)
	}
}

func TestAssessPlan_ForecastTimeIsTheOldestSourceTime(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[0].DepartTime = "08:00"
	inner := pwUniform(10, 190, 12, nil, 0, paAt(14, 0, 0), paAt(24, 23, 0))
	fetched := paAt(14, 5, 12)
	fc := forecastSource{Provider: "wx", Fetch: func(ctx context.Context, lat, lon float64) (passageForecast, error) {
		f, err := inner.Fetch(ctx, lat, lon)
		f.FetchedAt = fetched
		return f, err
	}}
	got := paAssess(t, p, legs, &paFake{fc: fc})
	if !paParse(t, got.ForecastAsOf).Equal(fetched) || !paParse(t, got.AsOf).Equal(paNow) {
		t.Fatalf("as_of %s forecast_as_of %s", got.AsOf, got.ForecastAsOf)
	}
}

func TestAssessPlan_WavesNotCheckedIsReportedButWindAgainstTideIsNot(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[0].DepartTime = "08:00"
	l := paAssess(t, p, legs, &paFake{}).Legs[0]
	if !pwHas(l.NotChecked, "wave") || pwHas(l.NotChecked, "wind_against_tide") {
		t.Fatalf("not_checked = %v", l.NotChecked)
	}
}

// ── how the work is shared out ──────────────────────────────────────────

func TestAssessPlan_LegsAreRankedThreeAtATime(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 1, 1, 0)
	var mu sync.Mutex
	running, most := 0, 0
	f := &paFake{departures: func(q planDepartureQuery) ([]tidalDeparture, error) {
		mu.Lock()
		running++
		if running > most {
			most = running
		}
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		mu.Lock()
		running--
		mu.Unlock()
		return []tidalDeparture{{Depart: q.Earliest.Add(time.Hour), Current: planCurrentReading{Have: true, MeanKn: 0.2}}}, nil
	}}
	got := paAssess(t, p, legs, f)
	if len(got.Legs) != 4 || len(f.rankings) != 4 {
		t.Fatalf("%d legs, %d rankings", len(got.Legs), len(f.rankings))
	}
	if most != planLegWorkers {
		t.Fatalf("%d rankings ran at once, want the pool of %d filled and not passed", most, planLegWorkers)
	}
}

func TestAssessPlan_TheForecastIsFetchedWhileTheRankingRuns(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	rankStarted := make(chan struct{})
	var once sync.Once
	var fetchSawRanking atomic.Bool
	inner := pwUniform(10, 190, 12, nil, 0, paAt(14, 0, 0), paAt(24, 23, 0))
	fc := forecastSource{Provider: "wx", Fetch: func(ctx context.Context, lat, lon float64) (passageForecast, error) {
		// A forecast fetch that waited for the ranking to finish would wait here
		// for ever: the ranking is not asked until the fetch is under way.
		select {
		case <-rankStarted:
			fetchSawRanking.Store(true)
		case <-time.After(2 * time.Second):
		}
		return inner.Fetch(ctx, lat, lon)
	}}
	f := &paFake{fc: fc, departures: func(q planDepartureQuery) ([]tidalDeparture, error) {
		once.Do(func() { close(rankStarted) })
		return []tidalDeparture{{Depart: q.Earliest.Add(time.Hour), Current: planCurrentReading{Have: true, MeanKn: 0.2}}}, nil
	}}
	paAssess(t, p, legs, f)
	if !fetchSawRanking.Load() {
		t.Fatal("the ranking waited for the forecast")
	}
}

// ── stop rows: warnings ─────────────────────────────────────────────────

func paWarning(day, kind string) forecastWarningsBundle {
	return forecastWarningsBundle{Region: "QLD", Bulletins: []forecastWarningBulletin{{
		ID: "1", Title: "Strong wind", Category: "wind", DetailsURL: "https://example.test/w",
		Sections: []forecastWarningSection{{Day: day, WarningType: kind}},
	}}}
}

func TestAssessPlan_AWarningCoveringTheNightIsOnTheStopRow(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	// B is reached on the 14th and left on the 15th: its one night is the 14th.
	night := time.Date(2026, 11, 14, 0, 0, 0, 0, paLoc).Weekday().String()
	f := &paFake{warnings: func(lat, lon float64) (forecastWarningsBundle, error) { return paWarning(night, "Gale Warning"), nil }}
	got := paAssess(t, p, legs, f)
	var b *planStopAssessment
	for i := range got.Stops {
		if got.Stops[i].Stop == 1 {
			b = &got.Stops[i]
		}
	}
	if b == nil || len(b.Nights) != 1 {
		t.Fatalf("stops = %+v", got.Stops)
	}
	n := b.Nights[0]
	if n.Date != "2026-11-14" || n.Warning != "Gale Warning" || n.URL != "https://example.test/w" {
		t.Fatalf("night = %+v", n)
	}
}

func TestAssessPlan_AWarningForAnotherDayLeavesTheStopRowEmpty(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	other := time.Date(2026, 11, 17, 0, 0, 0, 0, paLoc).Weekday().String()
	f := &paFake{warnings: func(lat, lon float64) (forecastWarningsBundle, error) { return paWarning(other, "Gale Warning"), nil }}
	for _, s := range paAssess(t, p, legs, f).Stops {
		for _, n := range s.Nights {
			if n.Warning != "" {
				t.Fatalf("night = %+v", n)
			}
		}
	}
}

func TestAssessPlan_WarningsThatCannotBeReadAreSaidOnceNotTakenForNone(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 1, 0)
	f := &paFake{warnings: func(lat, lon float64) (forecastWarningsBundle, error) {
		return forecastWarningsBundle{}, errors.New("forecast warnings provider \"bom\" unavailable")
	}}
	got := paAssess(t, p, legs, f)
	if !strings.Contains(got.WarningsError, "unavailable") || f.warningsReads != 1 {
		t.Fatalf("warnings_error %q after %d reads", got.WarningsError, f.warningsReads)
	}
}

func TestAssessPlan_TheWarningsProviderIsResolvedOnceAndAPlaceIsReadOnce(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 1, 0)
	// Two nights at places a few metres apart are one place to the warnings.
	p.Stops[2].Lat, p.Stops[2].Lon = p.Stops[1].Lat+0.0001, p.Stops[1].Lon
	f := &paFake{}
	paAssess(t, p, legs, f)
	if f.warningsOpen != 1 || f.warningsReads != 1 {
		t.Fatalf("provider resolved %d times, place read %d times, want 1 and 1", f.warningsOpen, f.warningsReads)
	}
}

func TestAssessPlan_NoNightInTheComingWeekMeansNoWarningsRead(t *testing.T) {
	p, legs := paPlan(t, "2026-11-30", 0, 1, 0)
	f := &paFake{}
	paAssess(t, p, legs, f)
	if f.warningsOpen != 0 || f.warningsReads != 0 {
		t.Fatalf("the provider was resolved %d times and read %d times for nights beyond the week", f.warningsOpen, f.warningsReads)
	}
}

// ── the assessor's own answers the plan reads ───────────────────────────

func TestCell_LimitingCarriesAShortPhraseAndTheCardSentenceIsUnchanged(t *testing.T) {
	sec := func(s *passageWindowLimiting) string {
		if s == nil || s.Sector == nil {
			return ""
		}
		return *s.Sector
	}
	waveLimits := pwLimitsAll()
	waveLimits.Ahead.WaveMNoGo = pwF(1.5)
	cases := []struct {
		name        string
		limits      passageLimits
		fc          forecastSource
		sun         func(time.Time, float64, float64) (time.Time, time.Time, bool)
		short, text string
		sector      string
	}{
		// The legs run north, so a wind from the north is dead ahead.
		{"gust", pwLimitsAll(), pwUniform(18, 0, 32, nil, 0, pwDay(19, 0), pwDay(25, 23)), pwNoSun,
			"gusts 32 kn ahead", "Gusts 32 kn from N, ahead · Alpha → Bravo", "ahead"},
		{"wind marginal", pwLimitsAll(), pwUniform(15, 0, 15, nil, 0, pwDay(19, 0), pwDay(25, 23)), pwNoSun,
			"wind 15 kn ahead", "Wind 15 kn from N, ahead (marginal from 14) · Alpha → Bravo", "ahead"},
		{"wave with direction", waveLimits, pwUniform(5, 0, 5, pwF(2.1), 0, pwDay(19, 0), pwDay(25, 23)), pwNoSun,
			"waves 2.1 m ahead", "Waves 2.1 m ahead · Alpha → Bravo", "ahead"},
	}
	for _, c := range cases {
		a := pwArgs([]string{"2026-10-20"}, "06:00", pwNorthLeg())
		a.Legs[0].PassageHours = pwF(2)
		p, err := assessItinerary(context.Background(), a, c.limits, pwNow, c.fc, pwNoTides, c.sun)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		l := pwCell0(t, p).Limiting
		if l == nil || l.Short != c.short || l.Text != c.text || sec(l) != c.sector {
			t.Errorf("%s: limiting = %+v, want short %q and text %q", c.name, l, c.short, c.text)
		}
	}
}

func TestCell_AMissingHourPastTheForecastIsNotAGap(t *testing.T) {
	// The week's forecast ends on the 25th; a leg on the 27th is ahead of it.
	a := pwArgs([]string{"2026-10-27"}, "06:00", pwNorthLeg())
	p := pwAssess(t, a, pwLimitsAll(), pwWeek(5, 180))
	if c := pwCell0(t, p); c.State != "no_data" || c.noData != passageNoDataBeyondForecast {
		t.Fatalf("cell = %+v", c)
	}
}

func TestCell_AMissingHourInsideTheForecastIsAGap(t *testing.T) {
	hours := map[int64]passageHour{}
	for h := pwDay(19, 0); !h.After(pwDay(25, 23)); h = h.Add(time.Hour) {
		if h.Equal(pwDay(20, 8)) {
			continue
		}
		hours[h.Unix()] = passageHour{WindMS: kn(5), GustMS: kn(5), WindDirDeg: 180}
	}
	fc := forecastSource{Provider: "wx", Fetch: func(context.Context, float64, float64) (passageForecast, error) {
		return passageForecast{Hours: hours}, nil
	}}
	a := pwArgs([]string{"2026-10-20"}, "06:00", pwNorthLeg())
	p := pwAssess(t, a, pwLimitsAll(), fc)
	if c := pwCell0(t, p); c.State != "no_data" || c.noData != passageNoDataGap {
		t.Fatalf("cell = %+v", c)
	}
}

func TestCapRoutePoints_KeepsTheEndsAndTheBends(t *testing.T) {
	var pts []passageRoutePoint
	// A long straight run south with one bend east at -26.5.
	for i := 0; i <= 20; i++ {
		pts = append(pts, passageRoutePoint{Lat: -25 - float64(i)*0.075, Lon: 153})
	}
	for i := 1; i <= 20; i++ {
		pts = append(pts, passageRoutePoint{Lat: -26.5, Lon: 153 + float64(i)*0.075})
	}
	got := capRoutePoints(pts, passageWindowMaxRoutePoints)
	if len(got) > passageWindowMaxRoutePoints || len(got) < 3 || got[0] != pts[0] || got[len(got)-1] != pts[len(pts)-1] {
		t.Fatalf("got %d points, ends %v %v", len(got), got[0], got[len(got)-1])
	}
	bend := false
	for _, p := range got {
		if p.Lat == -26.5 && p.Lon == 153 {
			bend = true
		}
	}
	if !bend {
		t.Fatalf("the bend was dropped: %v", got)
	}
	short := pts[:5]
	if len(capRoutePoints(short, passageWindowMaxRoutePoints)) != 5 {
		t.Fatal("a short route is left alone")
	}
}

func TestPlanPassageLeg_BoundsEveryRouteToTheSameNumberOfPoints(t *testing.T) {
	var pts []passageRoutePoint
	for i := 0; i < 30; i++ {
		pts = append(pts, passageRoutePoint{Lat: -25 - float64(i)*0.1, Lon: 153 + float64(i%3)*0.05})
	}
	hours := 8.0
	got := planPassageLeg(0, passageLegArgs{Route: pts, PassageHours: &hours})
	if len(got.segs) > passageWindowMaxRoutePoints-1 {
		t.Fatalf("%d segments from a 30 point route", len(got.segs))
	}
	ten := pts[:passageWindowMaxRoutePoints]
	if again := planPassageLeg(0, passageLegArgs{Route: ten, PassageHours: &hours}); len(again.segs) != len(ten)-1 {
		t.Fatalf("a route inside the bound lost points: %d segments", len(again.segs))
	}
}

// ── the endpoint ────────────────────────────────────────────────────────

func TestPlanAssessmentHandler_ServesTheAssessment(t *testing.T) {
	store := withTestDocumentStore(t)
	prevSeg, prevDeps := planRouteSegment, planAssessDepsFor
	t.Cleanup(func() { planRouteSegment, planAssessDepsFor = prevSeg, prevDeps })
	planRouteSegment = func(_ context.Context, aLat, aLon, bLat, bLon float64) (waterRoute, error) {
		return routeOf(30, [2]float64{aLat, aLon}, [2]float64{bLat, bLon}), nil
	}
	f := &paFake{}
	planAssessDepsFor = func() planAssessDeps {
		d := paDeps(f)
		d.now = func() time.Time { return paNow }
		return d
	}
	in := plan{Name: "Run", Stops: stopsOf("2026-11-14", 0, 1, 0)}
	in.Stops[0].DepartTime = "08:00"
	saved, err := store.CreatePlan(in)
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprint(saved.ID)
	c, rec := planCtx(http.MethodGet, "/api/plans/"+id+"/assessment", "", id, "")
	if err := planAssessmentHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("assessment: %v %d %s", err, rec.Code, rec.Body.String())
	}
	var out planAssessment
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Legs) != 2 || out.Legs[0].Verdict != "go" {
		t.Fatalf("assessment = %s (%v)", rec.Body.String(), err)
	}
	for _, key := range []string{`"as_of"`, `"forecast_as_of"`, `"problem"`, `"warnings_error"`, `"depart_suggested"`, `"current_kn"`, `"current_note"`, `"limiting"`, `"note_kind"`} {
		if !strings.Contains(rec.Body.String(), key) {
			t.Errorf("the page binds %s; it is missing from %s", key, rec.Body.String())
		}
	}

	c, rec = planCtx(http.MethodGet, "/api/plans/999/assessment", "", "999", "")
	if err := planAssessmentHandler(c); err != nil || rec.Code != http.StatusNotFound {
		t.Fatalf("missing plan: %v %d", err, rec.Code)
	}
}

// A second read of an unchanged plan inside ten minutes is answered from the
// memo, with no provider asked. A changed plan, changed limits or speed, an old
// entry and a plan write each ask again.
func TestPlanAssessmentHandler_MemoisesForTenMinutesAndFollowsTheContent(t *testing.T) {
	store := withTestDocumentStore(t)
	planAssessMemoClear()
	prevSeg, prevDeps := planRouteSegment, planAssessDepsFor
	t.Cleanup(func() { planRouteSegment, planAssessDepsFor = prevSeg, prevDeps; planAssessMemoClear() })
	planRouteSegment = func(_ context.Context, aLat, aLon, bLat, bLon float64) (waterRoute, error) {
		return routeOf(30, [2]float64{aLat, aLon}, [2]float64{bLat, bLon}), nil
	}
	f := &paFake{}
	clock := paNow
	planAssessDepsFor = func() planAssessDeps {
		d := paDeps(f)
		d.now = func() time.Time { return clock }
		return d
	}
	saved, err := store.CreatePlan(plan{Name: "Run", Stops: stopsOf("2026-11-14", 0, 1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprint(saved.ID)
	get := func() string {
		c, rec := planCtx(http.MethodGet, "/api/plans/"+id+"/assessment", "", id, "")
		if err := planAssessmentHandler(c); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("assessment: %v %d %s", err, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	first := get()
	asked := f.fetches
	if asked == 0 {
		t.Fatal("the first read must ask the forecast")
	}
	clock = clock.Add(9 * time.Minute)
	if second := get(); second != first || f.fetches != asked {
		t.Fatalf("an unchanged plan inside ten minutes is the memo's: fetches %d -> %d", asked, f.fetches)
	}

	// Another speed is another assessment.
	f.speed = 7
	get()
	if f.fetches == asked {
		t.Fatal("a changed speed must not be answered from the memo")
	}
	asked = f.fetches

	// Ten minutes on, the memo has expired.
	clock = clock.Add(11 * time.Minute)
	get()
	if f.fetches == asked {
		t.Fatal("an entry older than ten minutes must be asked again")
	}
	asked = f.fetches

	// A change to the plan is asked again.
	edited := saved
	edited.Stops[0].DepartTime = "09:00"
	if _, _, err := store.UpdatePlan(saved.ID, edited); err != nil {
		t.Fatal(err)
	}
	get()
	if f.fetches == asked {
		t.Fatal("a changed plan must not be answered from the memo")
	}
}

func TestPlansRoutes_AssessmentIsReadTier(t *testing.T) {
	for _, r := range plansRoutes() {
		if r.Path == "/api/plans/:id/assessment" {
			if r.Method != http.MethodGet || r.Tier != tierRead {
				t.Fatalf("route = %+v", r)
			}
			return
		}
	}
	t.Fatal("no /api/plans/:id/assessment route")
}
