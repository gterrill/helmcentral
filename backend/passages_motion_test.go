package main

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func motionSample(at time.Time, roll, pitch, heave *float64) trackSample {
	s := fix(at, -20, 149)
	s.RollDeg, s.PitchDeg, s.HeaveM = roll, pitch, heave
	return s
}

// The peaks are the largest magnitude seen between one kept point and the
// next, not the value on the tick that happened to be kept.
func TestRecorderMotionPeaksAreLargestMagnitudeSinceLastKeptPoint(t *testing.T) {
	rec, store := newTestRecorder(t)
	rec.Observe(motionSample(passageEpoch, nil, nil, nil))
	rec.Observe(motionSample(passageEpoch.Add(5*time.Second), pfloat(-14), pfloat(3), pfloat(0.2)))
	rec.Observe(motionSample(passageEpoch.Add(10*time.Second), pfloat(6), pfloat(-5), pfloat(-0.8)))
	rec.Observe(motionSample(passageEpoch.Add(10*time.Minute), pfloat(2), pfloat(1), pfloat(0.1)))

	pts, _ := store.TrackPointsSince(passageEpoch)
	got := pts[len(pts)-1]
	if got.RollPeakDeg == nil || *got.RollPeakDeg != 14 {
		t.Errorf("roll peak %v, want 14", got.RollPeakDeg)
	}
	if got.PitchPeakDeg == nil || *got.PitchPeakDeg != 5 {
		t.Errorf("pitch peak %v, want 5", got.PitchPeakDeg)
	}
	if got.HeavePeakM == nil || *got.HeavePeakM != 0.8 {
		t.Errorf("heave peak %v, want 0.8", got.HeavePeakM)
	}
}

func TestRecorderMotionPeaksResetAfterEachWrite(t *testing.T) {
	rec, store := newTestRecorder(t)
	rec.Observe(motionSample(passageEpoch, pfloat(20), pfloat(8), pfloat(1)))
	rec.Observe(motionSample(passageEpoch.Add(10*time.Minute), pfloat(3), nil, nil))
	pts, _ := store.TrackPointsSince(passageEpoch)
	if len(pts) != 2 {
		t.Fatalf("want 2 points, got %d", len(pts))
	}
	if p := pts[0]; p.RollPeakDeg == nil || *p.RollPeakDeg != 20 {
		t.Errorf("first roll peak %v, want 20", p.RollPeakDeg)
	}
	second := pts[1]
	if second.RollPeakDeg == nil || *second.RollPeakDeg != 3 {
		t.Errorf("second roll peak %v, want 3 (the 20 belongs to the first point)", second.RollPeakDeg)
	}
	if second.PitchPeakDeg != nil || second.HeavePeakM != nil {
		t.Errorf("no reading since the last point must stay null, got pitch %v heave %v", second.PitchPeakDeg, second.HeavePeakM)
	}
}

func TestRecorderMotionAbsentIsNullNotZero(t *testing.T) {
	rec, store := newTestRecorder(t)
	rec.Observe(fix(passageEpoch, -20, 149))
	pts, _ := store.TrackPointsSince(passageEpoch)
	p := pts[0]
	if p.RollPeakDeg != nil || p.PitchPeakDeg != nil || p.HeavePeakM != nil {
		t.Errorf("no motion reading must stay null: %+v", p)
	}
}

// A reading that arrives on a tick whose fix is rejected still counts: the
// boat moved, whatever the GPS said.
func TestRecorderMotionCountsOnTicksWithABadFix(t *testing.T) {
	rec, store := newTestRecorder(t)
	rec.Observe(fix(passageEpoch, -20, 149))
	bad := motionSample(passageEpoch.Add(5*time.Second), pfloat(18), nil, nil)
	bad.FixOK = false
	rec.Observe(bad)
	rec.Observe(motionSample(passageEpoch.Add(10*time.Minute), pfloat(1), nil, nil))
	pts, _ := store.TrackPointsSince(passageEpoch)
	if got := pts[len(pts)-1].RollPeakDeg; got == nil || *got != 18 {
		t.Errorf("roll peak %v, want 18", got)
	}
}

func TestStatsMotionMaximaAndTheirTimes(t *testing.T) {
	pts := instrumentedRun()
	pts[10].RollPeakDeg = pfloat(9)
	pts[20].RollPeakDeg = pfloat(14)
	pts[30].RollPeakDeg = pfloat(11)
	pts[15].PitchPeakDeg = pfloat(6)
	pts[40].HeavePeakM = pfloat(0.8)
	s := computePassageStats(pts)

	if s.MaxRollDeg == nil || *s.MaxRollDeg != 14 || s.MaxRollAt == nil || *s.MaxRollAt != pts[20].At.Unix() {
		t.Errorf("roll %v at %v", s.MaxRollDeg, s.MaxRollAt)
	}
	if s.MaxPitchDeg == nil || *s.MaxPitchDeg != 6 || s.MaxPitchAt == nil || *s.MaxPitchAt != pts[15].At.Unix() {
		t.Errorf("pitch %v at %v", s.MaxPitchDeg, s.MaxPitchAt)
	}
	if s.MaxHeaveM == nil || *s.MaxHeaveM != 0.8 || s.MaxHeaveAt == nil || *s.MaxHeaveAt != pts[40].At.Unix() {
		t.Errorf("heave %v at %v", s.MaxHeaveM, s.MaxHeaveAt)
	}
}

// The first point's peak covers the time before the passage began (or before a
// trim), so it is not the passage's motion.
func TestStatsMotionIgnoresTheFirstPointsPeaks(t *testing.T) {
	pts := instrumentedRun()
	pts[0].RollPeakDeg = pfloat(30)
	pts[0].PitchPeakDeg = pfloat(20)
	pts[0].HeavePeakM = pfloat(2)
	pts[5].RollPeakDeg = pfloat(8)
	s := computePassageStats(pts)
	if s.MaxRollDeg == nil || *s.MaxRollDeg != 8 {
		t.Errorf("roll %v, want 8", s.MaxRollDeg)
	}
	if s.MaxPitchDeg != nil || s.MaxHeaveM != nil {
		t.Errorf("pitch %v heave %v, want none", s.MaxPitchDeg, s.MaxHeaveM)
	}
}

func TestStatsMotionAbsentWhenNoPointHasAReading(t *testing.T) {
	s := computePassageStats(instrumentedRun())
	if s.MaxRollDeg != nil || s.MaxRollAt != nil || s.MaxPitchDeg != nil || s.MaxPitchAt != nil || s.MaxHeaveM != nil || s.MaxHeaveAt != nil {
		t.Errorf("old passages have no motion: %+v", s)
	}
}

func TestStatsMaxSpeedAndGustCarryTheirTimes(t *testing.T) {
	pts := instrumentedRun()
	pts[7].SOGKts = pfloat(9.5)
	pts[12].GustKts = pfloat(30)
	s := computePassageStats(pts)
	if s.MaxSOGAt == nil || *s.MaxSOGAt != pts[7].At.Unix() {
		t.Errorf("max sog at %v, want %d", s.MaxSOGAt, pts[7].At.Unix())
	}
	if s.MaxGustAt == nil || *s.MaxGustAt != pts[12].At.Unix() {
		t.Errorf("max gust at %v, want %d", s.MaxGustAt, pts[12].At.Unix())
	}
}

func TestStatsMaximaTimesAbsentWithTheirFigure(t *testing.T) {
	pts := instrumentedRun()
	for i := range pts {
		pts[i].GustKts, pts[i].SOGKts = nil, nil
	}
	s := computePassageStats(pts)
	if s.MaxGustAt != nil || s.MaxSOGAt != nil {
		t.Errorf("times without a figure: %v %v", s.MaxGustAt, s.MaxSOGAt)
	}
}

func TestMotionPeaksRoundTripThroughTheStore(t *testing.T) {
	store := newTestPassageStore(t)
	p := recordedTrackPoint{At: passageEpoch, Lat: -20, Lon: 149, RollPeakDeg: pfloat(12.5), HeavePeakM: pfloat(0.4)}
	if err := store.AppendTrackPoint(p); err != nil {
		t.Fatal(err)
	}
	pts, _ := store.TrackPointsSince(passageEpoch)
	got := pts[0]
	if got.RollPeakDeg == nil || *got.RollPeakDeg != 12.5 || got.HeavePeakM == nil || *got.HeavePeakM != 0.4 || got.PitchPeakDeg != nil {
		t.Errorf("round trip: %+v", got)
	}
}

// A database written before motion was recorded keeps every row and gains
// null columns.
func TestSchemaAddsMotionColumnsAndKeepsOldRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE track_points (
		id INTEGER PRIMARY KEY AUTOINCREMENT, t_ms INTEGER NOT NULL, segment INTEGER NOT NULL,
		lat REAL NOT NULL, lon REAL NOT NULL, sog_kts REAL, cog_deg REAL, tws_kts REAL, gust_kts REAL,
		soc_pct REAL, fuel_lph REAL, water_l REAL, gen_state TEXT, anchor_set_ms INTEGER,
		source TEXT NOT NULL DEFAULT 'recorded')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO track_points (t_ms, segment, lat, lon, sog_kts) VALUES (?,?,?,?,?)`, passageEpoch.UnixMilli(), 0, -20.0, 149.0, 5.0); err != nil {
		t.Fatal(err)
	}
	if err := createPassageSchema(db); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	pts, err := trackPointsIn(db, passageEpoch.Add(-time.Hour), passageEpoch.Add(time.Hour), "")
	if err != nil || len(pts) != 1 {
		t.Fatalf("rows after upgrade: %v %d", err, len(pts))
	}
	if pts[0].SOGKts == nil || *pts[0].SOGKts != 5 || pts[0].RollPeakDeg != nil {
		t.Errorf("old row changed: %+v", pts[0])
	}
}

func attitudeSnapshot(t *testing.T, now time.Time, seenAge time.Duration) *signalKSnapshot {
	t.Helper()
	snap := newSignalKSnapshot()
	snap.setSelfContext("vessels.self")
	seen := now.Add(-seenAge)
	snap.applyDelta(signalKDelta{Context: "vessels.self", Updates: []signalKUpdate{{Values: []signalKValue{
		{Path: "navigation.attitude", Value: map[string]any{"roll": 0.2, "pitch": -0.1, "yaw": 1.0}},
		{Path: "navigation.attitude.heave", Value: -0.03},
	}}}}, seen)
	return snap
}

func TestBuildTrackSampleReadsAttitudeInDegrees(t *testing.T) {
	now := time.Now()
	s := buildTrackSample(attitudeSnapshot(t, now, time.Second), liveState(), "", nil, now)
	if s.RollDeg == nil || !approx(*s.RollDeg, 0.2*180/3.141592653589793, 1e-9) {
		t.Errorf("roll %v", s.RollDeg)
	}
	if s.PitchDeg == nil || !approx(*s.PitchDeg, -0.1*180/3.141592653589793, 1e-9) {
		t.Errorf("pitch %v", s.PitchDeg)
	}
	if s.HeaveM == nil || *s.HeaveM != -0.03 {
		t.Errorf("heave %v", s.HeaveM)
	}
}

func TestBuildTrackSampleStaleAttitudeIsAbsent(t *testing.T) {
	now := time.Now()
	s := buildTrackSample(attitudeSnapshot(t, now, time.Minute), liveState(), "", nil, now)
	if s.RollDeg != nil || s.PitchDeg != nil || s.HeaveM != nil {
		t.Errorf("stale attitude must be absent: %v %v %v", s.RollDeg, s.PitchDeg, s.HeaveM)
	}
}

// Figures stored before the times of the maxima were kept are worked out again
// from the points, so old passages gain the buttons too.
func TestStoredFiguresInTheOldShapeAreWorkedOutAgain(t *testing.T) {
	store := newTestPassageStore(t)
	pts := instrumentedRun()
	appendAll(t, store, pts)
	rec, err := store.InsertPassage(passageRecord{Start: pts[0].At, End: pts[len(pts)-1].At})
	if err != nil {
		t.Fatal(err)
	}
	old := `{"stats":{"distance_nm":6,"max_gust_kts":18},"start_lon":149,"start_lat":-20,"timezone":"Australia/Brisbane"}`
	if _, err := store.db.Exec(`UPDATE passages SET stats_json = ? WHERE id = ?`, old, rec.ID); err != nil {
		t.Fatal(err)
	}
	f, err := store.PassageFiguresFor(rec)
	if err != nil {
		t.Fatal(err)
	}
	if f.Stats.MaxGustAt == nil {
		t.Errorf("old-shape figures must be recomputed, got %+v", f.Stats)
	}
}
