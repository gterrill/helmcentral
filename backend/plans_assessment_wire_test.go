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

func TestPlanForecastMemo_ACancelledLeaderDoesNotFailTheReadersSharingItsRead(t *testing.T) {
	m := newPlanForecastMemo(planForecastMemoTTL)
	var calls atomic.Int32
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	fc := forecastSource{Provider: "wx", Fetch: func(ctx context.Context, _, _ float64) (passageForecast, error) {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-ctx.Done():
			return passageForecast{}, ctx.Err()
		case <-release:
			return passageForecast{FetchedAt: paNow}, nil
		}
	}}
	wrapped := m.wrap(fc, "Australia/Brisbane", paNow)
	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderErr := make(chan error, 1)
	go func() {
		_, err := wrapped.Fetch(leaderCtx, -27.0, 153.0)
		leaderErr <- err
	}()
	<-started
	followerGot := make(chan error, 1)
	go func() {
		_, err := wrapped.Fetch(context.Background(), -27.0, 153.0)
		followerGot <- err
	}()
	time.Sleep(50 * time.Millisecond) // the follower is waiting on the leader's read
	cancelLeader()
	// The leader's own request is over and says so, without waiting for the read.
	select {
	case err := <-leaderErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("leader err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the cancelled leader kept waiting")
	}
	close(release)
	select {
	case err := <-followerGot:
		if err != nil {
			t.Fatalf("the follower failed with the leader's cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the follower never got the forecast")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("%d provider reads, want 1 shared", got)
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

// A southbound leg's current sets south from 06:00Z and north before it: the run is
// helped only after 06:00Z. The departures come out fairest first, each with the one
// figure of its own run, and each is run on a timeline the current has moved.
func TestOrderDepartures_IsEveryDepartureFairestStreamFirstWithItsCurrent(t *testing.T) {
	prov := steadyCurrent(0)
	prov.at = func(lat, lon float64, ts time.Time) (float64, float64, bool) {
		if ts.Before(time.Date(2026, 9, 12, 6, 0, 0, 0, time.UTC)) {
			return 0.5, 0, true
		}
		return 0.5, 180, true
	}
	hours := 3.0
	p, err := (&legPaceModel{CruiseKn: 10, Slow: map[paceCell]float64{}}).profile(lpLeg(-27.0, -27.0-lpDeg(10*hours)))
	if err != nil {
		t.Fatal(err)
	}
	field, err := tidalDepsWithProvider(prov).readLegCurrents(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	var deps []time.Time
	for d := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC); !d.After(time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)); d = d.Add(planDepartureStep) {
		deps = append(deps, d)
	}
	got, err := orderDepartures(runsOf(p, field, deps...), field)
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
		// The current is in the speed: a run it helps arrives before the still-water
		// time, one it hinders after it.
		still := c.Depart.Add(hoursDuration(hours))
		switch {
		case c.Current.MeanKn > 0.3 && !c.run.tl.arrive().Before(still):
			t.Fatalf("departure %s helped by %.2f kn arrives %s, not before %s", c.Depart, c.Current.MeanKn, c.run.tl.arrive(), still)
		case c.Current.MeanKn < -0.3 && !c.run.tl.arrive().After(still):
			t.Fatalf("departure %s hindered by %.2f kn arrives %s, not after %s", c.Depart, c.Current.MeanKn, c.run.tl.arrive(), still)
		}
	}
	if best := got[0].Current.MeanKn; math.Abs(best-0.5*1.943844) > 0.05 {
		t.Fatalf("the best departure's current is %.2f kn, want about %.2f", best, 0.5*1.943844)
	}
}

// A departure the model covers less than half of is left out when another is
// covered better, and with none well covered the best of them is ranked.
func TestOrderDepartures_ADepartureUnderHalfCoveredYieldsToABetterCoveredOne(t *testing.T) {
	p, err := (&legPaceModel{CruiseKn: 10, Slow: map[paceCell]float64{}}).profile(lpLeg(-27.0, -27.0-lpDeg(10)))
	if err != nil {
		t.Fatal(err)
	}
	// The first of three departures has a current for a fifth of the run and the
	// strongest of them; the second for all of it.
	// The current is in the speed, so the departures are told apart by the hour their
	// steps are entered in: the early run is over within the hour, the later one is at
	// sea in the next.
	early, later := lpNow, lpNow.Add(time.Hour)
	field := crField{along: func(k int, t time.Time) (float64, bool) {
		switch {
		case t.Before(later):
			return 3, k < len(p.steps)/5
		case t.Before(later.Add(time.Hour)):
			return 1, true
		}
		return 0, false
	}}
	got, err := orderDepartures(runsOf(p, field, early, later), field)
	if err != nil || len(got) != 1 || !got[0].Depart.Equal(later) {
		t.Fatalf("got %+v, %v: the one with the better cover alone is ranked", got, err)
	}
	got, err = orderDepartures(runsOf(p, field, early), field)
	if err != nil || len(got) != 1 || !got[0].Depart.Equal(early) {
		t.Fatalf("got %+v, %v: with none well covered the best of them is ranked", got, err)
	}
}

func TestAssessPlan_ASuggestedDepartureArrivesWhenThePaceSaysNotAtTheMinute(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	legs[0].DistanceNm = 12.3
	cur := steadyCurrent(180)
	cur.from = paAt(13, 0, 0)
	ranking := tidalDepsWithProvider(cur)
	ranking.now = func() time.Time { return paNow }
	f := &paFake{speed: 5.7}
	d := paDeps(f)
	d.legCurrents = ranking.readLegCurrents
	l := assessPlan(context.Background(), p, legs, d, planAssessOptions{}).Legs[0]
	if !l.DepartSuggested {
		t.Fatalf("leg = %+v", l)
	}
	hours := 12.3 / 5.7
	want := paParse(t, l.Depart).Add(hoursDuration(hours)).Truncate(time.Second)
	if got := paParse(t, l.Arrive); !got.Equal(want) {
		t.Fatalf("arrive %s, want %s (the pace's, not the minute-rounded 2h09m)", got, want)
	}
	if l.Hours == nil || math.Abs(*l.Hours-hours) > 1e-6 {
		t.Fatalf("hours = %v, want %v", l.Hours, hours)
	}
}

// southbound is a leg of nm nautical miles south from -27, on lon 149.
func southbound(t *testing.T, nm float64) *legPaceProfile {
	t.Helper()
	p, err := (&legPaceModel{CruiseKn: 10, Slow: map[paceCell]float64{}}).profile(lpLeg(-27.0, -27.0-lpDeg(nm)))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOrderDepartures_NoCurrentAtAnyPointIsTheErrorInTheOperatorsWords(t *testing.T) {
	// No current at any point: the ranking has nothing to go by, and the plan says
	// so in its own words. Nothing is made up.
	prov := steadyCurrent(0)
	prov.at = func(lat, lon float64, ts time.Time) (float64, float64, bool) { return 0, 0, false }
	p := southbound(t, 30)
	field, err := tidalDepsWithProvider(prov).readLegCurrents(context.Background(), p)
	if err != nil || field.none() == "" {
		t.Fatalf("field none = %q, err %v: the reason the model has nothing", field.none(), err)
	}
	got, err := orderDepartures(runsOf(p, field, time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)), field)
	if err == nil {
		t.Fatalf("ranked %d departures with nothing to rank by", len(got))
	}
	if strings.Contains(err.Error(), "flood_set") || !strings.Contains(err.Error(), "has no current for the run") {
		t.Fatalf("error = %q: it is read by the operator, not by Mate", err)
	}
}

func TestReadLegCurrents_AFailedRequestIsAnErrorWithNoToolName(t *testing.T) {
	prov := steadyCurrent(0)
	prov.fail = context.DeadlineExceeded
	_, err := tidalDepsWithProvider(prov).readLegCurrents(context.Background(), southbound(t, 30))
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

func TestRunReading_HelpingIsPositiveAndHinderingNegative(t *testing.T) {
	const kn = 0.5 * 1.943844
	dep := time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC)
	p := southbound(t, 30)

	for _, tc := range []struct {
		name string
		set  float64
		want float64
	}{{"with the current", 180, kn}, {"against the current", 0, -kn}} {
		field, err := tidalDepsWithProvider(steadyCurrent(tc.set)).readLegCurrents(context.Background(), p)
		if err != nil {
			t.Fatal(err)
		}
		r, err := p.run(dep, field)
		if err != nil {
			t.Fatal(err)
		}
		got := runReading(r, field)
		if !got.Have || math.Abs(got.MeanKn-tc.want) > 0.05 || r.modelledNm < 29.9 || r.unmodelledNm != 0 {
			t.Fatalf("%s: %+v, want about %.2f over the whole leg (%+v)", tc.name, got, tc.want, r)
		}
	}
}

func TestRunReading_NoModelCurrentIsNoReadingWithItsReason(t *testing.T) {
	prov := steadyCurrent(0)
	prov.at = func(lat, lon float64, ts time.Time) (float64, float64, bool) { return 0, 0, false }
	p := southbound(t, 30)
	field, err := tidalDepsWithProvider(prov).readLegCurrents(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	r, err := p.run(time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC), field)
	if got := runReading(r, field); err != nil || got.Have || got.Why == "" || got.MeanKn != 0 {
		t.Fatalf("got %+v %v: a leg the model has nothing for is not 0 kn", got, err)
	}
}

// A departure whose hours the series does not reach has a reason of its own, and is
// not read as slack.
func TestRunReading_AnHourTheSeriesDoesNotReachIsSaidSo(t *testing.T) {
	p := southbound(t, 30)
	field, err := tidalDepsWithProvider(steadyCurrent(180)).readLegCurrents(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	// The stub's series ends five days after 11 September.
	r, err := p.run(time.Date(2026, 9, 30, 2, 0, 0, 0, time.UTC), field)
	got := runReading(r, field)
	if err != nil || got.Have || got.Why != noCurrentAtTheHours {
		t.Fatalf("got %+v %v", got, err)
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
		departures: func() ([]tidalDeparture, error) {
			time.Sleep(delay)
			return []tidalDeparture{paDeparture(6, 15, 0.4)}, nil
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
