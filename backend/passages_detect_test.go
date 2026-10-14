package main

import (
	"testing"
	"time"
)

const (
	sc = time.Second
	mn = time.Minute
)

// A boat sitting for 30 mn, running 2 h at 6 kn, sitting again for 2 h.
func simpleTrip(rest time.Duration) *trackBuilder {
	b := newTrackBuilder()
	b.run(30*mn, 10*mn, 0)
	b.run(2*time.Hour, 20*sc, 6)
	b.run(rest, 10*mn, 0)
	return b
}

func TestDetectorFindsOnePassageBackdatedToLastRestPoint(t *testing.T) {
	b := simpleTrip(2 * time.Hour)
	lastRest := b.pts[2] // third 10-minute rest point, immediately before the run
	got := detectLoggedPassages(b.pts)
	if len(got.Closed) != 1 {
		t.Fatalf("want 1 passage, got %d: %+v", len(got.Closed), got.Closed)
	}
	p := got.Closed[0]
	if !p.Start.Equal(lastRest.At) {
		t.Errorf("start %v, want last at-rest point %v", p.Start, lastRest.At)
	}
	// arrival is the first moment at rest, not 60 minutes later
	wantEnd := passageEpoch.Add(30*mn + 2*time.Hour + 10*mn)
	if !p.End.Equal(wantEnd) {
		t.Errorf("end %v, want first rest point %v", p.End, wantEnd)
	}
	if !approx(p.DistanceNM, 12, 0.2) {
		t.Errorf("distance %.2f NM, want about 12", p.DistanceNM)
	}
}

func TestDetectorKeepsLunchStopOfFortyFiveMinutesInside(t *testing.T) {
	b := newTrackBuilder()
	b.run(30*mn, 10*mn, 0)
	b.run(time.Hour, 20*sc, 6)
	b.run(45*mn, 5*mn, 0)
	b.run(time.Hour, 20*sc, 6)
	b.run(2*time.Hour, 10*mn, 0)
	got := detectLoggedPassages(b.pts)
	if len(got.Closed) != 1 {
		t.Fatalf("a 45 minute stop must stay inside one passage, got %d", len(got.Closed))
	}
}

func TestDetectorSplitsAtSeventyMinuteStop(t *testing.T) {
	b := newTrackBuilder()
	b.run(30*mn, 10*mn, 0)
	b.run(time.Hour, 20*sc, 6)
	b.run(70*mn, 5*mn, 0)
	b.run(time.Hour, 20*sc, 6)
	b.run(2*time.Hour, 10*mn, 0)
	got := detectLoggedPassages(b.pts)
	if len(got.Closed) != 2 {
		t.Fatalf("a 70 minute stop must split, got %d", len(got.Closed))
	}
	if !got.Closed[0].End.Before(got.Closed[1].Start) {
		t.Errorf("passages overlap: %+v", got.Closed)
	}
}

func TestDetectorEndsWhenAnchorIsDropped(t *testing.T) {
	b := newTrackBuilder()
	b.run(30*mn, 10*mn, 0)
	b.run(time.Hour, 20*sc, 6)
	b.run(10*mn, 20*sc, 0.5) // slowing to set the hook
	dropped := b.last().At
	b.run(20*mn, 5*mn, 0)
	for i := range b.pts {
		if !b.pts[i].At.Before(dropped) {
			d := dropped
			b.pts[i].AnchorSetAt = &d
		}
	}
	got := detectLoggedPassages(b.pts)
	if len(got.Closed) != 1 {
		t.Fatalf("want 1 passage ended by the anchor, got %d (open %v)", len(got.Closed), got.Open)
	}
	if got.Closed[0].End.After(dropped) {
		t.Errorf("end %v is after the drop %v; the anchor must end it without waiting an hour", got.Closed[0].End, dropped)
	}
	if got.Open != nil {
		t.Errorf("passage still open after anchor drop")
	}
}

func TestDetectorIgnoresAnchorSetBeforeDeparture(t *testing.T) {
	b := simpleTrip(2 * time.Hour)
	old := passageEpoch.Add(-24 * time.Hour)
	for i := range b.pts {
		b.pts[i].AnchorSetAt = &old
	}
	if got := detectLoggedPassages(b.pts); len(got.Closed) != 1 {
		t.Fatalf("a watch set before departure must not end the passage, got %d", len(got.Closed))
	}
}

func TestDetectorDropsShortRuns(t *testing.T) {
	// 10 minutes at 6 kn is 1 NM but under 15 minutes
	b := newTrackBuilder()
	b.run(30*mn, 10*mn, 0)
	b.run(10*mn, 20*sc, 6)
	b.run(2*time.Hour, 2*mn, 0)
	if got := detectLoggedPassages(b.pts); len(got.Closed) != 0 {
		t.Errorf("under 15 minutes must be dropped, got %+v", got.Closed)
	}
	// 20 minutes at 3.2 kn is 1.07 NM, but 20 minutes at 3 kn... use under 0.5 NM:
	// 20 minutes at 3 kn is 1 NM; a slow 20 mn creep at 1.4 kn never starts.
	b = newTrackBuilder()
	b.run(30*mn, 10*mn, 0)
	b.run(20*mn, 20*sc, 1.4)
	b.run(2*time.Hour, 10*mn, 0)
	if got := detectLoggedPassages(b.pts); len(got.Closed) != 0 {
		t.Errorf("a creep below 3 kn is not a passage, got %+v", got.Closed)
	}
}

func TestDetectorDropsRunUnderHalfMile(t *testing.T) {
	// Starts (3 kn held 2 mn) and lasts 16 minutes, but 3 kn for the first
	// 2 minutes then 0.9 kn is 0.1 + 0.24 NM.
	b := newTrackBuilder()
	b.run(30*mn, 10*mn, 0)
	b.run(2*mn+20*sc, 20*sc, 3.2)
	b.run(14*mn, 20*sc, 1.1)
	b.run(2*time.Hour, 10*mn, 0)
	got := detectLoggedPassages(b.pts)
	if len(got.Closed) != 0 {
		t.Errorf("under 0.5 NM must be dropped, got %+v", got.Closed)
	}
}

func TestDetectorNeedsTwoMinutesAtThreeKnots(t *testing.T) {
	b := newTrackBuilder()
	b.run(30*mn, 10*mn, 0)
	b.run(100*sc, 20*sc, 5) // a wake bump: under 2 minutes
	b.run(2*time.Hour, 10*mn, 0)
	if got := detectLoggedPassages(b.pts); len(got.Closed) != 0 || got.Open != nil {
		t.Errorf("100 s at 5 kn must not start a passage: %+v", got)
	}
}

func TestDetectorReportsOpenPassageAndResumePoint(t *testing.T) {
	b := newTrackBuilder()
	b.run(30*mn, 10*mn, 0)
	rest := b.last().At
	b.run(time.Hour, 20*sc, 6)
	got := detectLoggedPassages(b.pts)
	if len(got.Closed) != 0 || got.Open == nil {
		t.Fatalf("want an open passage only, got %+v", got)
	}
	if !got.Resume.Equal(rest) {
		t.Errorf("resume %v, want the passage start %v", got.Resume, rest)
	}
}

func TestDetectorIncrementalRunMatchesOneShot(t *testing.T) {
	b := simpleTrip(2 * time.Hour)
	full := detectLoggedPassages(b.pts)

	cut := 3 + 270 // part-way through the run
	first := detectLoggedPassages(b.pts[:cut])
	var rest []recordedTrackPoint
	for _, p := range b.pts {
		if !p.At.Before(first.Resume) {
			rest = append(rest, p)
		}
	}
	second := detectLoggedPassages(rest)
	// first.Open then re-found from Resume must produce the identical passage
	if len(second.Closed) != len(full.Closed) || !second.Closed[0].Start.Equal(full.Closed[0].Start) || !second.Closed[0].End.Equal(full.Closed[0].End) {
		t.Errorf("incremental %+v differs from one-shot %+v", second.Closed, full.Closed)
	}
}

func TestDetectorSegmentBreakEndsAtLastMovingPoint(t *testing.T) {
	b := newTrackBuilder()
	b.run(30*mn, 10*mn, 0)
	b.run(time.Hour, 20*sc, 6)
	lastMoving := b.last().At
	b.gap(5 * time.Hour)
	b.run(30*mn, 10*mn, 0)
	got := detectLoggedPassages(b.pts)
	if len(got.Closed) != 1 || !got.Closed[0].End.Equal(lastMoving) {
		t.Errorf("want end at the last moving point %v, got %+v", lastMoving, got.Closed)
	}
}

func TestDetectorDistanceSkipsSegmentBreaks(t *testing.T) {
	b := newTrackBuilder()
	b.run(30*mn, 10*mn, 0)
	b.run(30*mn, 20*sc, 6)
	b.gap(8 * mn)
	b.lat += 5 // a 300 NM jump across the break must not count
	b.run(30*mn, 20*sc, 6)
	b.run(2*time.Hour, 10*mn, 0)
	got := detectLoggedPassages(b.pts)
	if len(got.Closed) != 1 {
		t.Fatalf("want 1 passage, got %d", len(got.Closed))
	}
	if !approx(got.Closed[0].DistanceNM, 6, 0.3) {
		t.Errorf("distance %.2f, want about 6 (break not bridged)", got.Closed[0].DistanceNM)
	}
}
