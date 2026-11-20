package main

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

// feedWindow drives the accumulator with a 10 Hz sine from start to end seconds
// of sensor time.
func feedWindow(m *motionAccumulator, start, end float64) {
	feedSine(m, start, end, 10, 5, 3, 0, 0, nil)
}

func TestRecorderTakesMotionFiguresForTheWindowSinceTheLastKeptPoint(t *testing.T) {
	rec, store := newTestRecorder(t)
	rec.motion = newMotionAccumulator()

	feedWindow(rec.motion, 0, 12)
	rec.Observe(fix(passageEpoch, -20, 149))
	feedWindow(rec.motion, 12, 24)
	rec.Observe(fix(passageEpoch.Add(10*time.Minute), -20, 149))
	rec.Observe(fix(passageEpoch.Add(20*time.Minute), -20, 149))

	pts, err := store.TrackPointsSince(passageEpoch)
	if err != nil || len(pts) != 3 {
		t.Fatalf("points %d, err %v", len(pts), err)
	}
	for i, p := range pts[:2] {
		if p.MotionRateHz == nil || math.Abs(*p.MotionRateHz-10) > 0.2 {
			t.Errorf("point %d motion rate %v, want ~10", i, p.MotionRateHz)
		}
		if p.RollRMSDeg == nil || p.RollAccelPeakDps2 == nil || p.RollJerkP95Dps3 == nil || p.HeaveSDM != nil {
			t.Errorf("point %d figures: %+v", i, p.motionFigures)
		}
	}
	if pts[2].motionFigures != (motionFigures{}) {
		t.Errorf("a window with no samples must stay null, got %+v", pts[2].motionFigures)
	}
}

// The second point's window continues the filter: its first derivatives are
// present without waiting for the filter to fill again.
func TestRecorderWindowResetKeepsTheFilterHistory(t *testing.T) {
	rec, store := newTestRecorder(t)
	rec.motion = newMotionAccumulator()
	feedWindow(rec.motion, 0, 10)
	rec.Observe(fix(passageEpoch, -20, 149))
	feedWindow(rec.motion, 10, 10.6)
	rec.Observe(fix(passageEpoch.Add(10*time.Minute), -20, 149))
	pts, _ := store.TrackPointsSince(passageEpoch)
	if got := pts[1]; got.RollJerkP95Dps3 == nil {
		t.Errorf("0.6 s of new samples should still carry jerk: %+v", got.motionFigures)
	}
}

func TestRecorderWithoutAMotionStreamStoresNullMotion(t *testing.T) {
	rec, store := newTestRecorder(t)
	rec.Observe(fix(passageEpoch, -20, 149))
	pts, _ := store.TrackPointsSince(passageEpoch)
	if pts[0].motionFigures != (motionFigures{}) {
		t.Errorf("no stream, no figures: %+v", pts[0].motionFigures)
	}
}

func TestRecorderStoresTheContextReadingsOnTheKeptSample(t *testing.T) {
	rec, store := newTestRecorder(t)
	s := fix(passageEpoch, -20, 149)
	s.HeadingDeg, s.TWADeg, s.STWKts, s.RPMMean = pfloat(271), pfloat(-48), pfloat(9.6), pfloat(1810)
	rec.Observe(s)
	pts, _ := store.TrackPointsSince(passageEpoch)
	p := pts[0]
	if p.HeadingDeg == nil || *p.HeadingDeg != 271 || p.TWADeg == nil || *p.TWADeg != -48 ||
		p.STWKts == nil || *p.STWKts != 9.6 || p.RPMMean == nil || *p.RPMMean != 1810 {
		t.Errorf("context readings: %+v", p)
	}
}

func allMotionFigures() motionFigures {
	return motionFigures{
		RollRMSDeg: pfloat(1.1), RollRatePeakDps: pfloat(2.2), RollRateRMSDps: pfloat(3.3),
		RollAccelPeakDps2: pfloat(4.4), RollJerkP95Dps3: pfloat(5.5),
		PitchRMSDeg: pfloat(6.6), PitchAccelPeakDps2: pfloat(7.7), PitchJerkP95Dps3: pfloat(8.8),
		HeaveSDM: pfloat(0.12), VertAccelP95Ms2: pfloat(0.9), VertAccelPeakMs2: pfloat(1.4),
		MotionRateHz: pfloat(10.1), HeaveRateHz: pfloat(9.9),
	}
}

func TestSnapAndContextColumnsRoundTripThroughTheStore(t *testing.T) {
	store := newTestPassageStore(t)
	p := recordedTrackPoint{
		At: passageEpoch, Lat: -20, Lon: 149, motionFigures: allMotionFigures(),
		trackContext: trackContext{HeadingDeg: pfloat(12), TWADeg: pfloat(-130), STWKts: pfloat(8.2), RPMMean: pfloat(1795)},
	}
	if err := store.AppendTrackPoint(p); err != nil {
		t.Fatal(err)
	}
	pts, _ := store.TrackPointsSince(passageEpoch)
	got := pts[0]
	if got.motionFigures != p.motionFigures {
		// Pointer fields compare by address, so compare by value.
		a, _ := json.Marshal(got.motionFigures)
		b, _ := json.Marshal(p.motionFigures)
		if string(a) != string(b) {
			t.Errorf("motion round trip:\n got %s\nwant %s", a, b)
		}
	}
	if got.HeadingDeg == nil || *got.HeadingDeg != 12 || got.TWADeg == nil || *got.TWADeg != -130 ||
		got.STWKts == nil || *got.STWKts != 8.2 || got.RPMMean == nil || *got.RPMMean != 1795 {
		t.Errorf("context round trip: %+v", got)
	}
}

func TestMissingSnapColumnsReadBackNull(t *testing.T) {
	store := newTestPassageStore(t)
	if err := store.AppendTrackPoint(recordedTrackPoint{At: passageEpoch, Lat: -20, Lon: 149, RollPeakDeg: pfloat(3)}); err != nil {
		t.Fatal(err)
	}
	pts, _ := store.TrackPointsSince(passageEpoch)
	got := pts[0]
	if got.motionFigures != (motionFigures{}) || got.HeadingDeg != nil || got.TWADeg != nil || got.STWKts != nil || got.RPMMean != nil {
		t.Errorf("unrecorded columns must be null: %+v", got)
	}
}

func TestTrackPointJSONCarriesTheSnapFiguresByName(t *testing.T) {
	p := recordedTrackPoint{At: passageEpoch, motionFigures: allMotionFigures(), trackContext: trackContext{HeadingDeg: pfloat(1), TWADeg: pfloat(2), STWKts: pfloat(3), RPMMean: pfloat(4)}}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"roll_rms_deg", "roll_rate_peak_dps", "roll_rate_rms_dps", "roll_accel_peak_dps2", "roll_jerk_p95_dps3",
		"pitch_rms_deg", "pitch_accel_peak_dps2", "pitch_jerk_p95_dps3", "heave_sd_m",
		"vert_accel_p95_ms2", "vert_accel_peak_ms2", "motion_rate_hz", "heave_rate_hz",
		"heading_deg", "twa_deg", "stw_kts", "rpm_mean",
	} {
		if _, ok := m[key]; !ok {
			t.Errorf("track point JSON lacks %q", key)
		}
	}
	// Absent figures are an explicit null, not a missing key, as roll_peak_deg is.
	raw, _ = json.Marshal(recordedTrackPoint{At: passageEpoch})
	if !strings.Contains(string(raw), `"roll_jerk_p95_dps3":null`) {
		t.Errorf("an absent figure must be null in JSON: %s", raw)
	}
}

func contextSnapshot(t *testing.T, now time.Time, seenAge time.Duration, deltas ...signalKValue) *signalKSnapshot {
	t.Helper()
	snap := newSignalKSnapshot()
	snap.setSelfContext("vessels.self")
	snap.applyDelta(signalKDelta{Context: "vessels.self", Updates: []signalKUpdate{{Values: deltas}}}, now.Add(-seenAge))
	return snap
}

func TestBuildTrackSampleReadsHeadingWindAngleAndSpeedThroughWater(t *testing.T) {
	now := time.Now()
	snap := contextSnapshot(t, now, time.Second,
		signalKValue{Path: "navigation.headingTrue", Value: -0.5}, // a negative angle wraps to 0-360
		signalKValue{Path: "environment.wind.angleTrueWater", Value: -0.8},
		signalKValue{Path: "navigation.speedThroughWater", Value: 5.0},
	)
	s := buildTrackSample(snap, liveState(), "", nil, now)
	if s.HeadingDeg == nil || !approx(*s.HeadingDeg, math.Mod(-0.5*180/math.Pi+360, 360), 1e-9) {
		t.Errorf("heading %v", s.HeadingDeg)
	}
	if s.TWADeg == nil || !approx(*s.TWADeg, -0.8*180/math.Pi, 1e-9) {
		t.Errorf("twa %v, want signed degrees", s.TWADeg)
	}
	if s.STWKts == nil || !approx(*s.STWKts, 5.0*metersPerSecondToKnots, 1e-9) {
		t.Errorf("stw %v", s.STWKts)
	}
}

func TestBuildTrackSampleStaleContextReadingsAreAbsent(t *testing.T) {
	now := time.Now()
	snap := contextSnapshot(t, now, time.Minute,
		signalKValue{Path: "navigation.headingTrue", Value: 1.0},
		signalKValue{Path: "environment.wind.angleTrueWater", Value: 1.0},
		signalKValue{Path: "navigation.speedThroughWater", Value: 5.0},
		signalKValue{Path: "propulsion.0.revolutions", Value: 30.0},
	)
	s := buildTrackSample(snap, liveState(), "", nil, now)
	if s.HeadingDeg != nil || s.TWADeg != nil || s.STWKts != nil || s.RPMMean != nil {
		t.Errorf("stale readings must be absent: %v %v %v %v", s.HeadingDeg, s.TWADeg, s.STWKts, s.RPMMean)
	}
}

func TestTrackMeanRPMAveragesRunningEnginesWhateverTheirIds(t *testing.T) {
	now := time.Now()
	hz := map[string]float64{
		"propulsion.0.revolutions":         30, // 1800 rpm
		"propulsion.1.revolutions":         31, // 1860 rpm
		"propulsion.generator.revolutions": 0,  // not turning
	}
	read := func(path string) alarmSample {
		if strings.Contains(path, "generator") {
			return alarmSample{EngineOff: true}
		}
		return alarmSample{Present: true, Value: hz[path], LastSeen: now.Add(-time.Second)}
	}
	paths := []string{"propulsion.0.revolutions", "propulsion.1.revolutions", "propulsion.generator.revolutions"}
	got := trackMeanRPM(read, paths, now)
	if got == nil || !approx(*got, 1830, 1e-9) {
		t.Errorf("mean rpm %v, want 1830 across the two engines that run", got)
	}
}

func TestTrackMeanRPMIsAbsentRatherThanZeroWhenNothingRuns(t *testing.T) {
	now := time.Now()
	off := func(string) alarmSample { return alarmSample{EngineOff: true} }
	if got := trackMeanRPM(off, []string{"propulsion.port.revolutions"}, now); got != nil {
		t.Errorf("all engines off: %v, want absent", *got)
	}
	idle := func(string) alarmSample { return alarmSample{Present: true, Value: 0, LastSeen: now} }
	if got := trackMeanRPM(idle, []string{"propulsion.port.revolutions"}, now); got != nil {
		t.Errorf("zero revolutions: %v, want absent", *got)
	}
	if got := trackMeanRPM(off, nil, now); got != nil {
		t.Errorf("no engines at all: %v", *got)
	}
}

// One engine on the bus with an old reading, and not switched off, means the
// mean would be over a subset of what is running: absent, not a wrong number.
func TestTrackMeanRPMWithOneStaleEngineIsAbsent(t *testing.T) {
	now := time.Now()
	read := func(path string) alarmSample {
		if strings.Contains(path, "starboard") {
			return alarmSample{Present: true, Value: 30, LastSeen: now.Add(-time.Minute)}
		}
		return alarmSample{Present: true, Value: 30, LastSeen: now.Add(-time.Second)}
	}
	if got := trackMeanRPM(read, []string{"propulsion.port.revolutions", "propulsion.starboard.revolutions"}, now); got != nil {
		t.Errorf("mean %v with a stale engine, want absent", *got)
	}
}

// An engine path that stopped reporting hours ago and is not switched off nulls
// the fuel total and the rpm mean: a dead engine feed is not hidden.
func TestAThreeHourStaleEnginePathNullsFuelAndRPM(t *testing.T) {
	now := time.Now()
	snap := newSignalKSnapshot()
	snap.setSelfContext("vessels.self")
	apply := func(age time.Duration, path string, value float64) {
		snap.applyDelta(signalKDelta{Context: "vessels.self", Updates: []signalKUpdate{{Values: []signalKValue{{Path: path, Value: value}}}}}, now.Add(-age))
	}
	apply(3*time.Hour, "propulsion.0.fuel.rate", 1.0/3600000)
	apply(3*time.Hour, "propulsion.0.revolutions", 30)
	for _, id := range []string{"port", "starboard"} {
		apply(time.Second, "propulsion."+id+".fuel.rate", 2.0/3600000)
		apply(time.Second, "propulsion."+id+".revolutions", 30)
	}
	s := buildTrackSample(snap, liveState(), "", nil, now)
	if s.FuelLph != nil {
		t.Errorf("fuel %v, want absent with a stale engine path", *s.FuelLph)
	}
	if s.RPMMean != nil {
		t.Errorf("rpm %v, want absent with a stale engine path", *s.RPMMean)
	}
}

// Only propulsion.<id>.revolutions is an engine's speed. A drive or shaft
// revolutions path deeper in the branch is not another engine.
func TestRPMMeanIgnoresRevolutionsDeeperThanTheEngine(t *testing.T) {
	now := time.Now()
	snap := newSignalKSnapshot()
	snap.setSelfContext("vessels.self")
	for path, value := range map[string]float64{
		"propulsion.port.revolutions":       30,
		"propulsion.port.drive.revolutions": 60,
	} {
		snap.applyDelta(signalKDelta{Context: "vessels.self", Updates: []signalKUpdate{{Values: []signalKValue{{Path: path, Value: value}}}}}, now.Add(-time.Second))
	}
	s := buildTrackSample(snap, liveState(), "", nil, now)
	if s.RPMMean == nil || math.Abs(*s.RPMMean-1800) > 0.5 {
		t.Errorf("rpm mean %v, want 1800 from the engine path alone", s.RPMMean)
	}
}

// With a fresh attitude in the snapshot and none on the motion stream, the
// recorder says so once, and again only after samples have returned and gone.
func TestRecorderWarnsOncePerEpisodeWhenTheMotionStreamDeliversNothing(t *testing.T) {
	buf := captureMotionLog(t)
	rec, _ := newTestRecorder(t)
	clock := &testClock{t: motionT0}
	rec.motion = newMotionAccumulatorClock(clock.now)
	warnings := func() int {
		return strings.Count(buf.String(), "motion stream delivering no attitude while SignalK has it")
	}
	tick := func(i int, withSnapshotAttitude bool) {
		clock.advance(10 * time.Minute)
		s := fix(passageEpoch.Add(time.Duration(i)*10*time.Minute), -20, 149)
		if withSnapshotAttitude {
			s.RollDeg, s.PitchDeg = pfloat(1), pfloat(1)
		}
		rec.Observe(s)
	}

	tick(0, true)
	tick(1, true)
	if warnings() != 1 {
		t.Fatalf("want one warning for the episode, got %d: %s", warnings(), buf)
	}
	tick(2, false) // the snapshot has no attitude either: nothing to say
	feedWindow(rec.motion, 0, 5)
	tick(3, true) // samples are back: episode over
	tick(4, true) // gone again
	if warnings() != 2 {
		t.Errorf("a new episode warns again, got %d", warnings())
	}
}

// Under way, points are kept every few seconds, so no single window spans a
// minute. A stream that delivers nothing for 70 s is still reported, and not
// before 60 s have passed.
func TestRecorderWarnsUnderWayWhenAttitudeIsSilentForAMinute(t *testing.T) {
	buf := captureMotionLog(t)
	rec, _ := newTestRecorder(t)
	clock := &testClock{t: motionT0}
	rec.motion = newMotionAccumulatorClock(clock.now)
	warnings := func() int {
		return strings.Count(buf.String(), "motion stream delivering no attitude while SignalK has it")
	}
	for i := 0; i < 9; i++ { // 8 s windows, 72 s in all
		clock.advance(8 * time.Second)
		s := fix(passageEpoch.Add(time.Duration(i)*8*time.Second), -20+0.001*float64(i), 149)
		s.RollDeg, s.PitchDeg = pfloat(1), pfloat(1)
		rec.Observe(s)
		if i == 6 && warnings() != 0 { // 56 s after start
			t.Fatalf("warned at 56 s: %s", buf)
		}
	}
	if warnings() != 1 {
		t.Errorf("want one warning after 70 s of silence under way, got %d: %s", warnings(), buf)
	}
}

// A point kept seconds after startup has not given the stream time to deliver.
func TestRecorderDoesNotWarnBeforeTheWindowHasSpannedAMinute(t *testing.T) {
	buf := captureMotionLog(t)
	rec, _ := newTestRecorder(t)
	clock := &testClock{t: motionT0}
	rec.motion = newMotionAccumulatorClock(clock.now)
	clock.advance(30 * time.Second)
	s := fix(passageEpoch, -20, 149)
	s.RollDeg, s.PitchDeg = pfloat(1), pfloat(1)
	rec.Observe(s)
	if strings.Contains(buf.String(), "motion stream delivering no") {
		t.Errorf("warned 30 s after startup: %s", buf)
	}
}

func TestSchemaAddsSnapColumnsToAnOlderDatabase(t *testing.T) {
	store := newTestPassageStore(t)
	for _, c := range []string{
		"roll_rms_deg", "roll_rate_peak_dps", "roll_rate_rms_dps", "roll_accel_peak_dps2", "roll_jerk_p95_dps3",
		"pitch_rms_deg", "pitch_accel_peak_dps2", "pitch_jerk_p95_dps3", "heave_sd_m",
		"vert_accel_p95_ms2", "vert_accel_peak_ms2", "motion_rate_hz", "heave_rate_hz",
		"heading_deg", "twa_deg", "stw_kts", "rpm_mean",
	} {
		if _, err := store.db.Exec(`SELECT ` + c + ` FROM track_points LIMIT 1`); err != nil {
			t.Errorf("column %s: %v", c, err)
		}
	}
	// Opening the schema a second time must not fail on columns that exist.
	if err := createPassageSchema(store.db); err != nil {
		t.Errorf("second open: %v", err)
	}
}
