package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The plan's suggested departure and current column read the same ranking and
// the same modelled current as Mate's plan_tidal_departure. The fixtures are
// that tool's own (assistant_tidal_departure_test.go).

func southboundQuery(hours float64) planDepartureQuery {
	return planDepartureQuery{
		Route: []assistantLatLon{{Lat: -27.0, Lon: 153.0}, {Lat: -27.3, Lon: 153.0}},
		Hours: hours,
	}
}

func TestPlanForecastMemo_ReadersMissingOnTheSamePointAtOnceShareOneRead(t *testing.T) {
	m := newPlanForecastMemo(planForecastMemoTTL)
	var calls atomic.Int32
	release := make(chan struct{})
	fc := forecastSource{Provider: "wx", Fetch: func(context.Context, float64, float64) (passageForecast, error) {
		calls.Add(1)
		<-release
		return passageForecast{FetchedAt: paNow}, nil
	}}
	wrapped := m.wrap(fc, "Australia/Brisbane", paNow)
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := wrapped.Fetch(context.Background(), -27.0, 153.0); err != nil {
				t.Error(err)
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("%d provider reads for 5 concurrent readers", got)
	}
}

func TestPlanForecastMemo_ExpiredPointsAreClearedOncePerTTLNotOnEveryMiss(t *testing.T) {
	m := newPlanForecastMemo(10 * time.Minute)
	fc := forecastSource{Provider: "wx", Fetch: func(context.Context, float64, float64) (passageForecast, error) {
		return passageForecast{}, nil
	}}
	read := func(at time.Time, lat float64) {
		if _, err := m.wrap(fc, "z", at).Fetch(context.Background(), lat, 153.0); err != nil {
			t.Fatal(err)
		}
	}
	t0 := paNow
	read(t0, -27.0)
	read(t0.Add(11*time.Minute), -28.0) // sweeps: the first point is gone
	if len(m.m) != 1 {
		t.Fatalf("%d points kept, want only the fresh one", len(m.m))
	}
	read(t0.Add(12*time.Minute), -29.0)
	read(t0.Add(23*time.Minute), -30.0) // a second sweep a TTL on
	if len(m.m) != 1 {
		t.Fatalf("%d points kept", len(m.m))
	}
}

func TestPlanForecastMemo_ReadsAPointOncePerTenMinutesAndNeverKeepsAFailure(t *testing.T) {
	m := newPlanForecastMemo(10 * time.Minute)
	calls, fail := 0, false
	fc := forecastSource{Provider: "wx", WaveProvider: "wv", Fetch: func(_ context.Context, lat, lon float64) (passageForecast, error) {
		calls++
		if fail {
			return passageForecast{}, fmt.Errorf("provider down")
		}
		return passageForecast{FetchedAt: paNow}, nil
	}}
	t0 := paNow
	wrapped := m.wrap(fc, "Australia/Brisbane", t0)
	if _, err := wrapped.Fetch(context.Background(), -27.001, 153.001); err != nil {
		t.Fatal(err)
	}
	// The same 0.01 degree point, the way the assessor keys it, is not read twice.
	if _, err := wrapped.Fetch(context.Background(), -27.004, 153.004); err != nil || calls != 1 {
		t.Fatalf("same point: calls = %d, err = %v", calls, err)
	}
	// Another assessment, nine minutes on, still reuses it; eleven minutes on, does not.
	if _, err := m.wrap(fc, "Australia/Brisbane", t0.Add(9*time.Minute)).Fetch(context.Background(), -27.001, 153.001); err != nil || calls != 1 {
		t.Fatalf("inside ten minutes: calls = %d", calls)
	}
	if _, err := m.wrap(fc, "Australia/Brisbane", t0.Add(11*time.Minute)).Fetch(context.Background(), -27.001, 153.001); err != nil || calls != 2 {
		t.Fatalf("after ten minutes: calls = %d", calls)
	}
	// Another zone is another read, and a failure is not kept.
	fail = true
	if _, err := m.wrap(fc, "Pacific/Auckland", t0).Fetch(context.Background(), -27.001, 153.001); err == nil {
		t.Fatal("the failure must come through")
	}
	fail = false
	if _, err := m.wrap(fc, "Pacific/Auckland", t0).Fetch(context.Background(), -27.001, 153.001); err != nil || calls != 4 {
		t.Fatalf("after a failure: calls = %d, err = %v", calls, err)
	}
}

func TestPlanDepartures_IsEveryDepartureFairestStreamFirstWithItsCurrent(t *testing.T) {
	// The current sets south from 06:00Z and north before it: a southbound run is
	// helped only after 06:00Z.
	p := steadyCurrent(0)
	p.at = func(lat, lon float64, ts time.Time) (float64, float64, bool) {
		if ts.Before(time.Date(2026, 9, 12, 6, 0, 0, 0, time.UTC)) {
			return 0.5, 0, true
		}
		return 0.5, 180, true
	}
	deps := tidalDepsWithProvider(p)
	q := southboundQuery(3)
	q.Earliest = time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	q.Latest = time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	got, err := deps.planDepartures(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if want := 12*4 + 1; len(got) != want {
		t.Fatalf("%d departures, want one every quarter hour of the window (%d)", len(got), want)
	}
	if got[0].Depart.Before(time.Date(2026, 9, 12, 5, 55, 0, 0, time.UTC)) {
		t.Fatalf("first %s: a southbound run is best once the current sets south", got[0].Depart)
	}
	for i, c := range got {
		if !c.Current.Have {
			t.Fatalf("departure %d has no current: %+v", i, c)
		}
		if i > 0 && c.Current.MeanKn > got[i-1].Current.MeanKn {
			t.Fatalf("departure %d (%.2f kn) is fairer than the one before it (%.2f kn)", i, c.Current.MeanKn, got[i-1].Current.MeanKn)
		}
	}
	if best := got[0].Current.MeanKn; math.Abs(best-0.5*1.943844) > 0.05 {
		t.Fatalf("the best departure's current is %.2f kn, want about %.2f", best, 0.5*1.943844)
	}
}

func TestPlanDepartures_ARankingThatCannotBeDoneIsItsError(t *testing.T) {
	// No current at any point and no flood direction: the ranking has nothing to
	// go by, and the plan says so in its own words. Nothing is made up.
	p := steadyCurrent(0)
	p.at = func(lat, lon float64, ts time.Time) (float64, float64, bool) { return 0, 0, false }
	deps := tidalDepsWithProvider(p)
	q := southboundQuery(3)
	q.Earliest = time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	q.Latest = time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	got, err := deps.planDepartures(context.Background(), q)
	if err == nil {
		t.Fatalf("ranked %d departures with nothing to rank by", len(got))
	}
	if strings.Contains(err.Error(), "flood_set") || !strings.Contains(err.Error(), "has no current for the run") {
		t.Fatalf("error = %q: it is read by the operator, not by Mate", err)
	}
}

func TestPlanDepartures_ErrorsCarryNoToolName(t *testing.T) {
	p := steadyCurrent(0)
	p.fail = context.DeadlineExceeded
	deps := tidalDepsWithProvider(p)
	q := southboundQuery(3)
	q.Earliest = time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	q.Latest = time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	_, err := deps.planDepartures(context.Background(), q)
	if err == nil || strings.Contains(err.Error(), "plan_tidal_departure") {
		t.Fatalf("error = %v", err)
	}
}

// ── the warnings read through the fetch loop's own check ────────────────

func TestPlanWarningsFrom_AStaleServedBundleIsAnErrorNotNoWarnings(t *testing.T) {
	now := time.Date(2026, 11, 14, 4, 30, 0, 0, time.UTC)
	stale := &stubForecastWarningsProvider{id: "bom", ttl: 600, bundle: forecastWarningsBundle{Cached: true, CachedAt: now.Add(-3 * time.Hour)}}
	_, err := planWarningsFrom(stale, "bom", now)(-27, 153)
	if err == nil || !strings.Contains(err.Error(), `provider "bom"`) || !strings.Contains(err.Error(), "older than its own") {
		t.Fatalf("err = %v: a provider serving a stale cache is failing, not quiet", err)
	}
	fresh := &stubForecastWarningsProvider{id: "bom", ttl: 600, bundle: forecastWarningsBundle{Cached: true, CachedAt: now.Add(-time.Minute), Region: "QLD"}}
	got, err := planWarningsFrom(fresh, "bom", now)(-27, 153)
	if err != nil || got.Region != "QLD" {
		t.Fatalf("got %+v, %v: a cache hit inside the TTL is fresh", got, err)
	}
	down := &stubForecastWarningsProvider{id: "bom", err: fmt.Errorf("connection refused")}
	if _, err := planWarningsFrom(down, "bom", now)(-27, 153); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("err = %v", err)
	}
}

func TestPlanCurrentAlong_HelpingIsPositiveAndHinderingNegative(t *testing.T) {
	const kn = 0.5 * 1.943844
	dep := time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC)

	south := tidalDepsWithProvider(steadyCurrent(180))
	got, err := south.planCurrentAlong(context.Background(), southboundQuery(3), dep)
	if err != nil || !got.Have || math.Abs(got.MeanKn-kn) > 0.05 {
		t.Fatalf("with the current: %+v %v, want about %.2f", got, err, kn)
	}

	north := tidalDepsWithProvider(steadyCurrent(0))
	got, err = north.planCurrentAlong(context.Background(), southboundQuery(3), dep)
	if err != nil || !got.Have || math.Abs(got.MeanKn+kn) > 0.05 {
		t.Fatalf("against the current: %+v %v, want about -%.2f", got, err, kn)
	}
}

func TestPlanCurrentAlong_NoModelCurrentIsNoReadingWithItsReason(t *testing.T) {
	p := steadyCurrent(0)
	p.at = func(lat, lon float64, ts time.Time) (float64, float64, bool) { return 0, 0, false }
	got, err := tidalDepsWithProvider(p).planCurrentAlong(context.Background(), southboundQuery(3), time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC))
	if err != nil || got.Have || got.Why == "" || got.MeanKn != 0 {
		t.Fatalf("got %+v %v: a leg the model has nothing for is not 0 kn", got, err)
	}
}

func TestPlanCurrentAlong_AFailedRequestIsAnError(t *testing.T) {
	p := steadyCurrent(0)
	p.fail = context.DeadlineExceeded
	if _, err := tidalDepsWithProvider(p).planCurrentAlong(context.Background(), southboundQuery(3), time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("a failed request is not a reading")
	}
}

// ── the ETA speed comes from the passages Helmcentral recorded ───────────

// recordPassage stores a passage that began daysAgo days before now and covered
// nm nautical miles in underwayHours hours under way.
func recordPassage(t *testing.T, store *passageStore, now time.Time, daysAgo int, nm, underwayHours float64, hidden bool) {
	t.Helper()
	start := now.AddDate(0, 0, -daysAgo)
	rec, err := store.InsertPassage(passageRecord{Start: start, End: start.Add(time.Duration(underwayHours * float64(time.Hour))), Title: "t", Hidden: hidden})
	if err != nil {
		t.Fatal(err)
	}
	figures := passageFigures{Stats: passageStats{DistanceNM: nm, UnderwaySeconds: underwayHours * 3600}}
	if err := store.storeFigures(rec, figures); err != nil {
		t.Fatal(err)
	}
}

func TestRecordedCruisingSpeed_IsTheMedianOfTheLastTwelveMonthsOfPassages(t *testing.T) {
	store := newTestPassageStore(t)
	now := time.Date(2026, 11, 14, 4, 30, 0, 0, time.UTC)
	recordPassage(t, store, now, 10, 30, 5, false) // 6 kn
	recordPassage(t, store, now, 40, 35, 5, false) // 7 kn
	recordPassage(t, store, now, 90, 40, 5, false) // 8 kn
	// Outside the window, or not a passage worth counting: none of these count.
	recordPassage(t, store, now, 400, 120, 5, false) // 24 kn, 13 months ago
	recordPassage(t, store, now, 20, 1, 0.2, false)  // a marina move
	recordPassage(t, store, now, 25, 100, 5, true)   // hidden by the operator
	got, err := recordedCruisingSpeed(context.Background(), store, now)
	if err != nil || got != 7 {
		t.Fatalf("speed = %v, %v; want 7 kn, the median of 6, 7 and 8", got, err)
	}
}

func TestRecordedCruisingSpeed_NoPassagesIsAnErrorNotADefault(t *testing.T) {
	store := newTestPassageStore(t)
	now := time.Date(2026, 11, 14, 4, 30, 0, 0, time.UTC)
	recordPassage(t, store, now, 400, 30, 5, false)
	recordPassage(t, store, now, 5, 0.5, 0.1, false)
	got, err := recordedCruisingSpeed(context.Background(), store, now)
	if err == nil || !strings.Contains(err.Error(), "no recorded passages yet") {
		t.Fatalf("speed = %v, err = %v; want the no-recorded-passages error", got, err)
	}
}

func TestRecordedCruisingSpeed_NoPassageStoreIsAnError(t *testing.T) {
	if _, err := recordedCruisingSpeed(context.Background(), nil, time.Now()); err == nil {
		t.Fatal("no store, no speed")
	}
}

// Wall time of one assessment of a four-leg plan when every provider call takes
// 200 ms (forecast per point, ranking per leg, warnings per place), cold and
// then with the point forecasts kept. Skipped under -short: it sleeps.
func TestPlanAssessmentLatency_ColdAndWarm(t *testing.T) {
	if testing.Short() {
		t.Skip("sleeps; run without -short to measure")
	}
	const delay = 200 * time.Millisecond
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0, 1, 0)
	f := &paFake{
		fc: func() forecastSource {
			base := pwUniform(10, 190, 12, nil, 0, paAt(14, 0, 0), paAt(24, 23, 0))
			inner := base.Fetch
			base.Fetch = func(ctx context.Context, lat, lon float64) (passageForecast, error) {
				time.Sleep(delay)
				return inner(ctx, lat, lon)
			}
			return base
		}(),
		departures: func(q planDepartureQuery) ([]tidalDeparture, error) {
			time.Sleep(delay)
			return []tidalDeparture{{Depart: q.Earliest.Add(time.Hour), Current: planCurrentReading{Have: true, MeanKn: 0.4}}}, nil
		},
		warnings: func(lat, lon float64) (forecastWarningsBundle, error) {
			time.Sleep(delay)
			return forecastWarningsBundle{}, nil
		},
	}
	d := paDeps(f)
	points := newPlanForecastMemo(planForecastMemoTTL)
	inner := d.sources
	d.sources = func(zone func() (string, error), now time.Time) (passageSources, error) {
		src, err := inner(zone, now)
		if err != nil {
			return src, err
		}
		src.fc = points.wrap(src.fc, "Australia/Brisbane", now)
		return src, nil
	}
	for _, run := range []string{"cold", "warm"} {
		start := time.Now()
		out := assessPlan(context.Background(), p, legs, d, planAssessOptions{})
		t.Logf("%s: %s for %d legs (%d forecast reads so far)", run, time.Since(start).Round(10*time.Millisecond), len(out.Legs), f.fetches)
	}
}

// A request that is over stops the walk through the passages: no speed, and the
// error is the context's.
func TestRecordedCruisingSpeed_ACancelledContextStopsTheLoop(t *testing.T) {
	store := newTestPassageStore(t)
	now := time.Date(2026, 11, 14, 4, 30, 0, 0, time.UTC)
	recordPassage(t, store, now, 10, 30, 5, false)
	recordPassage(t, store, now, 40, 35, 5, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := recordedCruisingSpeed(ctx, store, now)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("speed = %v, err = %v; want the context's error", got, err)
	}
}
