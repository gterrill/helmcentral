package main

import (
	"testing"
	"time"
)

func newTestRecorder(t *testing.T) (*trackRecorder, *passageStore) {
	t.Helper()
	store := newTestPassageStore(t)
	rec, err := newTrackRecorder(store)
	if err != nil {
		t.Fatalf("newTrackRecorder: %v", err)
	}
	return rec, store
}

func fix(at time.Time, lat, lon float64) trackSample {
	return trackSample{At: at, Lat: lat, Lon: lon, FixOK: true}
}

func count(t *testing.T, s *passageStore) int {
	t.Helper()
	pts, err := s.TrackPointsSince(passageEpoch.Add(-24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return len(pts)
}

// 25 m of latitude is 0.000225 degrees.
const metres25 = 25.0 / 111195.0

func TestRecorderKeepsFirstFix(t *testing.T) {
	rec, store := newTestRecorder(t)
	kept, err := rec.Observe(fix(passageEpoch, -20, 149))
	if err != nil || !kept || count(t, store) != 1 {
		t.Fatalf("first fix: kept=%v err=%v rows=%d", kept, err, count(t, store))
	}
}

func TestRecorderKeepRuleNeedsBothDistanceAndTime(t *testing.T) {
	rec, store := newTestRecorder(t)
	rec.Observe(fix(passageEpoch, -20, 149))

	// 30 m moved but only 3 s: not kept
	if kept, _ := rec.Observe(fix(passageEpoch.Add(3*time.Second), -20+1.2*metres25, 149)); kept {
		t.Error("moved 30 m in 3 s must not be kept")
	}
	// 10 m moved after 6 s: not kept
	if kept, _ := rec.Observe(fix(passageEpoch.Add(6*time.Second), -20+0.4*metres25, 149)); kept {
		t.Error("moved 10 m must not be kept")
	}
	// 30 m moved after 6 s: kept
	if kept, _ := rec.Observe(fix(passageEpoch.Add(7*time.Second), -20+1.2*metres25, 149)); !kept {
		t.Error("moved 30 m after 7 s must be kept")
	}
	if n := count(t, store); n != 2 {
		t.Errorf("rows %d, want 2", n)
	}
}

func TestRecorderKeepsOnePointEveryTenMinutesAtRest(t *testing.T) {
	rec, store := newTestRecorder(t)
	rec.Observe(fix(passageEpoch, -20, 149))
	for s := 5; s < 600; s += 5 {
		rec.Observe(fix(passageEpoch.Add(time.Duration(s)*time.Second), -20, 149))
	}
	if n := count(t, store); n != 1 {
		t.Fatalf("rows %d before ten minutes, want 1", n)
	}
	if kept, _ := rec.Observe(fix(passageEpoch.Add(10*time.Minute), -20, 149)); !kept {
		t.Error("the ten-minute rest point must be kept")
	}
}

func TestRecorderGeneratorChangeForcesAPoint(t *testing.T) {
	rec, store := newTestRecorder(t)
	s := fix(passageEpoch, -20, 149)
	s.GenState = pstr("stopped")
	rec.Observe(s)

	s2 := fix(passageEpoch.Add(5*time.Second), -20, 149)
	s2.GenState = pstr("stopped")
	if kept, _ := rec.Observe(s2); kept {
		t.Error("unchanged generator state must not force a point")
	}
	s3 := fix(passageEpoch.Add(10*time.Second), -20, 149)
	s3.GenState = pstr("running")
	if kept, _ := rec.Observe(s3); !kept {
		t.Error("generator starting must force a point")
	}
	s4 := fix(passageEpoch.Add(15*time.Second), -20, 149)
	s4.GenState = pstr("running")
	if kept, _ := rec.Observe(s4); kept {
		t.Error("the new state is now the baseline")
	}
	if n := count(t, store); n != 2 {
		t.Errorf("rows %d, want 2", n)
	}
}

func TestRecorderRejectsBadFixes(t *testing.T) {
	cases := map[string]trackSample{
		"sentinel":        fix(passageEpoch.Add(time.Hour), -1, -1),
		"null island":     fix(passageEpoch.Add(time.Hour), 0, 0),
		"latitude range":  fix(passageEpoch.Add(time.Hour), 91, 149),
		"longitude range": fix(passageEpoch.Add(time.Hour), -20, 181),
		"critical":        {At: passageEpoch.Add(time.Hour), Lat: -20, Lon: 149.1, FixOK: false},
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			rec, store := newTestRecorder(t)
			rec.Observe(fix(passageEpoch, -20, 149))
			if kept, err := rec.Observe(s); kept || err != nil {
				t.Errorf("kept=%v err=%v, want rejected", kept, err)
			}
			if n := count(t, store); n != 1 {
				t.Errorf("rows %d, want 1", n)
			}
		})
	}
}

func TestRecorderRejectsJumpImplyingMoreThanFortyKnots(t *testing.T) {
	rec, store := newTestRecorder(t)
	rec.Observe(fix(passageEpoch, -20, 149))
	// 1 NM in 60 s is 60 kn
	if kept, _ := rec.Observe(fix(passageEpoch.Add(time.Minute), -20+1.0/60, 149)); kept {
		t.Error("60 kn jump must be rejected")
	}
	// 0.5 NM in 60 s is 30 kn: plausible for a fast boat, kept
	if kept, _ := rec.Observe(fix(passageEpoch.Add(2*time.Minute), -20+0.5/60, 149)); !kept {
		t.Error("30 kn must be kept")
	}
	if n := count(t, store); n != 2 {
		t.Errorf("rows %d, want 2", n)
	}
}

func TestRecorderStartsNewSegmentAfterTenMinuteGapWithoutFix(t *testing.T) {
	rec, store := newTestRecorder(t)
	rec.Observe(fix(passageEpoch, -20, 149))
	rec.Observe(fix(passageEpoch.Add(5*time.Minute), -20, 149))
	// 11 minutes of nothing but rejected fixes
	rec.Observe(trackSample{At: passageEpoch.Add(8 * time.Minute), Lat: -1, Lon: -1, FixOK: true})
	rec.Observe(fix(passageEpoch.Add(16*time.Minute+time.Second), -20, 149))
	pts, _ := store.TrackPointsSince(passageEpoch)
	last := pts[len(pts)-1]
	if last.Segment != 1 {
		t.Errorf("segment %d after an 11 minute fix gap, want 1", last.Segment)
	}
	if pts[0].Segment != 0 {
		t.Errorf("first segment %d, want 0", pts[0].Segment)
	}
}

func TestRecorderGapIsMeasuredFromLastGoodFixNotLastKeptPoint(t *testing.T) {
	// A boat at anchor keeps a point every 10 minutes; the 9 minutes between
	// are good fixes and must not read as a gap.
	rec, store := newTestRecorder(t)
	for s := 0; s <= 25*60; s += 5 {
		rec.Observe(fix(passageEpoch.Add(time.Duration(s)*time.Second), -20, 149))
	}
	pts, _ := store.TrackPointsSince(passageEpoch)
	for _, p := range pts {
		if p.Segment != 0 {
			t.Fatalf("anchored boat split into segment %d at %v", p.Segment, p.At)
		}
	}
}

func TestRecorderGustIsMaxTrueWindSinceLastKeptPoint(t *testing.T) {
	rec, store := newTestRecorder(t)
	rec.Observe(fix(passageEpoch, -20, 149))
	for i, w := range []float64{10, 18, 12} {
		s := fix(passageEpoch.Add(time.Duration(i+1)*5*time.Second), -20, 149)
		s.TWSKts = pfloat(w)
		rec.Observe(s)
	}
	s := fix(passageEpoch.Add(10*time.Minute), -20, 149)
	s.TWSKts = pfloat(11)
	rec.Observe(s)
	pts, _ := store.TrackPointsSince(passageEpoch)
	got := pts[len(pts)-1]
	if got.GustKts == nil || *got.GustKts != 18 {
		t.Errorf("gust %v, want 18", got.GustKts)
	}
	if got.TWSKts == nil || *got.TWSKts != 11 {
		t.Errorf("tws %v, want the instantaneous 11", got.TWSKts)
	}
}

func TestRecorderStoresAbsentReadingsAsNull(t *testing.T) {
	rec, store := newTestRecorder(t)
	s := fix(passageEpoch, -20, 149)
	s.SOGKts = pfloat(0)
	rec.Observe(s)
	pts, _ := store.TrackPointsSince(passageEpoch)
	p := pts[0]
	if p.SOGKts == nil || *p.SOGKts != 0 {
		t.Errorf("sog %v, want a present zero", p.SOGKts)
	}
	if p.TWSKts != nil || p.GustKts != nil || p.SoCPct != nil || p.FuelLph != nil || p.WaterL != nil || p.GenState != nil || p.COGDeg != nil {
		t.Errorf("absent readings must stay absent: %+v", p)
	}
}

func TestRecorderResumesFromStoredLog(t *testing.T) {
	store := newTestPassageStore(t)
	rec, _ := newTrackRecorder(store)
	rec.Observe(fix(passageEpoch, -20, 149))
	rec.Observe(fix(passageEpoch.Add(15*time.Minute), -20, 149)) // new segment
	rec2, err := newTrackRecorder(store)
	if err != nil {
		t.Fatal(err)
	}
	// 3 s later, same place: a restarted recorder must know the last kept point
	if kept, _ := rec2.Observe(fix(passageEpoch.Add(15*time.Minute+3*time.Second), -20, 149)); kept {
		t.Error("restarted recorder lost its baseline")
	}
	rec2.Observe(fix(passageEpoch.Add(25*time.Minute+time.Second), -20, 149))
	pts, _ := store.TrackPointsSince(passageEpoch)
	if got := pts[len(pts)-1].Segment; got != 1 {
		t.Errorf("segment after restart %d, want 1", got)
	}
}

func TestRecorderForcesPointWhenBoatComesToRest(t *testing.T) {
	rec, store := newTestRecorder(t)
	s := fix(passageEpoch, -20, 149)
	s.SOGKts = pfloat(5)
	rec.Observe(s)
	// stopped dead: no 25 m movement, only 5 s later
	s2 := fix(passageEpoch.Add(5*time.Second), -20, 149)
	s2.SOGKts = pfloat(0.4)
	if kept, _ := rec.Observe(s2); !kept {
		t.Fatal("first sample below 1 kn after moving must be kept")
	}
	s3 := fix(passageEpoch.Add(10*time.Second), -20, 149)
	s3.SOGKts = pfloat(0.3)
	if kept, _ := rec.Observe(s3); kept {
		t.Error("only the first rest sample is forced")
	}
	if n := count(t, store); n != 2 {
		t.Errorf("rows %d, want 2", n)
	}
}

func TestRecorderRecoversFromBadKeptFixAfterTwoMinutes(t *testing.T) {
	rec, store := newTestRecorder(t)
	rec.Observe(fix(passageEpoch, -20, 149))
	// a bad fix 2 NM away squeaks in over a long enough interval
	rec.Observe(fix(passageEpoch.Add(10*time.Minute), -20.05, 149))
	// then the real position returns; each is a >40 kn jump from the bad point
	var recovered time.Time
	for s := 5; s <= 200; s += 5 {
		at := passageEpoch.Add(10*time.Minute + time.Duration(s)*time.Second)
		if kept, _ := rec.Observe(fix(at, -20, 149)); kept && recovered.IsZero() {
			recovered = at
		}
	}
	// the bad fix must have been tested: it was at -20.05, so recovery must come
	if recovered.IsZero() {
		t.Fatal("recorder never recovered from a bad kept fix")
	}
	if d := recovered.Sub(passageEpoch.Add(10 * time.Minute)); d < 2*time.Minute || d > 2*time.Minute+10*time.Second {
		t.Errorf("recovered after %v, want about 2 minutes", d)
	}
	pts, _ := store.TrackPointsSince(passageEpoch)
	last := pts[len(pts)-1]
	if last.Lat != -20 || last.Segment != pts[len(pts)-2].Segment+1 {
		t.Errorf("recovered point must start a new segment: %+v after %+v", last, pts[len(pts)-2])
	}
}

func TestRecorderSingleIsolatedJumpStaysRejected(t *testing.T) {
	rec, store := newTestRecorder(t)
	rec.Observe(fix(passageEpoch, -20, 149))
	rec.Observe(fix(passageEpoch.Add(5*time.Second), -20.05, 149)) // glitch
	for s := 10; s <= 300; s += 5 {
		rec.Observe(fix(passageEpoch.Add(time.Duration(s)*time.Second), -20, 149))
	}
	pts, _ := store.TrackPointsSince(passageEpoch)
	for _, p := range pts {
		if p.Lat != -20 || p.Segment != 0 {
			t.Fatalf("glitch leaked or split the track: %+v", p)
		}
	}
}
