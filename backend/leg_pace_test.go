package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ── two made-up boats, so no constant of either can leak into the code ───────

type lpBoat struct {
	name     string
	cruiseKn float64
	cruiseRP float64
	slowKn   float64 // at the berth
	slowRP   float64
	berthMin float64 // minutes at the berth, short enough to stay in its cell
}

var lpBoats = []lpBoat{
	{name: "displacement", cruiseKn: 8, cruiseRP: 2000, slowKn: 1.5, slowRP: 800, berthMin: 6},
	{name: "planing", cruiseKn: 20, cruiseRP: 3400, slowKn: 3, slowRP: 1100, berthMin: 4},
}

// lpBerthLat is inside the 0.005 degree cell [-20.005, -20.0); the berth stretch
// of every boat ends before it leaves it.
const lpBerthLat = -20.0045

var lpNow = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

// lpStretch is a run along the meridian: dir +1 is north, -1 south. rpm 0 is no
// reading at all, which is what a boat under sail or an import has.
type lpStretch struct {
	min, kn, rpm, dir float64
}

func lpCruise(b lpBoat, hours float64) lpStretch {
	return lpStretch{min: hours * 60, kn: b.cruiseKn, rpm: b.cruiseRP, dir: 1}
}

func lpBerth(b lpBoat) lpStretch {
	return lpStretch{min: b.berthMin, kn: b.slowKn, rpm: b.slowRP, dir: 1}
}

// lpPoints lays the stretches out as track points every 30 s from lpBerthLat.
func lpPoints(start time.Time, seg int, stretches []lpStretch) []recordedTrackPoint {
	lat := lpBerthLat
	at := start
	pts := []recordedTrackPoint{{At: at, Segment: seg, Lat: lat, Lon: 149.0, SOGKts: pfloat(0)}}
	const step = 30 * time.Second
	for _, s := range stretches {
		for el := time.Duration(0); el < time.Duration(s.min*float64(time.Minute)); el += step {
			at = at.Add(step)
			lat += s.dir * s.kn * step.Hours() / 60
			p := recordedTrackPoint{At: at, Segment: seg, Lat: lat, Lon: 149.0, SOGKts: pfloat(s.kn)}
			if s.rpm > 0 {
				p.RPMMean = pfloat(s.rpm)
			}
			pts = append(pts, p)
		}
	}
	return pts
}

// lpAddPassage stores a passage that began daysAgo days before lpNow.
func lpAddPassage(t *testing.T, store *passageStore, daysAgo int, stretches ...lpStretch) passageRecord {
	t.Helper()
	start := lpNow.AddDate(0, 0, -daysAgo)
	pts := lpPoints(start, daysAgo, stretches)
	rec, err := store.InsertPassage(passageRecord{Start: start, End: pts[len(pts)-1].At, Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pts {
		if err := store.AppendTrackPoint(p); err != nil {
			t.Fatal(err)
		}
	}
	return rec
}

// lpFleet stores n passages of a boat, each from the berth and then cruising.
func lpFleet(t *testing.T, store *passageStore, b lpBoat, n int, cruiseHours float64) []passageRecord {
	t.Helper()
	var out []passageRecord
	for i := range n {
		out = append(out, lpAddPassage(t, store, 10+30*i, lpBerth(b), lpCruise(b, cruiseHours)))
	}
	return out
}

func eachBoat(t *testing.T, f func(t *testing.T, b lpBoat)) {
	for _, b := range lpBoats {
		t.Run(b.name, func(t *testing.T) { f(t, b) })
	}
}

// lpIv is one interval, laid in the cell at lat/lon.
func lpIv(passage int, sec, kn, rpm, lat float64) paceInterval {
	return paceInterval{passage: passage, sec: sec, kn: kn, rpm: rpm, lat: lat, lon: 149.0}
}

// lpCruiseIvs is n passages of an hour each at the boat's cruise, each in a cell of
// its own away from the berth.
func lpCruiseIvs(b lpBoat, n int) []paceInterval {
	var ivs []paceInterval
	for p := range n {
		for k := range 4 {
			ivs = append(ivs, lpIv(p, 900, b.cruiseKn, b.cruiseRP, -19.0-0.1*float64(p)-0.01*float64(k)))
		}
	}
	return ivs
}

// lpPaceTrack is the leg pace's part of full track points, as the narrow read
// gives it.
func lpPaceTrack(pts []recordedTrackPoint) []paceTrackPoint {
	out := make([]paceTrackPoint, len(pts))
	for i, p := range pts {
		out[i] = paceTrackPoint{At: p.At, Segment: p.Segment, Source: p.Source, Lat: p.Lat, Lon: p.Lon}
		if p.RPMMean != nil && *p.RPMMean > 0 {
			out[i].RPM = *p.RPMMean
		}
	}
	return out
}

// The narrow read gives the same intervals as the full one, over a store that has
// everything the walk judges a point on: a gap, a new segment, a jump, a stop,
// sailing, a zero rpm, and an import with no rpm at all and long gaps.
func TestLegPace_TheNarrowReadGivesTheSameIntervalsAsTheFullOne(t *testing.T) {
	store := newTestPassageStore(t)
	b := lpBoats[0]
	lpAddPassage(t, store, 10, lpBerth(b), lpCruise(b, 1), lpStretch{min: 30, kn: b.cruiseKn, dir: 1}) // motoring then sailing
	lpAddPassage(t, store, 20, lpStretch{min: 30, kn: b.cruiseKn, rpm: b.cruiseRP, dir: -1}, lpStretch{min: 10, kn: 0.3, rpm: 700, dir: 1})
	// A hole in the record, a new segment and a jump inside one passage.
	start := lpNow.AddDate(0, 0, -30)
	pts := lpPoints(start, 1, []lpStretch{lpCruise(b, 0.5)})
	last := pts[len(pts)-1]
	late := last.At.Add(20 * time.Minute)
	pts = append(pts,
		recordedTrackPoint{At: late, Segment: 1, Lat: last.Lat + 0.2, Lon: 149.0, trackContext: trackContext{RPMMean: pfloat(b.cruiseRP)}},
		recordedTrackPoint{At: late.Add(30 * time.Second), Segment: 1, Lat: last.Lat + 2, Lon: 149.0, trackContext: trackContext{RPMMean: pfloat(b.cruiseRP)}},
		recordedTrackPoint{At: late.Add(time.Minute), Segment: 2, Lat: last.Lat + 2.01, Lon: 149.0, trackContext: trackContext{RPMMean: pfloat(0)}},
		recordedTrackPoint{At: late.Add(90 * time.Second), Segment: 2, Lat: last.Lat + 2.02, Lon: 149.0, trackContext: trackContext{RPMMean: pfloat(b.cruiseRP)}},
	)
	for _, p := range pts {
		if err := store.AppendTrackPoint(p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.InsertPassage(passageRecord{Start: start, End: pts[len(pts)-1].At, Title: "t"}); err != nil {
		t.Fatal(err)
	}
	// An import: points hours apart still make a leg, and it has no rpm.
	istart := lpNow.AddDate(0, 0, -40)
	for k := range 5 {
		p := recordedTrackPoint{At: istart.Add(time.Duration(k) * 2 * time.Hour), Segment: 7, Lat: -20 + 0.3*float64(k), Lon: 149.0, Source: trackSourceImported}
		if err := store.AppendTrackPoint(p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.InsertPassage(passageRecord{Start: istart, End: istart.Add(8 * time.Hour), Title: "i", Source: trackSourceImported}); err != nil {
		t.Fatal(err)
	}

	recs, err := store.ListPassages()
	if err != nil || len(recs) != 4 {
		t.Fatalf("%d passages, %v", len(recs), err)
	}
	for i, r := range recs {
		full, err := store.TrackPointsBetween(r.Start, r.End)
		if err != nil {
			t.Fatal(err)
		}
		narrow, err := store.PaceTrackPointsBetween(context.Background(), r.Start, r.End)
		if err != nil {
			t.Fatal(err)
		}
		want, got := paceIntervalsOf(i, lpPaceTrack(full)), paceIntervalsOf(i, narrow)
		if len(want) == 0 {
			t.Fatalf("passage %d has no intervals to compare", i)
		}
		if !reflect.DeepEqual(want, got) {
			t.Errorf("passage %d: narrow read gave %d intervals unlike the full read's %d\nfull   %+v\nnarrow %+v", i, len(got), len(want), want, got)
		}
	}
}

// ── 1 and 2: the two ways there is nothing to learn from ─────────────────────

func TestLegPace_NoVisiblePassageInTwelveMonthsIsNoPassagesInTheLastTwelveMonths(t *testing.T) {
	eachBoat(t, func(t *testing.T, b lpBoat) {
		store := newTestPassageStore(t)
		lpAddPassage(t, store, 400, lpBerth(b), lpCruise(b, 1)) // 13 months ago
		_, err := learnLegPace(context.Background(), store, lpNow)
		if err == nil || !strings.Contains(err.Error(), "no passages recorded in the last twelve months") {
			t.Fatalf("err = %v, want no passages recorded in the last twelve months", err)
		}
		if !isLegPaceLearnedNot(err) {
			t.Fatalf("%v is an answer about the record, so it is kept", err)
		}
	})
}

func TestLegPace_AnEmptyRecordIsNoRecordedPassagesYet(t *testing.T) {
	store := newTestPassageStore(t)
	_, err := learnLegPace(context.Background(), store, lpNow)
	if err == nil || !strings.Contains(err.Error(), "no recorded passages yet") {
		t.Fatalf("err = %v, want no recorded passages yet", err)
	}
}

func TestLegPace_PassagesWithNoRPMUnderWayIsNoEngineRPM(t *testing.T) {
	eachBoat(t, func(t *testing.T, b lpBoat) {
		store := newTestPassageStore(t)
		for i := range 4 {
			lpAddPassage(t, store, 10+i*20, lpStretch{min: 120, kn: b.cruiseKn, dir: 1}) // sailing: rpm nil
		}
		_, err := learnLegPace(context.Background(), store, lpNow)
		if err == nil || !strings.Contains(err.Error(), "no engine rpm recorded under way") {
			t.Fatalf("err = %v, want no engine rpm recorded under way", err)
		}
	})
}

// ── 3: sailing is never low revs and never in the band ──────────────────────

func TestLegPace_SailingIsNeverLowRevsAndNeverInTheBand(t *testing.T) {
	eachBoat(t, func(t *testing.T, b lpBoat) {
		store := newTestPassageStore(t)
		for i := range 4 {
			// Sailing out of the berth cell at a crawl, then motoring.
			lpAddPassage(t, store, 10+i*20,
				lpStretch{min: b.berthMin, kn: b.slowKn, rpm: 0, dir: 1}, lpCruise(b, 1))
		}
		m, err := learnLegPace(context.Background(), store, lpNow)
		if err != nil {
			t.Fatal(err)
		}
		if len(m.Slow) != 0 {
			t.Errorf("a cell crawled through under sail is slow water: %v", m.Slow)
		}
		if math.Abs(m.BandHours-4) > 0.1 {
			t.Errorf("band hours = %.2f, want the 4 motoring hours only", m.BandHours)
		}
		if !approx(m.CruiseKn, b.cruiseKn, 0.05) {
			t.Errorf("cruise = %.2f, want %.1f", m.CruiseKn, b.cruiseKn)
		}
	})
}

// ── 4: what is left out of the intervals ─────────────────────────────────────

func TestLegPace_IdlingGapsAndJumpsAreLeftOut(t *testing.T) {
	at := lpNow
	// north is how far along the meridian the point is, in nautical miles.
	mk := func(dt time.Duration, seg int, north float64) recordedTrackPoint {
		at = at.Add(dt)
		return recordedTrackPoint{At: at, Segment: seg, Lat: -20 + north/60, Lon: 149, SOGKts: pfloat(5), trackContext: trackContext{RPMMean: pfloat(1800)}}
	}
	const half = 30 * time.Second
	pts := []recordedTrackPoint{
		mk(0, 0, 0),
		mk(half, 0, 0.0667),           // 8 kn: the one that counts
		mk(half, 0, 0.0707),           // 0.5 kn: drifting
		mk(11*time.Minute, 0, 0.5707), // a hole in the record
		mk(half, 0, 1.5707),           // 120 kn in 30 s: a jump
		mk(half, 1, 2.0707),           // another segment
	}
	got := paceIntervalsOf(0, lpPaceTrack(pts))
	if len(got) != 1 {
		t.Fatalf("%d intervals, want just the first: %+v", len(got), got)
	}
	if !approx(got[0].kn, 8, 0.05) || got[0].rpm != 1800 || got[0].sec != 30 {
		t.Errorf("interval = %+v", got[0])
	}
}

// ── 5: a bimodal record has one deterministic band ───────────────────────────

func TestLegPace_BimodalRPMPicksADeterministicWindowAndTiesGoHigh(t *testing.T) {
	eachBoat(t, func(t *testing.T, b lpBoat) {
		low, high := b.cruiseRP*0.6, b.cruiseRP
		mk := func(lowSec, highSec float64) []paceInterval {
			var ivs []paceInterval
			for p := range 4 {
				ivs = append(ivs,
					lpIv(p, lowSec, b.cruiseKn*0.6, low, -19-0.1*float64(p)),
					lpIv(p, highSec, b.cruiseKn, high, -18-0.1*float64(p)))
			}
			return ivs
		}
		tie, err := fitLegPace(mk(3600, 3600))
		if err != nil {
			t.Fatal(err)
		}
		if !approx(tie.CruiseKn, b.cruiseKn, 1e-6) || tie.BandLowRPM < low*1.2 {
			t.Errorf("a tie went to cruise %.2f kn, band from %.0f; want the higher revs", tie.CruiseKn, tie.BandLowRPM)
		}
		more, err := fitLegPace(mk(4000, 3600))
		if err != nil {
			t.Fatal(err)
		}
		if !approx(more.CruiseKn, b.cruiseKn*0.6, 1e-6) {
			t.Errorf("the denser window is the low one: cruise %.2f kn", more.CruiseKn)
		}
		// The order the intervals arrive in changes nothing.
		ivs := mk(3600, 3600)
		for i, j := 0, len(ivs)-1; i < j; i, j = i+1, j-1 {
			ivs[i], ivs[j] = ivs[j], ivs[i]
		}
		again, err := fitLegPace(ivs)
		if err != nil || again.CruiseKn != tie.CruiseKn || again.BandLowRPM != tie.BandLowRPM {
			t.Errorf("reversed order gave %+v, %v; first gave %+v", again, err, tie)
		}
	})
}

// ── 6: minimum data ──────────────────────────────────────────────────────────

func TestLegPace_BelowTheMinimumSaysWhatItHas(t *testing.T) {
	cases := []struct {
		name     string
		passages int
		hours    float64
		want     string
	}{
		{"two passages", 2, 3, "6.0 h at cruising revs over 2 passages"},
		{"1.5 hours", 3, 0.5, "1.5 h at cruising revs over 3 passages"},
		{"enough", 3, 0.7, ""},
	}
	for _, c := range cases {
		for _, b := range lpBoats {
			t.Run(c.name+"/"+b.name, func(t *testing.T) {
				ivs := lpCruiseIvs(b, 0)
				for p := range c.passages {
					ivs = append(ivs, lpIv(p, c.hours*3600, b.cruiseKn, b.cruiseRP, -19-0.1*float64(p)))
				}
				m, err := fitLegPace(ivs)
				if c.want == "" {
					if err != nil || m == nil {
						t.Fatalf("enough data, got %v", err)
					}
					return
				}
				if err == nil || !strings.Contains(err.Error(), "cruise speed not learned yet: "+c.want) || !strings.Contains(err.Error(), "needs 2 h over 3") {
					t.Fatalf("err = %v, want not learned yet: %s, needs 2 h over 3", err, c.want)
				}
			})
		}
	}
}

// ── 7, 8, 9, 10: when it is fitted again, through the real store ─────────────

// lpCounted is a cache whose fit is fit, counting how often it ran.
func lpCounted(fit func(ctx context.Context, now time.Time) (*legPaceModel, error)) (*legPaceCache, *atomic.Int32) {
	var fits atomic.Int32
	return newLegPaceCache(func(ctx context.Context, now time.Time) (*legPaceModel, error) {
		fits.Add(1)
		return fit(ctx, now)
	}), &fits
}

func lpSeven(context.Context, time.Time) (*legPaceModel, error) {
	return &legPaceModel{CruiseKn: 7}, nil
}

func TestLegPace_TheFirstRequestFitsAndTheNextReusesIt(t *testing.T) {
	c, fits := lpCounted(lpSeven)
	for range 3 {
		if m, err := c.model(context.Background(), lpNow); err != nil || m.CruiseKn != 7 {
			t.Fatalf("got %+v, %v", m, err)
		}
	}
	if n := fits.Load(); n != 1 {
		t.Fatalf("three requests with no change fitted %d times, want 1", n)
	}
}

func TestLegPace_AChangeMakesTheNextRequestFitAgain(t *testing.T) {
	c, fits := lpCounted(lpSeven)
	c.model(context.Background(), lpNow)
	c.bump()
	c.model(context.Background(), lpNow)
	c.model(context.Background(), lpNow)
	if n := fits.Load(); n != 2 {
		t.Fatalf("a change then two requests fitted %d times, want 2", n)
	}
}

func TestLegPace_AChangeDuringAFitMakesTheFollowingRequestFitAgain(t *testing.T) {
	var c *legPaceCache
	var fits atomic.Int32
	c = newLegPaceCache(func(ctx context.Context, now time.Time) (*legPaceModel, error) {
		if fits.Add(1) == 1 {
			c.bump() // a passage is recorded while the first fit reads
		}
		return &legPaceModel{CruiseKn: 7}, nil
	})
	c.model(context.Background(), lpNow)
	c.model(context.Background(), lpNow)
	if n := fits.Load(); n != 2 {
		t.Fatalf("a change during the fit: %d fits after two requests, want 2", n)
	}
	c.model(context.Background(), lpNow)
	if n := fits.Load(); n != 2 {
		t.Fatalf("the refit saw the change, so the third request reuses it: %d fits", n)
	}
}

func TestLegPace_ANewDayMakesTheNextRequestFitAgain(t *testing.T) {
	c, fits := lpCounted(lpSeven)
	c.model(context.Background(), lpNow)
	c.model(context.Background(), lpNow.Add(2*time.Hour)) // the same UTC day
	if n := fits.Load(); n != 1 {
		t.Fatalf("the same day fitted %d times, want 1", n)
	}
	c.model(context.Background(), lpNow.AddDate(0, 0, 1))
	if n := fits.Load(); n != 2 {
		t.Fatalf("the next day fitted %d times, want 2", n)
	}
}

func TestLegPace_AFailedFitIsReturnedAndNotKept(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	c, fits := lpCounted(func(ctx context.Context, now time.Time) (*legPaceModel, error) {
		if fail.Load() {
			return nil, errors.New("the passage record could not be read: disk gone")
		}
		return &legPaceModel{CruiseKn: 7}, nil
	})
	if m, err := c.model(context.Background(), lpNow); m != nil || err == nil || !strings.Contains(err.Error(), "disk gone") {
		t.Fatalf("a failed fit: %+v, %v", m, err)
	}
	fail.Store(false)
	if m, err := c.model(context.Background(), lpNow); err != nil || m.CruiseKn != 7 {
		t.Fatalf("the next request retries and succeeds: %+v, %v", m, err)
	}
	if n := fits.Load(); n != 2 {
		t.Fatalf("%d fits, want 2: the failure was not kept", n)
	}
	// A good fit is not replaced by a later failure while nothing changed: it is
	// reused and the failing fit never runs.
	fail.Store(true)
	if m, err := c.model(context.Background(), lpNow); err != nil || m.CruiseKn != 7 {
		t.Fatalf("unchanged, so reused: %+v, %v", m, err)
	}
	// After a change the failure is returned, not the older fit.
	c.bump()
	if m, err := c.model(context.Background(), lpNow); m != nil || err == nil {
		t.Fatalf("after a change a failed fit must not fall back to the old one: %+v, %v", m, err)
	}
}

// A record with too little in it is an answer, and is kept until something changes;
// only a fit that could not read is retried at once.
func TestLegPace_TooLittleRecordedIsKeptUntilAChange(t *testing.T) {
	c, fits := lpCounted(func(ctx context.Context, now time.Time) (*legPaceModel, error) {
		return nil, errLegPaceNoRPM
	})
	for range 3 {
		if _, err := c.model(context.Background(), lpNow); !errors.Is(err, errLegPaceNoRPM) {
			t.Fatalf("err = %v", err)
		}
	}
	if n := fits.Load(); n != 1 {
		t.Fatalf("%d fits for an unchanged record with no rpm, want 1", n)
	}
	c.bump()
	c.model(context.Background(), lpNow)
	if n := fits.Load(); n != 2 {
		t.Fatalf("%d fits after a change, want 2", n)
	}
}

func TestLegPace_ACancelledRequestReturnsTheContextsErrorAndKeepsNothing(t *testing.T) {
	b := lpBoats[0]
	store := newTestPassageStore(t)
	lpFleet(t, store, b, 3, 1)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if m, err := store.pace.model(cancelled, lpNow); m != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled request: %+v, %v, want the context's error", m, err)
	}
	if m, err := store.pace.model(context.Background(), lpNow); err != nil || m.Passages != 3 {
		t.Fatalf("the next request fits afresh: %+v, %v", m, err)
	}
}

// The fit belongs to every request waiting on it: a request that gives up leaves
// at once with its own error, and the fit carries on and is kept for the next.
func TestLegPace_ACallerGivingUpLeavesTheFitRunningAndKept(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	var fitCtxErr atomic.Value
	c, fits := lpCounted(func(ctx context.Context, now time.Time) (*legPaceModel, error) {
		started <- struct{}{}
		<-release
		fitCtxErr.Store(fmt.Sprint(ctx.Err()))
		return &legPaceModel{CruiseKn: 7}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := c.model(ctx, lpNow)
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want the caller's context error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a cancelled caller kept waiting on the fit")
	}
	close(release)
	// The next caller waits on the same fit, or finds it kept.
	if m, err := c.model(context.Background(), lpNow); err != nil || m.CruiseKn != 7 {
		t.Fatalf("next caller: %+v, %v", m, err)
	}
	if n := fits.Load(); n != 1 {
		t.Fatalf("%d fits, want 1: the fit carried on and was reused", n)
	}
	if got := fitCtxErr.Load(); got != "<nil>" {
		t.Fatalf("the fit's own context was cut by its caller: %v", got)
	}
}

func TestLegPace_AFitErrorSharedByWaitersIsNotKept(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	c, fits := lpCounted(func(ctx context.Context, now time.Time) (*legPaceModel, error) {
		if fail.Load() {
			return nil, errors.New("the passage record could not be read: disk gone")
		}
		return &legPaceModel{CruiseKn: 7}, nil
	})
	if _, err := c.model(context.Background(), lpNow); err == nil {
		t.Fatal("want the read error")
	}
	fail.Store(false)
	if m, err := c.model(context.Background(), lpNow); err != nil || m.CruiseKn != 7 {
		t.Fatalf("retry: %+v, %v", m, err)
	}
	if n := fits.Load(); n != 2 {
		t.Fatalf("%d fits, want 2", n)
	}
}

func TestLegPace_ConcurrentRequestsShareOneFit(t *testing.T) {
	release := make(chan struct{})
	c, fits := lpCounted(func(ctx context.Context, now time.Time) (*legPaceModel, error) {
		<-release
		return &legPaceModel{CruiseKn: 7}, nil
	})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if m, err := c.model(context.Background(), lpNow); err != nil || m.CruiseKn != 7 {
				t.Errorf("got %+v, %v", m, err)
			}
		}()
	}
	time.Sleep(50 * time.Millisecond) // the others are waiting on the first
	close(release)
	wg.Wait()
	if n := fits.Load(); n != 1 {
		t.Fatalf("eight concurrent requests fitted %d times, want 1", n)
	}
}

func TestLegPace_AHiddenPassageDropsOutAtTheNextRequest(t *testing.T) {
	eachBoat(t, func(t *testing.T, b lpBoat) {
		store := newTestPassageStore(t)
		recs := lpFleet(t, store, b, 3, 1) // 3 h in band over 3 passages: just enough
		if m, err := store.pace.model(context.Background(), lpNow); err != nil || m.Passages != 3 {
			t.Fatalf("first fit: %+v, %v", m, err)
		}
		if _, err := store.PatchPassage(recs[0].ID, passagePatch{Hidden: func() *bool { v := true; return &v }()}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.pace.model(context.Background(), lpNow); err == nil || !strings.Contains(err.Error(), "over 2 passages") {
			t.Fatalf("after hiding one: err = %v, want a pace from 2 passages that is not enough", err)
		}
	})
}

func TestLegPace_MotionBackfillAddingRPMRefitsTheModel(t *testing.T) {
	b := lpBoats[0]
	store := newTestPassageStore(t)
	for i := range 4 {
		lpAddPassage(t, store, 10+i*20, lpStretch{min: 60, kn: b.cruiseKn, dir: 1}) // no rpm yet
	}
	if _, err := store.pace.model(context.Background(), lpNow); err == nil || !strings.Contains(err.Error(), "no engine rpm recorded under way") {
		t.Fatalf("before the backfill: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE track_points SET rpm_mean = ?`, b.cruiseRP); err != nil {
		t.Fatal(err)
	}
	store.changed() // what backfillMotionSpan does after it stores
	if m, err := store.pace.model(context.Background(), lpNow); err != nil || !approx(m.CruiseKn, b.cruiseKn, 0.05) {
		t.Fatalf("after the backfill: %+v, %v", m, err)
	}
}

func TestLegPace_APassageAgingOutOfTwelveMonthsDropsOnTheNewDay(t *testing.T) {
	b := lpBoats[1]
	store := newTestPassageStore(t)
	lpAddPassage(t, store, 300, lpBerth(b), lpCruise(b, 1))
	lpAddPassage(t, store, 200, lpBerth(b), lpCruise(b, 1))
	lpAddPassage(t, store, 100, lpBerth(b), lpCruise(b, 1))
	if m, err := store.pace.model(context.Background(), lpNow); err != nil || m.Passages != 3 {
		t.Fatalf("first fit: %+v, %v", m, err)
	}
	// With no write at all, a day later the 300-day-old passage is 400 days old.
	if _, err := store.pace.model(context.Background(), lpNow.AddDate(0, 0, 100)); err == nil || !strings.Contains(err.Error(), "over 2 passages") {
		t.Fatalf("a new day should drop it: %v", err)
	}
}

// ── 11, 12, 13, 17: which cells are slow water ───────────────────────────────

func TestLegPace_SlowCells(t *testing.T) {
	cases := []struct {
		name string
		// per passage: the cell's time, as a share of the boat's cruise speed and revs
		passages int
		knShare  float64
		rpShare  float64
		slow     bool
	}{
		{"seen on one passage", 1, 0.2, 0.4, false},
		{"seen on two passages", 2, 0.2, 0.4, true},
		{"slow at cruise revs, a river against the stream", 3, 0.2, 1.0, false},
		{"slower, but over three quarters of cruise", 3, 0.8, 0.4, false},
		{"exactly three quarters of cruise", 3, 0.75, 0.4, true},
		{"low revs but as fast as cruise", 3, 1.0, 0.4, false},
	}
	for _, c := range cases {
		for _, b := range lpBoats {
			t.Run(c.name+"/"+b.name, func(t *testing.T) {
				ivs := lpCruiseIvs(b, 4)
				for p := range c.passages {
					ivs = append(ivs, lpIv(p, 600, b.cruiseKn*c.knShare, b.cruiseRP*c.rpShare, lpBerthLat))
				}
				m, err := fitLegPace(ivs)
				if err != nil {
					t.Fatal(err)
				}
				speed, slow := m.Slow[cellOf(lpBerthLat, 149.0)]
				if slow != c.slow {
					t.Fatalf("slow = %v (%.2f kn), want %v; cells %v", slow, speed, c.slow, m.Slow)
				}
				if slow && !approx(speed, b.cruiseKn*c.knShare, 1e-6) {
					t.Errorf("the cell's speed is its median, %.3f, got %.3f", b.cruiseKn*c.knShare, speed)
				}
			})
		}
	}
}

func TestLegPace_CruiseSpeedLeavesTheSlowCellsIntervalsOut(t *testing.T) {
	eachBoat(t, func(t *testing.T, b lpBoat) {
		ivs := lpCruiseIvs(b, 4) // 4 h at cruise
		// A berth cell on 3 passages: mostly low revs and slow, with enough
		// cruise-rev crawling in it (more than the cruising hours) to drag the
		// first pass's median down.
		for p := range 3 {
			ivs = append(ivs,
				lpIv(p, 6000, b.slowKn, b.slowRP, lpBerthLat),
				lpIv(p, 5000, b.cruiseKn*0.6, b.cruiseRP, lpBerthLat))
		}
		m, err := fitLegPace(ivs)
		if err != nil {
			t.Fatal(err)
		}
		if _, slow := m.Slow[cellOf(lpBerthLat, 149)]; !slow {
			t.Fatalf("the berth is slow water: %v", m.Slow)
		}
		if !approx(m.CruiseKn, b.cruiseKn, 1e-6) {
			t.Errorf("cruise = %.3f kn, want %.1f: the berth's crawl is not cruising", m.CruiseKn, b.cruiseKn)
		}
	})
}

// ── 14: cells snap by floor ──────────────────────────────────────────────────

func TestLegPace_CellsSnapByFloor(t *testing.T) {
	cases := []struct {
		lat, lon float64
		want     paceCell
	}{
		{0.005, 0.005, paceCell{1, 1}},
		{0.00499, 0.00499, paceCell{0, 0}},
		{0, 0, paceCell{0, 0}},
		{-0.0001, -0.0001, paceCell{-1, -1}},
		{-0.005, -0.005, paceCell{-1, -1}},
		{-0.00501, -0.00501, paceCell{-2, -2}},
	}
	for _, c := range cases {
		if got := cellOf(c.lat, c.lon); got != c.want || cellOf(c.lat, c.lon) != got {
			t.Errorf("cellOf(%v, %v) = %v, want %v", c.lat, c.lon, got, c.want)
		}
	}
}

// ── the leg's profile: 15, 16, 19 and the knots ──────────────────────────────

// lpLeg is a leg up a meridian, as long as the route is by the great circle.
func lpLeg(startLat, endLat float64) planLeg {
	route := [][2]float64{{startLat, 149.0}, {endLat, 149.0}}
	return planLeg{From: 0, To: 1, DistanceNm: lpNm(startLat, endLat), Route: route}
}

func lpNm(a, b float64) float64 { return haversineMeters(a, 149, b, 149) / 1852 }

// lpDeg is the degrees of latitude that make nm nautical miles.
func lpDeg(nm float64) float64 { return nm / lpNm(0, 1) }

// lpCellLow is the south edge of the berth cell.
var lpCellLow = math.Floor(lpBerthLat/legPaceCellDeg) * legPaceCellDeg

func lpModel(b lpBoat) *legPaceModel {
	return &legPaceModel{CruiseKn: b.cruiseKn, Slow: map[paceCell]float64{cellOf(lpBerthLat, 149.0): b.slowKn}}
}

func TestLegPace_ARouteClippingASlowCellIsTimedByItsLengthInside(t *testing.T) {
	eachBoat(t, func(t *testing.T, b lpBoat) {
		m := lpModel(b)
		cases := []struct {
			name       string
			start, end float64
			insideNm   float64
			tol        float64
		}{
			// 1.2 nm of open water then 0.15 nm into the cell: nine steps, the last inside.
			{"ends inside", lpCellLow - lpDeg(1.2), lpCellLow + lpDeg(0.15), 0.15, 1e-6},
			// Across the whole cell with open water either side: within a step of its width.
			{"across it", lpCellLow - lpDeg(0.6), lpCellLow + legPaceCellDeg + lpDeg(0.6), lpNm(lpCellLow, lpCellLow+legPaceCellDeg), 0.15},
			{"misses it", lpCellLow + 0.02, lpCellLow + 0.04, 0, 1e-6},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				leg := lpLeg(c.start, c.end)
				p, err := m.profile(leg)
				if err != nil {
					t.Fatal(err)
				}
				tl, err := p.pace(lpNow)
				if err != nil {
					t.Fatal(err)
				}
				// The nautical miles in slow water are what the slow hours were made from.
				slowNm := p.slowHours() * b.slowKn
				if math.Abs(slowNm-c.insideNm) > c.tol {
					t.Errorf("%.4f nm in slow water, want %.4f within %.3f", slowNm, c.insideNm, c.tol)
				}
				want := (leg.DistanceNm-slowNm)/b.cruiseKn + p.slowHours()
				if math.Abs(tl.hours()-want) > 1.0/3600 {
					t.Errorf("hours = %.4f, want %.4f for %.3f nm in slow water", tl.hours(), want, slowNm)
				}
			})
		}
	})
}

func TestLegPace_ABerthCellAtTheStartAddsRealMinutesAndTheBasisSaysSo(t *testing.T) {
	eachBoat(t, func(t *testing.T, b lpBoat) {
		m := lpModel(b)
		leg := lpLeg(lpBerthLat, lpBerthLat+lpDeg(3)) // 3 nm north
		p, err := m.profile(leg)
		if err != nil {
			t.Fatal(err)
		}
		tl, err := p.pace(lpNow)
		if err != nil {
			t.Fatal(err)
		}
		cruiseOnly := leg.DistanceNm / b.cruiseKn
		if tl.hours() <= cruiseOnly+2.0/60 {
			t.Errorf("hours %.3f vs %.3f at cruise: the berth should add minutes", tl.hours(), cruiseOnly)
		}
		slowMin := int(math.Round(p.slowHours()*60/5)) * 5
		want := fmt.Sprintf("Cruise %.1f kn, %d min in slow water.", b.cruiseKn, slowMin)
		if got := etaBasisText(p.cruiseKn, hoursDuration(p.slowHours())); got != want {
			t.Errorf("basis = %q, want %q", got, want)
		}
		if n := len(tl.knots); n != 3 {
			t.Errorf("a slow start then cruise has 3 knots, got %d", n)
		}
	})
}

func TestLegPace_NoSlowWaterIsAnOrdinaryTwoKnotLeg(t *testing.T) {
	eachBoat(t, func(t *testing.T, b lpBoat) {
		m := &legPaceModel{CruiseKn: b.cruiseKn, Slow: map[paceCell]float64{}}
		leg := lpLeg(-19, -18.5)
		p, err := m.profile(leg)
		if err != nil {
			t.Fatal(err)
		}
		tl, _ := p.pace(lpNow)
		if len(tl.knots) != 2 || math.Abs(tl.hours()-leg.DistanceNm/b.cruiseKn) > 1e-9 {
			t.Errorf("knots %d, hours %v, want 2 and %v", len(tl.knots), tl.hours(), leg.DistanceNm/b.cruiseKn)
		}
		if got, want := etaBasisText(p.cruiseKn, hoursDuration(p.slowHours())), fmt.Sprintf("Cruise %.1f kn.", b.cruiseKn); got != want {
			t.Errorf("basis = %q, want %q", got, want)
		}
	})
}

func TestLegPace_ALegIsWalkedOncePerPlanLegWork(t *testing.T) {
	b := lpBoats[0]
	leg := lpLeg(lpBerthLat, lpBerthLat+lpDeg(3))
	w := &planLegWork{}
	if err := w.setPace(leg, lpModel(b)); err != nil {
		t.Fatal(err)
	}
	first := w.profile
	// A second call (prepareUnderway after setHours) must not walk the route again:
	// a model that would give different steps leaves the first walk in place.
	if err := w.setPace(leg, &legPaceModel{CruiseKn: 3, Slow: map[paceCell]float64{}}); err != nil {
		t.Fatal(err)
	}
	if w.profile != first || w.profile.cruiseKn != b.cruiseKn {
		t.Errorf("the route was walked again: %+v", w.profile)
	}
}

func TestLegPace_APaceShiftsWithTheDepartureAndKeepsItsShape(t *testing.T) {
	eachBoat(t, func(t *testing.T, b lpBoat) {
		p, err := lpModel(b).profile(lpLeg(lpBerthLat, lpBerthLat+lpDeg(3)))
		if err != nil {
			t.Fatal(err)
		}
		d1 := lpNow
		d2 := lpNow.Add(7*time.Hour + 20*time.Minute)
		a, _ := p.pace(d1)
		c, _ := p.pace(d2)
		if a.duration() != c.duration() || len(a.knots) != len(c.knots) {
			t.Fatalf("the shape changed with the departure: %v vs %v", a.duration(), c.duration())
		}
	})
}

// ── 18: the wording ──────────────────────────────────────────────────────────

func TestLegPace_BasisWording(t *testing.T) {
	cases := []struct {
		kn   float64
		slow time.Duration
		want string
	}{
		{10.3, 0, "Cruise 10.3 kn."},
		{10.3, 40 * time.Minute, "Cruise 10.3 kn, 40 min in slow water."},
		{10.34, 42 * time.Minute, "Cruise 10.3 kn, 40 min in slow water."},
		{8, 43 * time.Minute, "Cruise 8.0 kn, 45 min in slow water."},
		{8, 62 * time.Minute, "Cruise 8.0 kn, 1 h in slow water."},
		{8, 70 * time.Minute, "Cruise 8.0 kn, 1 h 10 min in slow water."},
		{8, 120 * time.Minute, "Cruise 8.0 kn, 2 h in slow water."},
		{8, 2 * time.Minute, "Cruise 8.0 kn."},
	}
	for _, c := range cases {
		if got := etaBasisText(c.kn, c.slow); got != c.want {
			t.Errorf("etaBasisText(%v, %v) = %q, want %q", c.kn, c.slow, got, c.want)
		}
	}
}

// ── the heading mix and the fit's whole shape ────────────────────────────────

func TestLegPace_TheHeadingMixIsKeptByTimeInTheBand(t *testing.T) {
	eachBoat(t, func(t *testing.T, b lpBoat) {
		store := newTestPassageStore(t)
		for i := range 3 {
			lpAddPassage(t, store, 10+i*20, lpCruise(b, 1))
		}
		south := lpCruise(b, 1)
		south.dir = -1
		lpAddPassage(t, store, 5, south)
		m, err := learnLegPace(context.Background(), store, lpNow)
		if err != nil {
			t.Fatal(err)
		}
		// Octant 0 is north, 4 is south.
		if math.Abs(m.Heading[0]-3) > 0.1 || math.Abs(m.Heading[4]-1) > 0.1 {
			t.Errorf("heading mix = %v, want about 3 h north and 1 h south", m.Heading)
		}
	})
}

// ── the counter: what bumps it ───────────────────────────────────────────────

func lpComfortAsked(store *passageStore) bool {
	select {
	case <-store.comfort.wake:
		return true
	default:
		return false
	}
}

func TestLegPace_AChangeToThePassagesBumpsThePaceAndAsksForComfort(t *testing.T) {
	store := newTestPassageStore(t)
	before := store.pace.changes.Load()
	store.changed()
	if store.pace.changes.Load() == before {
		t.Fatal("a change to the passages did not bump the pace counter")
	}
	if !lpComfortAsked(store) {
		t.Fatal("a change to the passages did not ask for a comfort fit")
	}
}

func TestLegPace_ASeaOnlySignalLeavesThePaceAlone(t *testing.T) {
	store := newTestPassageStore(t)
	before := store.pace.changes.Load()
	store.comfort.signal()
	if store.pace.changes.Load() != before {
		t.Fatal("a comfort-only signal bumped the pace counter")
	}
}

// The pace counter moves alone: the runner uses it for a new passage, because the
// comfort worker is asked once the figures are stored.
func TestLegPace_BumpingThePaceDoesNotAskForComfort(t *testing.T) {
	store := newTestPassageStore(t)
	store.pace.bump()
	if lpComfortAsked(store) {
		t.Fatal("a pace bump asked for a comfort fit")
	}
}

// A passage the runner newly records changes what the pace learns from, and a run
// that records nothing bumps nothing.
func TestLegPace_ANewlyRecordedPassageBumpsThePace(t *testing.T) {
	store := newTestPassageStore(t)
	appendAll(t, store, simpleTrip(2*time.Hour).pts)
	before := store.pace.changes.Load()
	if err := runPassageDetection(store); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.ListPassages(); len(got) != 1 {
		t.Fatalf("want one recorded passage, got %d", len(got))
	}
	if n := store.pace.changes.Load(); n != before+1 {
		t.Fatalf("a newly recorded passage moved the counter from %d to %d, want one bump", before, n)
	}
	if err := runPassageDetection(store); err != nil {
		t.Fatal(err)
	}
	if n := store.pace.changes.Load(); n != before+1 {
		t.Errorf("a run that recorded nothing bumped the counter to %d", n)
	}
}

// ── a fit cut short ──────────────────────────────────────────────────────────

// lpFlipCtx reports its context cancelled once Err has been asked after n times, so
// a test can cancel at a chosen point of a learn without a clock.
type lpFlipCtx struct {
	context.Context
	left atomic.Int32
}

func (c *lpFlipCtx) Err() error {
	if c.left.Add(-1) < 0 {
		return context.Canceled
	}
	return nil
}

func TestLegPace_ALearnCutShortReturnsTheContextsError(t *testing.T) {
	b := lpBoats[0]
	store := newTestPassageStore(t)
	lpFleet(t, store, b, 3, 1)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if m, err := learnLegPace(cancelled, store, lpNow); m != nil || !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled context: got %+v, %v, want the context's error", m, err)
	}

	// Cancelled after the last passage was split out, with only the fit left to do:
	// one Err call for the list, one before the single read and one for each of the
	// three passages come first.
	ctx := &lpFlipCtx{Context: context.Background()}
	ctx.left.Store(5)
	if m, err := learnLegPace(ctx, store, lpNow); m != nil || !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled after the last read: got %+v, %v, want the context's error", m, err)
	}
}

// One read of the year split by passage windows gives what a read for each passage
// gave: passages that touch at a shared point, overlap, or begin where another
// ended all get their own points.
func TestLegPace_OneReadForTheYearSplitsIntoTheSamePassages(t *testing.T) {
	store := newTestPassageStore(t)
	b := lpBoats[0]
	lpFleet(t, store, b, 3, 1)
	// A passage that overlaps the first, and points that belong to none.
	first := lpNow.AddDate(0, 0, -10)
	overlap := passageRecord{Start: first.Add(30 * time.Minute), End: first.Add(80 * time.Minute), Title: "o"}
	if _, err := store.InsertPassage(overlap); err != nil {
		t.Fatal(err)
	}
	for k := range 4 {
		p := recordedTrackPoint{At: lpNow.AddDate(0, 0, -5).Add(time.Duration(k) * time.Minute), Segment: 99, Lat: -19, Lon: 149.0, trackContext: trackContext{RPMMean: pfloat(b.cruiseRP)}}
		if err := store.AppendTrackPoint(p); err != nil {
			t.Fatal(err)
		}
	}
	recs, err := visiblePassagesSince(context.Background(), store, lpNow.AddDate(-1, 0, 0))
	if err != nil || len(recs) != 4 {
		t.Fatalf("%d passages, %v", len(recs), err)
	}
	var want []paceInterval
	for i, r := range recs {
		pts, err := store.PaceTrackPointsBetween(context.Background(), r.Start, r.End)
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, paceIntervalsOf(i, pts)...)
	}
	wantModel, wantErr := fitLegPace(want)
	got, gotErr := learnLegPace(context.Background(), store, lpNow)
	if (wantErr == nil) != (gotErr == nil) || !reflect.DeepEqual(wantModel, got) {
		t.Fatalf("the single read learned %+v, %v; a read for each passage learned %+v, %v", got, gotErr, wantModel, wantErr)
	}
}

func TestLegPace_TheNarrowReadStopsOnACancelledContext(t *testing.T) {
	store := newTestPassageStore(t)
	rec := lpAddPassage(t, store, 10, lpBerth(lpBoats[0]), lpCruise(lpBoats[0], 1))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pts, err := store.PaceTrackPointsBetween(ctx, rec.Start, rec.End)
	if pts != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled read gave %d points, %v, want the context's error", len(pts), err)
	}
}

// ── on the wire ──────────────────────────────────────────────────────────────

// A leg under way that is not assessable shows the arrival from its fix, with the
// basis, and its hours and speed are those of the same timeline.
func TestLegPace_AnUnderwayLegShowsItsArrivalWithItsBasisAndKeepsItsHoursAndSpeed(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	p.Stops[0].DepartTime = "03:00" // before now (04:30) at the stop: past, so the leg is not assessable
	b := lpBoats[0]
	f := &paFake{
		pace:     &legPaceModel{CruiseKn: b.cruiseKn},
		underway: paUnderwayFrom(p, paAt(14, 3, 0), paAt(14, 4, 20)),
	}
	out := assessPlan(context.Background(), p, legs, paDeps(f), planAssessOptions{})
	l := out.Legs[0]
	if l.NoteKind != planKindPast {
		t.Fatalf("setup: leg 0 should be past its departure, got note %q (%v)", l.Note, l.NoteKind)
	}
	if l.Arrive == "" || l.EtaBasis == nil {
		t.Fatalf("the row shows the arrival from the fix and its basis, got %q and %v", l.Arrive, l.EtaBasis)
	}
	if l.Hours == nil || l.SpeedKn == nil || math.Abs(*l.Hours**l.SpeedKn-legs[0].DistanceNm) > 1e-6 {
		t.Errorf("hours %v x speed_kn %v should be the distance %v", l.Hours, l.SpeedKn, legs[0].DistanceNm)
	}
}

// A leg under way whose ETA is behind it has nothing ahead to word a basis for.
func TestLegPace_AnUnderwayLegWhoseETAHasPassedHasNoBasis(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	p.Stops[0].DepartTime = "00:00" // now is 04:30: depart plus the leg's hours is long past
	b := lpBoats[0]
	end := p.Stops[1]
	f := &paFake{
		pace: &legPaceModel{CruiseKn: b.cruiseKn},
		// The latest fix is at the end of the leg, 10 minutes ago.
		underway: &planUnderway{Lat: p.Stops[0].Lat, Lon: p.Stops[0].Lon, Start: paAt(14, 0, 0), FixLat: end.Lat, FixLon: end.Lon, FixAt: paAt(14, 4, 20)},
	}
	l := assessPlan(context.Background(), p, legs, paDeps(f), planAssessOptions{}).Legs[0]
	if l.Hours == nil || *l.Hours >= 4.5 {
		t.Fatalf("setup: the leg's hours %v must put the ETA before now", l.Hours)
	}
	if l.EtaBasis != nil {
		t.Fatalf("an ETA that has passed has a basis: %q", *l.EtaBasis)
	}
}

func TestLegPace_TheAssessmentCarriesEtaBasisAndNullWithoutAnETA(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	b := lpBoats[0]
	// The first mile or so out of the first stop is slow water.
	slow := map[paceCell]float64{}
	a, z := legs[0].Route[0], legs[0].Route[1]
	for k := range 100 {
		u := float64(k) / 100 * 0.05
		slow[cellOf(a[0]+(z[0]-a[0])*u, a[1]+(z[1]-a[1])*u)] = b.slowKn
	}
	f := &paFake{pace: &legPaceModel{CruiseKn: b.cruiseKn, Slow: slow}}
	out := assessPlan(context.Background(), p, legs, paDeps(f), planAssessOptions{})
	l := out.Legs[0]
	if l.EtaBasis == nil || !strings.HasPrefix(*l.EtaBasis, "Cruise 8.0 kn") || !strings.Contains(*l.EtaBasis, "in slow water") {
		t.Fatalf("eta basis = %v, leg %+v, problem %q", l.EtaBasis, l, out.Problem)
	}
	if l.Hours == nil || *l.Hours <= legs[0].DistanceNm/b.cruiseKn {
		t.Fatalf("hours = %v, want more than the cruise-only %v", l.Hours, legs[0].DistanceNm/b.cruiseKn)
	}
	// speed_kn is the average the hours were worked out at, so hours x speed is the
	// distance; the cruise speed is in eta_basis.
	if l.SpeedKn == nil || math.Abs(*l.Hours**l.SpeedKn-legs[0].DistanceNm) > 1e-6 || *l.SpeedKn >= b.cruiseKn {
		t.Errorf("speed_kn = %v with hours %v, want the average that makes hours x speed the distance %v, under the cruise %v",
			l.SpeedKn, *l.Hours, legs[0].DistanceNm, b.cruiseKn)
	}

	none := assessPlan(context.Background(), p, legs, paDeps(&paFake{speedErr: errors.New("no engine rpm recorded under way")}), planAssessOptions{})
	if none.Legs[0].EtaBasis != nil {
		t.Errorf("a leg with no ETA has a basis: %q", *none.Legs[0].EtaBasis)
	}
	raw, err := json.Marshal(none.Legs[0])
	if err != nil || !strings.Contains(string(raw), `"eta_basis":null`) {
		t.Errorf("eta_basis must always be on the wire, null without an ETA: %s %v", raw, err)
	}
	if !strings.Contains(none.Problem, "No ETA: no engine rpm recorded under way") {
		t.Errorf("problem = %q", none.Problem)
	}
}

// A basis words the hours of an ETA, so a leg that shows no arrival has none. This
// leg's best departure runs past the forecast, so it shows no departure or arrival,
// though its hours (from now) are worked out.
func TestLegPace_ALegWithNoArrivalHasNoBasis(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	f := &paFake{
		speed: 3, // 10 hours: the 2.00 PM departure arrives at midnight, past the forecast
		fc:    pwUniform(10, 190, 12, nil, 0, paAt(14, 0, 0), paAt(14, 23, 0)),
		departures: func() ([]tidalDeparture, error) {
			return []tidalDeparture{paDeparture(14, 0, 0.4)}, nil
		},
	}
	l := paAssess(t, p, legs, f).Legs[0]
	if l.Arrive != "" {
		t.Fatalf("setup: the leg should show no arrival, got %q", l.Arrive)
	}
	if l.EtaBasis != nil {
		t.Errorf("a leg with no arrival has a basis: %q", *l.EtaBasis)
	}

	// With the operator's departure the arrival shows, and the basis with it.
	p.Stops[0].DepartTime = "08:00"
	with := paAssess(t, p, legs, &paFake{}).Legs[0]
	if with.Arrive == "" || with.EtaBasis == nil || *with.EtaBasis != "Cruise 6.0 kn." {
		t.Errorf("a leg with an arrival: arrive %q, basis %v", with.Arrive, with.EtaBasis)
	}
}

// A final run so short that its share of the leg is lost in the rounding of the
// sums before it (a micron of a leg of nautical miles) adds no knot of its own, and
// the timeline still builds whatever the distance the leg is scaled to.
func TestLegPace_ATinyFinalRunOfAnotherSpeedIsFoldedIn(t *testing.T) {
	b := lpBoats[0]
	boundary := lpCellLow + legPaceCellDeg // the north edge of the berth cell
	mid, end := boundary-1e-14, boundary+9e-14
	m := &legPaceModel{CruiseKn: b.cruiseKn, Slow: map[paceCell]float64{cellOf(mid+(end-mid)*0.5, 149.0): b.slowKn}}
	for _, dist := range []float64{lpNm(boundary-0.05, boundary), 7.123456789, 29.99999999, 30, 0.1 + 0.2, 1.0 / 3} {
		leg := planLeg{From: 0, To: 1, DistanceNm: dist, Route: [][2]float64{{boundary - 0.05, 149.0}, {mid, 149.0}, {end, 149.0}}}
		p, err := m.profile(leg)
		if err != nil {
			t.Fatal(err)
		}
		tl, err := p.pace(lpNow)
		if err != nil {
			t.Fatalf("distance %v: %v", dist, err)
		}
		if n := len(tl.knots); n != 2 {
			t.Errorf("distance %v: %d knots, a millimetre of slow water at the end is not a stretch of its own", dist, n)
		}
	}
}

func TestLegPace_GetPlansPassesTheBasisThrough(t *testing.T) {
	p, _ := paPlan(t, "2026-11-14", 0, 0)
	basis := "Cruise 8.0 kn, 10 min in slow water."
	got, err := planReportLegOf(p, planLegAssessment{Leg: 0, Date: "2026-11-14", EtaBasis: &basis}, planLeg{})
	if err != nil || got.EtaBasis == nil || *got.EtaBasis != basis {
		t.Fatalf("report leg = %+v, %v", got, err)
	}
}

// ── how long it takes ────────────────────────────────────────────────────────

func BenchmarkLegPaceFit(b *testing.B) {
	boat := lpBoats[0]
	var ivs []paceInterval
	for p := range 20 {
		for k := range 4000 {
			cell := float64(k%200) * 0.004
			rpm := boat.cruiseRP + float64(k%7)*10
			kn := boat.cruiseKn + float64(k%5)*0.1
			if k < 150 {
				rpm, kn = boat.slowRP, boat.slowKn
				cell = 0
			}
			ivs = append(ivs, lpIv(p, 30, kn, rpm, -20+cell))
		}
	}
	b.ResetTimer()
	for range b.N {
		if _, err := fitLegPace(ivs); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLearnLegPace is the whole learn from a store of 20 passages of 4000
// points each, the read and the walk as well as the fit.
func BenchmarkLearnLegPace(b *testing.B) {
	boat := lpBoats[0]
	store, err := newPassageStore(filepath.Join(b.TempDir(), "bench.sqlite"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.close() })
	tx, err := store.db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	const step = 5 * time.Second
	var recs []passageRecord
	for p := range 20 {
		start := lpNow.AddDate(0, 0, -10-10*p)
		at, lat := start, lpBerthLat+0.1*float64(p)
		for k := range 4000 {
			at = at.Add(step)
			lat += boat.cruiseKn * step.Hours() / 60
			rpm := boat.cruiseRP + float64(k%7)*10
			if err := insertTrackPoint(tx, recordedTrackPoint{
				At: at, Segment: p, Lat: lat, Lon: 149.0, SOGKts: pfloat(boat.cruiseKn),
				trackContext: trackContext{RPMMean: pfloat(rpm)},
			}); err != nil {
				b.Fatal(err)
			}
		}
		recs = append(recs, passageRecord{Start: start, End: at, Title: "t"})
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	for _, r := range recs {
		if _, err := store.InsertPassage(r); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for range b.N {
		if _, err := learnLegPace(context.Background(), store, lpNow); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLegPaceProfile(b *testing.B) {
	boat := lpBoats[0]
	m := lpModel(boat)
	for i := range 30 {
		m.Slow[cellOf(-20.0+0.01*float64(i), 149.0)] = boat.slowKn
	}
	leg := lpLeg(-20.0, -19.0) // 60 nm
	b.ResetTimer()
	for range b.N {
		if _, err := m.profile(leg); err != nil {
			b.Fatal(err)
		}
	}
}
