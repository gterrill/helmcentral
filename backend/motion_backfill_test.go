package main

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	ihttp "github.com/influxdata/influxdb-client-go/v2/api/http"
)

// fakeMotionInflux answers the backfill's queries from a table of samples by
// path, whatever the range asked for, so the code under test must do its own
// windowing.
type fakeMotionInflux struct {
	data  map[string][]normalMinuteRow
	calls []string
	err   error
	// failFirst makes the first n queries fail and the rest answer.
	failFirst int
}

func (f *fakeMotionInflux) install(t *testing.T) {
	t.Helper()
	old := motionBackfillQuery
	t.Cleanup(func() { motionBackfillQuery = old })
	motionBackfillQuery = func(_ context.Context, _ normalInflux, flux string) ([]normalMinuteRow, error) {
		// Every query is for measurements named in the filter or matched by the
		// revolutions pattern, so the fake answers by the same two tests.
		f.calls = append(f.calls, flux)
		if f.err != nil {
			return nil, f.err
		}
		if len(f.calls) <= f.failFirst {
			return nil, errors.New("timeout")
		}
		var out []normalMinuteRow
		for path, s := range f.data {
			if strings.Contains(flux, `"`+path+`"`) || (strings.Contains(flux, "=~") && strings.HasPrefix(path, "propulsion.")) {
				out = append(out, s...)
			}
		}
		return out, nil
	}
}

var testInflux = normalInflux{mainBucket: "SignalK_Data"}

// series lays value(i) at one sample a second from start, for n samples.
func series(path, source string, start time.Time, n int, value func(i int) float64) []normalMinuteRow {
	out := make([]normalMinuteRow, n)
	for i := range out {
		out[i] = normalMinuteRow{Path: path, Source: source, At: start.Add(time.Duration(i) * time.Second), Value: value(i)}
	}
	return out
}

func addRecorded(t *testing.T, s *passageStore, at time.Time, mut func(p *recordedTrackPoint)) {
	t.Helper()
	p := recordedTrackPoint{At: at, Lat: -25, Lon: 153, Source: trackSourceRecorded}
	if mut != nil {
		mut(&p)
	}
	if err := s.AppendTrackPoint(p); err != nil {
		t.Fatalf("AppendTrackPoint: %v", err)
	}
}

func pointAt(t *testing.T, s *passageStore, at time.Time) recordedTrackPoint {
	t.Helper()
	pts, err := s.TrackPointsBetween(at, at)
	if err != nil || len(pts) != 1 {
		t.Fatalf("point at %v: %v, %d points", at, err, len(pts))
	}
	return pts[0]
}

// A long run in the past. It is the moment live motion capture began in every
// test store, so points recorded before it are the backfill's job.
var backfillNow = passageEpoch.Add(30 * 24 * time.Hour)

// newBackfillStore is a passage store whose live motion capture began at
// backfillNow.
func newBackfillStore(t *testing.T) *passageStore {
	t.Helper()
	s := newTestPassageStore(t)
	if _, err := s.EnsureMotionCaptureStart(backfillNow); err != nil {
		t.Fatal(err)
	}
	return s
}

func wobble(i int) float64 { return rad(float64(i % 4)) }
func zero(int) float64     { return 0 }

// attitude is a gentle roll and pitch, one a second, for the minute ending at at.
func attitude(source string, at time.Time) map[string][]normalMinuteRow {
	start := at.Add(-59 * time.Second)
	return map[string][]normalMinuteRow{
		"navigation.attitude.roll":  series("navigation.attitude.roll", source, start, 60, wobble),
		"navigation.attitude.pitch": series("navigation.attitude.pitch", source, start, 60, zero),
	}
}

func mergeData(parts ...map[string][]normalMinuteRow) map[string][]normalMinuteRow {
	out := map[string][]normalMinuteRow{}
	for _, p := range parts {
		for k, v := range p {
			out[k] = append(out[k], v...)
		}
	}
	return out
}

func TestMotionCaptureStartIsStoredOnceAndNeverChanges(t *testing.T) {
	s := newTestPassageStore(t)
	if _, ok, err := s.MotionCaptureStart(); err != nil || ok {
		t.Fatalf("before the recorder has started: %v, %v", ok, err)
	}
	first, err := s.EnsureMotionCaptureStart(backfillNow)
	if err != nil || !first.Equal(backfillNow) {
		t.Fatalf("first start: %v, %v", first, err)
	}
	second, err := s.EnsureMotionCaptureStart(backfillNow.Add(48 * time.Hour))
	if err != nil || !second.Equal(backfillNow) {
		t.Fatalf("a later start moved the cutoff to %v, %v", second, err)
	}
	got, ok, err := s.MotionCaptureStart()
	if err != nil || !ok || !got.Equal(backfillNow) {
		t.Fatalf("stored cutoff %v, %v, %v", got, ok, err)
	}
}

func TestMotionBackfillFluxIsBoundedAndSafe(t *testing.T) {
	start := time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)
	stop := start.Add(2 * time.Hour)
	q, err := buildMotionBackfillFlux("SignalK_Data", "value", []string{"navigation.attitude.roll", "navigation.attitude.pitch"}, "", start, stop)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`from(bucket: "SignalK_Data")`,
		`range(start: time(v: "2026-10-20T01:00:00Z"), stop: time(v: "2026-10-20T03:00:00Z"))`,
		`r._measurement == "navigation.attitude.roll" or r._measurement == "navigation.attitude.pitch"`,
		`r._field == "value"`,
		`"source"`,
	} {
		if !strings.Contains(q, want) {
			t.Errorf("flux missing %q:\n%s", want, q)
		}
	}
	if strings.Contains(q, "aggregateWindow") {
		t.Errorf("raw readings must not be aggregated:\n%s", q)
	}
	if _, err := buildMotionBackfillFlux("b", "value", []string{"x${r._value}"}, "", start, stop); err == nil {
		t.Error("an interpolation in the measurement must be refused")
	}
	if _, err := buildMotionBackfillFlux("b${", "value", []string{"x"}, "", start, stop); err == nil {
		t.Error("an interpolation in the bucket must be refused")
	}
	rev, err := buildMotionBackfillFlux("SignalK_Data", "value", []string{"navigation.headingTrue"}, motionBackfillRevolutionsPattern, start, stop)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rev, `or r._measurement =~ /^propulsion\..*\.revolutions$/`) || !strings.Contains(rev, "2026-10-20T03:00:00Z") {
		t.Errorf("revolutions flux not an anchored, bounded regex:\n%s", rev)
	}
}

func TestMotionBackfillChoosesSourceWithMostPoints(t *testing.T) {
	a := series("p", "gw", passageEpoch, 50, func(int) float64 { return 1 })
	b := series("p", "other", passageEpoch, 120, func(int) float64 { return 2 })
	got := bestSource(append(a, b...))
	if len(got) != 120 || got[0].Source != "other" {
		t.Fatalf("chose %d points from %q, want the 120 from other", len(got), got[0].Source)
	}
	tie := bestSource(append(series("p", "b", passageEpoch, 5, zero), series("p", "a", passageEpoch, 5, zero)...))
	if len(tie) != 5 || tie[0].Source != "a" {
		t.Fatalf("a tie chose %q, want the name that sorts first", tie[0].Source)
	}
}

func TestMotionBackfillOnlyFillsPointsRecordedBeforeTheCutoff(t *testing.T) {
	s := newBackfillStore(t)
	before := backfillNow.Add(-time.Hour)
	atCutoff := backfillNow
	after := backfillNow.Add(time.Hour)
	imported := backfillNow.Add(-2 * time.Hour)
	for _, at := range []time.Time{before, atCutoff, after} {
		addRecorded(t, s, at, nil)
	}
	addRecorded(t, s, imported, func(p *recordedTrackPoint) { p.Source = trackSourceImported })
	f := &fakeMotionInflux{data: mergeData(attitude("gw", before), attitude("gw", atCutoff), attitude("gw", after))}
	f.install(t)
	sum, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow.Add(24*time.Hour))
	if err != nil || sum.PointsWithSway != 1 {
		t.Fatalf("run %+v, %v; want only the point before the cutoff filled", sum, err)
	}
	if p := pointAt(t, s, before); p.RollRMSDeg == nil {
		t.Error("the point before the cutoff was not filled")
	}
	for name, at := range map[string]time.Time{"at the cutoff": atCutoff, "after the cutoff": after, "imported": imported} {
		if p := pointAt(t, s, at); p.RollRMSDeg != nil || p.MotionRateHz != nil {
			t.Errorf("the point %s was filled", name)
		}
	}
}

func TestMotionBackfillWithNoCutoffYetFailsFast(t *testing.T) {
	s := newTestPassageStore(t)
	addRecorded(t, s, passageEpoch.Add(time.Hour), nil)
	f := &fakeMotionInflux{data: map[string][]normalMinuteRow{}}
	f.install(t)
	if _, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow); err == nil || len(f.calls) != 0 {
		t.Fatalf("err %v, %d queries; want an error and no query without a cutoff", err, len(f.calls))
	}
}

func TestMotionBackfillWindowIsTheSixtySecondsBeforeThePoint(t *testing.T) {
	s := newBackfillStore(t)
	// A point 10 s after the previous one: the window is still a whole minute,
	// not the 10 s gap.
	prev := passageEpoch.Add(time.Hour)
	at := prev.Add(10 * time.Second)
	addRecorded(t, s, prev, nil)
	addRecorded(t, s, at, nil)
	// Wild roll before the minute; gentle roll in it.
	early := series("navigation.attitude.roll", "gw", at.Add(-5*time.Minute), 200, func(i int) float64 { return rad(40 * float64(i%2*2-1)) })
	late := series("navigation.attitude.roll", "gw", at.Add(-59*time.Second), 60, func(i int) float64 { return rad(1 * float64(i%2*2-1)) })
	f := &fakeMotionInflux{data: map[string][]normalMinuteRow{
		"navigation.attitude.roll":  append(early, late...),
		"navigation.attitude.pitch": series("navigation.attitude.pitch", "gw", at.Add(-5*time.Minute), 300, zero),
	}}
	f.install(t)
	if _, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow); err != nil {
		t.Fatal(err)
	}
	p := pointAt(t, s, at)
	if p.RollRMSDeg == nil || *p.RollRMSDeg > 2 {
		t.Fatalf("roll RMS %v: samples from before the minute leaked into the window", p.RollRMSDeg)
	}
	if p.MotionRateHz == nil || !approx(*p.MotionRateHz, 1, 0.05) {
		t.Errorf("motion rate %v, want about 1 Hz over the whole minute", p.MotionRateHz)
	}
}

func TestMotionBackfillFiguresMatchTheAccumulatorOnA1HzSeries(t *testing.T) {
	s := newBackfillStore(t)
	at := passageEpoch.Add(time.Hour)
	addRecorded(t, s, at, nil)
	start := at.Add(-59 * time.Second)
	n := 60
	roll := series("navigation.attitude.roll", "gw", start, n, func(i int) float64 { return rad(6 * math.Sin(2*math.Pi*float64(i)/6)) })
	pitch := series("navigation.attitude.pitch", "gw", start, n, func(i int) float64 { return rad(2 * math.Sin(2*math.Pi*float64(i)/4)) })
	heave := series("navigation.attitude.heave", "gw", start, n, func(i int) float64 { return 0.3 * math.Sin(2*math.Pi*float64(i)/5) })
	f := &fakeMotionInflux{data: map[string][]normalMinuteRow{
		"navigation.attitude.roll": roll, "navigation.attitude.pitch": pitch, "navigation.attitude.heave": heave,
		"navigation.headingTrue":          {{Path: "navigation.headingTrue", Source: "gw", At: at.Add(-2 * time.Second), Value: rad(190)}},
		"environment.wind.angleTrueWater": {{Path: "environment.wind.angleTrueWater", Source: "gw", At: at.Add(-3 * time.Second), Value: rad(300)}},
		"navigation.speedThroughWater":    {{Path: "navigation.speedThroughWater", Source: "gw", At: at, Value: 5.144444}},
		"propulsion.0.revolutions":        {{Path: "propulsion.0.revolutions", Source: "gw", At: at.Add(-time.Second), Value: 20}},
		"propulsion.1.revolutions":        {{Path: "propulsion.1.revolutions", Source: "gw", At: at.Add(-time.Second), Value: 30}},
	}}
	f.install(t)

	sum, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow)
	if err != nil {
		t.Fatal(err)
	}
	if sum.PointsWithSway != 1 {
		t.Fatalf("%d points with sway, want 1", sum.PointsWithSway)
	}

	// The same samples straight through the accumulator on a sample-time clock.
	cur := at.Add(-time.Minute)
	acc := newMotionAccumulatorGap(func() time.Time { return cur }, motionBackfillMaxGap)
	acc.TakeWindow()
	for i := 0; i < n; i++ {
		cur = roll[i].At
		acc.AddAttitude(roll[i].At, roll[i].Value*180/math.Pi, pitch[i].Value*180/math.Pi)
		acc.AddHeave(heave[i].At, heave[i].Value)
	}
	cur = at
	want, _ := acc.TakeWindow()

	got := pointAt(t, s, at)
	eq := func(name string, g, w *float64) {
		t.Helper()
		if (g == nil) != (w == nil) || (g != nil && !approx(*g, *w, 1e-9)) {
			t.Errorf("%s = %v, accumulator says %v", name, g, w)
		}
	}
	eq("roll rms", got.RollRMSDeg, want.RollRMSDeg)
	eq("pitch rms", got.PitchRMSDeg, want.PitchRMSDeg)
	eq("heave sd", got.HeaveSDM, want.HeaveSDM)
	eq("motion rate", got.MotionRateHz, want.MotionRateHz)
	eq("heave rate", got.HeaveRateHz, want.HeaveRateHz)

	if got.RollRMSDeg == nil || !approx(*got.RollRMSDeg, 6/math.Sqrt2, 0.6) {
		t.Errorf("roll RMS %v, want about %.2f", got.RollRMSDeg, 6/math.Sqrt2)
	}
	if got.MotionRateHz == nil || !approx(*got.MotionRateHz, 1, 0.05) || got.HeaveRateHz == nil || !approx(*got.HeaveRateHz, 1, 0.05) {
		t.Errorf("rates %v %v, want about 1 Hz", got.MotionRateHz, got.HeaveRateHz)
	}
	for name, v := range map[string]*float64{
		"roll rate peak": got.RollRatePeakDps, "roll rate rms": got.RollRateRMSDps, "roll accel": got.RollAccelPeakDps2,
		"roll jerk": got.RollJerkP95Dps3, "pitch accel": got.PitchAccelPeakDps2, "pitch jerk": got.PitchJerkP95Dps3,
		"vert p95": got.VertAccelP95Ms2, "vert peak": got.VertAccelPeakMs2,
	} {
		if v != nil {
			t.Errorf("%s = %v at 1 Hz, want null", name, *v)
		}
	}
	if got.HeadingDeg == nil || !approx(*got.HeadingDeg, 190, 1e-6) {
		t.Errorf("heading %v", got.HeadingDeg)
	}
	if got.TWADeg == nil || !approx(*got.TWADeg, -60, 1e-6) {
		t.Errorf("twa %v, want -60", got.TWADeg)
	}
	if got.STWKts == nil || !approx(*got.STWKts, 10, 1e-3) {
		t.Errorf("stw %v", got.STWKts)
	}
	if got.RPMMean == nil || !approx(*got.RPMMean, 1500, 1e-6) {
		t.Errorf("rpm mean %v, want 1500", got.RPMMean)
	}
}

func TestMotionBackfillReadingsFartherThanTenSecondsAreAbsent(t *testing.T) {
	s := newBackfillStore(t)
	at := passageEpoch.Add(time.Hour)
	addRecorded(t, s, at, nil)
	f := &fakeMotionInflux{data: mergeData(attitude("gw", at), map[string][]normalMinuteRow{
		"navigation.headingTrue": {{Path: "navigation.headingTrue", Source: "gw", At: at.Add(-11 * time.Second), Value: rad(90)}},
		// One engine running now, one that last reported long ago: the mean is
		// absent, as live when a path that is not off has gone stale.
		"propulsion.0.revolutions": {{Path: "propulsion.0.revolutions", Source: "gw", At: at, Value: 20}},
		"propulsion.1.revolutions": {{Path: "propulsion.1.revolutions", Source: "gw", At: at.Add(-5 * time.Minute), Value: 20}},
	})}
	f.install(t)
	if _, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow); err != nil {
		t.Fatal(err)
	}
	p := pointAt(t, s, at)
	if p.HeadingDeg != nil {
		t.Errorf("heading 11 s away was kept: %v", *p.HeadingDeg)
	}
	if p.RPMMean != nil {
		t.Errorf("rpm mean %v with one engine's feed stale, want null", *p.RPMMean)
	}
}

func TestMotionBackfillNeverOverwritesAFilledValue(t *testing.T) {
	s := newBackfillStore(t)
	at := passageEpoch.Add(time.Hour)
	addRecorded(t, s, at, func(p *recordedTrackPoint) { p.RollRMSDeg = pfloat(5) })
	p := recordedTrackPoint{motionFigures: motionFigures{RollRMSDeg: pfloat(1), PitchRMSDeg: pfloat(2)}}
	var id int64
	if err := s.db.QueryRow(`SELECT id FROM track_points WHERE t_ms = ?`, at.UnixMilli()).Scan(&id); err != nil {
		t.Fatal(err)
	}
	stmt, err := s.db.Prepare(fillMotionSQL)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if err := fillMotionColumns(stmt, id, p, backfillNow); err != nil {
		t.Fatal(err)
	}
	got := pointAt(t, s, at)
	if got.RollRMSDeg == nil || *got.RollRMSDeg != 5 {
		t.Errorf("roll rms %v, want the existing 5", got.RollRMSDeg)
	}
	if got.PitchRMSDeg == nil || *got.PitchRMSDeg != 2 {
		t.Errorf("pitch rms %v, want the empty column filled with 2", got.PitchRMSDeg)
	}
}

// A point holding a live-recorder column is the start of live capture itself,
// so it is not a target and its figures are never touched.
func TestMotionBackfillLeavesAPointThatHasLiveContextFigures(t *testing.T) {
	s := newBackfillStore(t)
	at := passageEpoch.Add(time.Hour)
	addRecorded(t, s, at, func(p *recordedTrackPoint) {
		p.HeadingDeg = pfloat(42)
		p.RPMMean = pfloat(1800)
	})
	f := &fakeMotionInflux{data: mergeData(attitude("gw", at), map[string][]normalMinuteRow{
		"navigation.headingTrue": {{Path: "navigation.headingTrue", Source: "gw", At: at, Value: rad(190)}},
	})}
	f.install(t)
	sum, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow)
	if err != nil || sum.PointsWithSway != 0 || len(f.calls) != 0 {
		t.Fatalf("run: %+v, %v, %d queries; want the live point left alone", sum, err, len(f.calls))
	}
	p := pointAt(t, s, at)
	if p.RollRMSDeg != nil || p.HeadingDeg == nil || *p.HeadingDeg != 42 || p.RPMMean == nil || *p.RPMMean != 1800 {
		t.Errorf("live point changed: roll rms %v heading %v rpm %v", p.RollRMSDeg, p.HeadingDeg, p.RPMMean)
	}
}

func TestMotionBackfillAtMostTwoQueriesPerSpan(t *testing.T) {
	s := newBackfillStore(t)
	// Twenty points five minutes apart: one passage-sized span.
	for i := 0; i < 20; i++ {
		addRecorded(t, s, passageEpoch.Add(time.Duration(i)*5*time.Minute), nil)
	}
	f := &fakeMotionInflux{data: map[string][]normalMinuteRow{
		"navigation.attitude.heave": series("navigation.attitude.heave", "gw", passageEpoch, 600, func(i int) float64 { return float64(i % 2) }),
	}}
	f.install(t)
	sum, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Spans != 1 || len(f.calls) != 2 {
		t.Fatalf("%d spans, %d queries; want 1 span and 2 queries (attitude and heave, then heading, wind angle, speed and revolutions)", sum.Spans, len(f.calls))
	}
}

func TestMotionBackfillDoesNotRetryASpanInfluxHadNothingFor(t *testing.T) {
	s := newBackfillStore(t)
	addRecorded(t, s, passageEpoch.Add(time.Hour), nil)
	f := &fakeMotionInflux{data: map[string][]normalMinuteRow{}}
	f.install(t)
	first, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow)
	if err != nil {
		t.Fatal(err)
	}
	if first.SpansNoData != 1 || first.PointsWithSway != 0 || first.PointsContextOnly != 0 {
		t.Fatalf("first run: %+v, want one span with no data", first)
	}
	// The context query runs whether or not the span had attitude.
	if len(f.calls) != 2 {
		t.Fatalf("%d queries for a span with no attitude, want 2", len(f.calls))
	}
	f.calls = nil
	second, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 0 || second.Spans != 0 {
		t.Fatalf("second run queried %d times over %d spans, want none", len(f.calls), second.Spans)
	}
}

func TestMotionBackfillStoresAFinishedMarkerAndSkipsLaterStarts(t *testing.T) {
	s := newBackfillStore(t)
	at := passageEpoch.Add(time.Hour)
	addRecorded(t, s, at, nil)
	f := &fakeMotionInflux{data: attitude("gw", at)}
	f.install(t)
	first, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow)
	if err != nil || !first.Complete {
		t.Fatalf("first run: %+v, %v; want it complete", first, err)
	}
	if done, err := s.MotionBackfillFinished(); err != nil || !done {
		t.Fatalf("finished marker %v, %v", done, err)
	}
	// A point recorded before the cutoff but only now present would be ignored:
	// the marker ends the backfill whatever the table holds.
	addRecorded(t, s, at.Add(time.Hour), nil)
	f.calls = nil
	second, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow)
	if err != nil || len(f.calls) != 0 || second.Skipped == "" {
		t.Fatalf("second run: %+v, %v, %d queries; want it skipped", second, err, len(f.calls))
	}
}

func TestMotionBackfillOnceLogsCompleteAndNeverOpensInfluxAgain(t *testing.T) {
	s := newBackfillStore(t)
	if err := s.MarkMotionBackfillFinished(backfillNow); err != nil {
		t.Fatal(err)
	}
	old := openNormalInflux
	t.Cleanup(func() { openNormalInflux = old })
	opened := false
	openNormalInflux = func(string) (normalInflux, error) { opened = true; return testInflux, nil }
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	sum := runMotionBackfillOnce(context.Background(), s, "settings.yaml", backfillNow)
	if opened || sum.Skipped == "" || strings.Count(buf.String(), "complete") != 1 {
		t.Fatalf("opened %v, summary %+v, log %q; want one 'complete' line and no InfluxDB", opened, sum, buf.String())
	}
}

func TestMotionBackfillRetriesASpanWhenInfluxFailed(t *testing.T) {
	s := newBackfillStore(t)
	at := passageEpoch.Add(time.Hour)
	addRecorded(t, s, at, nil)
	f := &fakeMotionInflux{err: errors.New("bad response")}
	f.install(t)
	sum, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow)
	if err != nil || sum.SpansFailed != 1 || sum.SpansNoData != 0 || sum.Complete {
		t.Fatalf("run: %+v, %v; want one failed span, reported as failed and not as no data, and not complete", sum, err)
	}
	if done, _ := s.MotionBackfillFinished(); done {
		t.Fatal("a failed span must not finish the backfill")
	}
	f.err, f.calls = nil, nil
	f.data = attitude("gw", at)
	sum, err = runMotionBackfill(context.Background(), s, testInflux, backfillNow)
	if err != nil || sum.PointsWithSway != 1 || !sum.Complete {
		t.Fatalf("second run: %+v, %v; want the span tried again and the backfill complete", sum, err)
	}
}

func TestMotionBackfillAFailedQueryDoesNotBlockTheNextSpan(t *testing.T) {
	s := newBackfillStore(t)
	first := passageEpoch.Add(time.Hour)
	second := first.Add(5 * time.Hour) // beyond the span gap, so its own span
	addRecorded(t, s, first, nil)
	addRecorded(t, s, second, nil)
	f := &fakeMotionInflux{failFirst: 1, data: map[string][]normalMinuteRow{
		"navigation.headingTrue": {{Path: "navigation.headingTrue", Source: "gw", At: second, Value: rad(90)}},
	}}
	f.install(t)
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	sum, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow)
	if err != nil || sum.SpansFailed != 1 || sum.PointsContextOnly != 1 {
		t.Fatalf("run: %+v, %v; want the first span failed and the second filled", sum, err)
	}
	if !strings.Contains(buf.String(), "timeout") {
		t.Errorf("the failed query was not logged: %q", buf.String())
	}
	if got := pointAt(t, s, second); got.HeadingDeg == nil {
		t.Error("the later span was not filled")
	}
	// The failed span was left unstamped, so it is a target again.
	targets, err := s.motionBackfillTargets(backfillNow)
	if err != nil || len(targets) != 1 || !targets[0].At.Equal(first) {
		t.Fatalf("targets after the run: %+v, %v; want only the failed span's point", targets, err)
	}
}

func TestMotionBackfillDeadHostEndsTheRunAtOnce(t *testing.T) {
	s := newBackfillStore(t)
	first := passageEpoch.Add(time.Hour)
	second := first.Add(5 * time.Hour)
	third := second.Add(5 * time.Hour)
	for _, at := range []time.Time{first, second, third} {
		addRecorded(t, s, at, nil)
	}
	for name, dead := range map[string]error{
		"refused": fmt.Errorf("query: %w", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}),
		"dns":     fmt.Errorf("query: %w", &net.DNSError{Err: "no such host", Name: "influx.invalid"}),
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeMotionInflux{err: dead}
			f.install(t)
			old := openNormalInflux
			t.Cleanup(func() { openNormalInflux = old })
			openNormalInflux = func(string) (normalInflux, error) { return testInflux, nil }
			var buf bytes.Buffer
			log.SetOutput(&buf)
			t.Cleanup(func() { log.SetOutput(os.Stderr) })

			sum := runMotionBackfillOnce(context.Background(), s, "settings.yaml", backfillNow)
			if len(f.calls) != 1 {
				t.Fatalf("%d queries after the host was found dead, want 1", len(f.calls))
			}
			if n := strings.Count(buf.String(), "motion backfill"); n != 1 {
				t.Fatalf("%d log lines, want 1:\n%s", n, buf.String())
			}
			if sum.SpansFailed != 0 || sum.Complete {
				t.Fatalf("summary %+v: a dead host is not a failed span and not a finished run", sum)
			}
			if targets, err := s.motionBackfillTargets(backfillNow); err != nil || len(targets) != 3 {
				t.Fatalf("%d targets left, %v; want all three left for the next start", len(targets), err)
			}
		})
	}
}

func TestMotionBackfillReadsContextForASpanWithNoAttitude(t *testing.T) {
	s := newBackfillStore(t)
	at := passageEpoch.Add(time.Hour)
	addRecorded(t, s, at, nil)
	f := &fakeMotionInflux{data: map[string][]normalMinuteRow{
		"navigation.headingTrue": {{Path: "navigation.headingTrue", Source: "gw", At: at, Value: rad(190)}},
	}}
	f.install(t)
	sum, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow)
	if err != nil || sum.PointsContextOnly != 1 || sum.PointsWithSway != 0 {
		t.Fatalf("run: %+v, %v; want the point filled with its heading only", sum, err)
	}
	if p := pointAt(t, s, at); p.HeadingDeg == nil || !approx(*p.HeadingDeg, 190, 1e-6) {
		t.Errorf("heading %v, want 190", p.HeadingDeg)
	}
}

func TestMotionBackfillSummarySeparatesSwayContextOnlyAndNothing(t *testing.T) {
	s := newBackfillStore(t)
	a := passageEpoch.Add(time.Hour)
	b := a.Add(10 * time.Minute)
	c := b.Add(10 * time.Minute)
	for _, at := range []time.Time{a, b, c} {
		addRecorded(t, s, at, nil)
	}
	f := &fakeMotionInflux{data: mergeData(attitude("gw", a), map[string][]normalMinuteRow{
		"navigation.headingTrue": {{Path: "navigation.headingTrue", Source: "gw", At: b, Value: rad(10)}},
	})}
	f.install(t)
	sum, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow)
	if err != nil || sum.PointsWithSway != 1 || sum.PointsContextOnly != 1 || sum.SpansNoData != 0 {
		t.Fatalf("run: %+v, %v; want 1 point with sway, 1 with context only, no empty span", sum, err)
	}
}

func TestMotionBackfillPairsRollAndPitchFromOneSource(t *testing.T) {
	s := newBackfillStore(t)
	at := passageEpoch.Add(time.Hour)
	addRecorded(t, s, at, nil)
	start := at.Add(-55 * time.Second)
	// Roll has the most readings from "a", pitch from "b", yet only "b" has both
	// together at the same times.
	f := &fakeMotionInflux{data: map[string][]normalMinuteRow{
		"navigation.attitude.roll": append(
			series("navigation.attitude.roll", "a", start, 50, wobble),
			series("navigation.attitude.roll", "b", start.Add(20*time.Second), 30, wobble)...),
		"navigation.attitude.pitch": append(
			series("navigation.attitude.pitch", "a", start, 10, zero),
			series("navigation.attitude.pitch", "b", start.Add(20*time.Second), 30, zero)...),
	}}
	f.install(t)
	if _, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow); err != nil {
		t.Fatal(err)
	}
	if p := pointAt(t, s, at); p.RollRMSDeg == nil || p.PitchRMSDeg == nil {
		t.Errorf("roll rms %v, pitch rms %v: roll and pitch landed on different sources", p.RollRMSDeg, p.PitchRMSDeg)
	}
}

func TestMotionBackfillChoosesTheSourceWithinEachPointsOwnWindow(t *testing.T) {
	s := newBackfillStore(t)
	a := passageEpoch.Add(time.Hour)
	b := a.Add(20 * time.Minute) // same span, a different window
	addRecorded(t, s, a, nil)
	addRecorded(t, s, b, nil)
	// Source "one" has the most readings over the span but swapped out before b;
	// "two" covers b's window alone. Chosen once for the span, b would go blank.
	f := &fakeMotionInflux{data: mergeData(
		map[string][]normalMinuteRow{
			"navigation.attitude.roll":  series("navigation.attitude.roll", "one", a.Add(-59*time.Second), 60, wobble),
			"navigation.attitude.pitch": series("navigation.attitude.pitch", "one", a.Add(-59*time.Second), 60, zero),
			"navigation.headingTrue":    {{Path: "navigation.headingTrue", Source: "one", At: a, Value: rad(10)}},
		},
		attitude("two", b),
		map[string][]normalMinuteRow{
			"navigation.headingTrue": {{Path: "navigation.headingTrue", Source: "two", At: b, Value: rad(20)}},
		},
		// Pad "one" so it has more readings than "two" over the whole span.
		map[string][]normalMinuteRow{
			"navigation.headingTrue": series("navigation.headingTrue", "one", a.Add(time.Minute), 30, func(int) float64 { return rad(10) }),
		},
	)}
	f.install(t)
	if _, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow); err != nil {
		t.Fatal(err)
	}
	for name, at := range map[string]time.Time{"first": a, "second": b} {
		if p := pointAt(t, s, at); p.RollRMSDeg == nil || p.PitchRMSDeg == nil {
			t.Errorf("%s point has no sway: its own window's source was not used", name)
		}
	}
	if p := pointAt(t, s, b); p.HeadingDeg == nil || !approx(*p.HeadingDeg, 20, 1e-6) {
		t.Errorf("second point heading %v, want 20 from the source in its window", p.HeadingDeg)
	}
}

func TestMotionBackfillReadsOnlyReadingsAtOrBeforeThePoint(t *testing.T) {
	s := newBackfillStore(t)
	at := passageEpoch.Add(time.Hour)
	addRecorded(t, s, at, nil)
	f := &fakeMotionInflux{data: map[string][]normalMinuteRow{
		"navigation.headingTrue":       {{Path: "navigation.headingTrue", Source: "gw", At: at.Add(3 * time.Second), Value: rad(90)}},
		"navigation.speedThroughWater": {{Path: "navigation.speedThroughWater", Source: "gw", At: at.Add(-3 * time.Second), Value: 5.144444}},
	}}
	f.install(t)
	if _, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow); err != nil {
		t.Fatal(err)
	}
	p := pointAt(t, s, at)
	if p.HeadingDeg != nil {
		t.Errorf("heading from 3 s after the point was kept: %v", *p.HeadingDeg)
	}
	if p.STWKts == nil {
		t.Error("speed from 3 s before the point was dropped")
	}
}

func TestMotionBackfillWindowDoesNotCountTimeBeforeItsStart(t *testing.T) {
	s := newBackfillStore(t)
	a := passageEpoch.Add(time.Hour)
	b := a.Add(time.Minute)
	addRecorded(t, s, a, nil)
	addRecorded(t, s, b, nil)
	// One reading just before a's end and one 2.5 s later inside b's window: b holds
	// a single reading, which begins a run and measures no rate. Bridged from the
	// reading before its start, it would count time that lies in a's window.
	row := func(path string, at time.Time, v float64) normalMinuteRow {
		return normalMinuteRow{Path: path, Source: "gw", At: at, Value: v}
	}
	f := &fakeMotionInflux{data: map[string][]normalMinuteRow{
		"navigation.attitude.roll":  {row("navigation.attitude.roll", a.Add(-500*time.Millisecond), 0), row("navigation.attitude.roll", a.Add(2*time.Second), rad(5))},
		"navigation.attitude.pitch": {row("navigation.attitude.pitch", a.Add(-500*time.Millisecond), 0), row("navigation.attitude.pitch", a.Add(2*time.Second), rad(5))},
	}}
	f.install(t)
	if _, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow); err != nil {
		t.Fatal(err)
	}
	if p := pointAt(t, s, b); p.MotionRateHz != nil || p.RollRMSDeg != nil {
		t.Errorf("rate %v, roll rms %v: the window used time before its start", p.MotionRateHz, p.RollRMSDeg)
	}
}

func TestMotionBackfillReadsTheSignalKWritersField(t *testing.T) {
	s := newBackfillStore(t)
	addRecorded(t, s, passageEpoch.Add(time.Hour), nil)
	f := &fakeMotionInflux{data: map[string][]normalMinuteRow{}}
	f.install(t)
	in := testInflux
	in.field = "depth_field_setting"
	if _, err := runMotionBackfill(context.Background(), s, in, backfillNow); err != nil {
		t.Fatal(err)
	}
	for _, q := range f.calls {
		if !strings.Contains(q, `r._field == "value"`) || strings.Contains(q, "depth_field_setting") {
			t.Errorf("query reads the depth setting's field:\n%s", q)
		}
	}
}

func TestMotionBackfillTargetsAreFoundThroughTheTimeIndex(t *testing.T) {
	s := newBackfillStore(t)
	plan, err := s.db.Query(`EXPLAIN QUERY PLAN `+motionBackfillTargetsSQL, backfillNow.UnixMilli(), trackSourceRecorded)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	var all strings.Builder
	for plan.Next() {
		var id, parent, unused int
		var detail string
		if err := plan.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		all.WriteString(detail + "\n")
	}
	if !strings.Contains(all.String(), "idx_track_points_t") {
		t.Errorf("targets are not read through the time index:\n%s", all.String())
	}
}

// cutoffRun runs the backfill on a store whose stored start is stored, with live
// (#536) points at liveAt carrying a heading, and returns which of the recorded
// points were filled with a roll figure.
func cutoffRun(t *testing.T, stored time.Time, liveAt []time.Time, recorded []time.Time) map[time.Time]bool {
	t.Helper()
	s := newTestPassageStore(t)
	if _, err := s.EnsureMotionCaptureStart(stored); err != nil {
		t.Fatal(err)
	}
	for _, at := range liveAt {
		addRecorded(t, s, at, func(p *recordedTrackPoint) { h := 10.0; p.HeadingDeg = &h })
	}
	data := map[string][]normalMinuteRow{}
	for _, at := range recorded {
		addRecorded(t, s, at, nil)
		data = mergeData(data, attitude("gw", at))
	}
	f := &fakeMotionInflux{data: data}
	f.install(t)
	if _, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	out := map[time.Time]bool{}
	for _, at := range recorded {
		out[at] = pointAt(t, s, at).RollRMSDeg != nil
	}
	return out
}

func TestMotionBackfillCutoffIsTheFirstLiveRecorderPoint(t *testing.T) {
	live := backfillNow.Add(-2 * time.Hour)
	before := backfillNow.Add(-3 * time.Hour)
	between := backfillNow.Add(-time.Hour)  // after the first live point, before the stored start
	startup := backfillNow.Add(time.Second) // after the stored start
	got := cutoffRun(t, backfillNow, []time.Time{live}, []time.Time{before, between, startup})
	if !got[before] || got[between] || got[startup] {
		t.Fatalf("filled %v; want only the point before the first live-recorder point", got)
	}
}

func TestMotionBackfillCutoffFromDataOverridesAWrongStoredClock(t *testing.T) {
	live := backfillNow.Add(-2 * time.Hour)
	before := backfillNow.Add(-3 * time.Hour)
	between := backfillNow.Add(-time.Hour)
	got := cutoffRun(t, backfillNow.Add(48*time.Hour), []time.Time{live}, []time.Time{before, between})
	if !got[before] || got[between] {
		t.Fatalf("filled %v; a stored start in the future must not outrank the live data", got)
	}
}

func TestMotionBackfillCutoffIgnoresPointsTheBackfillFilled(t *testing.T) {
	s := newBackfillStore(t)
	a := backfillNow.Add(-3 * time.Hour)
	b := backfillNow.Add(-2 * time.Hour)
	addRecorded(t, s, a, nil)
	addRecorded(t, s, b, nil)
	f := &fakeMotionInflux{data: mergeData(attitude("gw", a), attitude("gw", b))}
	f.install(t)
	// A first run that fails to finish leaves a's figures behind; they must not
	// pull the cutoff back to a.
	f.failFirst = 0
	if _, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow); err != nil {
		t.Fatal(err)
	}
	live, ok, err := s.firstLiveMotionPoint()
	if err != nil || ok {
		t.Fatalf("first live point %v, %v, %v; backfilled points are not live capture", live, ok, err)
	}
}

func TestMotionBackfillPersistentQueryFailureEndsTheRun(t *testing.T) {
	for name, bad := range map[string]error{
		"401":  &ihttp.Error{StatusCode: 401, Message: "unauthorized"},
		"403":  &ihttp.Error{StatusCode: 403, Message: "forbidden"},
		"404":  &ihttp.Error{StatusCode: 404, Message: "bucket not found"},
		"tls":  fmt.Errorf("query: %w", x509.UnknownAuthorityError{}),
		"wrap": fmt.Errorf("query: %w", &ihttp.Error{StatusCode: 401}),
	} {
		t.Run(name, func(t *testing.T) {
			s := newBackfillStore(t)
			first := passageEpoch.Add(time.Hour)
			for _, at := range []time.Time{first, first.Add(5 * time.Hour), first.Add(10 * time.Hour)} {
				addRecorded(t, s, at, nil)
			}
			f := &fakeMotionInflux{err: bad}
			f.install(t)
			old := openNormalInflux
			t.Cleanup(func() { openNormalInflux = old })
			openNormalInflux = func(string) (normalInflux, error) { return testInflux, nil }
			var buf bytes.Buffer
			log.SetOutput(&buf)
			t.Cleanup(func() { log.SetOutput(os.Stderr) })

			sum := runMotionBackfillOnce(context.Background(), s, "settings.yaml", backfillNow)
			if len(f.calls) != 1 || strings.Count(buf.String(), "motion backfill") != 1 {
				t.Fatalf("%d queries, log:\n%s\nwant one query and one line", len(f.calls), buf.String())
			}
			if sum.SpansFailed != 0 || sum.Complete {
				t.Fatalf("summary %+v", sum)
			}
			if done, err := s.MotionBackfillFinished(); err != nil || done {
				t.Fatalf("finished marker %v, %v; want none", done, err)
			}
			if targets, err := s.motionBackfillTargets(backfillNow); err != nil || len(targets) != 3 {
				t.Fatalf("%d targets left, %v; want 3", len(targets), err)
			}
		})
	}
}

func TestMotionBackfillOtherQueryErrorsOnlySkipTheSpan(t *testing.T) {
	s := newBackfillStore(t)
	first := passageEpoch.Add(time.Hour)
	second := first.Add(5 * time.Hour)
	addRecorded(t, s, first, nil)
	addRecorded(t, s, second, nil)
	f := &fakeMotionInflux{data: map[string][]normalMinuteRow{}, err: &ihttp.Error{StatusCode: 500}}
	f.install(t)
	sum, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow)
	if err != nil || sum.SpansFailed != 2 || sum.Complete {
		t.Fatalf("run %+v, %v; a 500 skips the span and goes on", sum, err)
	}
}

func TestMotionBackfillPairsRollAndPitchByTheMillisecond(t *testing.T) {
	base := passageEpoch.Add(time.Hour)
	roll := []normalMinuteRow{{Path: motionRollPath, Source: "gw", At: base, Value: 0.1}}
	pitch := []normalMinuteRow{{Path: motionPitchPath, Source: "gw", At: base.Add(400 * time.Microsecond), Value: 0.2}}
	r, p := pairedSource(roll, pitch)
	if len(r) != 1 {
		t.Fatalf("pairedSource found no pair in the same millisecond")
	}
	if ev := pairAttitude(r, p); len(ev) != 1 {
		t.Fatalf("pairAttitude paired %d events from one millisecond, want 1", len(ev))
	}
}

func TestMotionBackfillContextSourceIsChosenByFreshness(t *testing.T) {
	s := newBackfillStore(t)
	at := passageEpoch.Add(time.Hour)
	addRecorded(t, s, at, nil)
	f := &fakeMotionInflux{data: map[string][]normalMinuteRow{
		// A has many readings but went quiet 20 s before the point.
		"navigation.headingTrue": append(
			series("navigation.headingTrue", "a", at.Add(-59*time.Second), 40, func(int) float64 { return rad(100) }),
			normalMinuteRow{Path: "navigation.headingTrue", Source: "b", At: at.Add(-2 * time.Second), Value: rad(200)}),
	}}
	f.install(t)
	if _, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow); err != nil {
		t.Fatal(err)
	}
	if p := pointAt(t, s, at); p.HeadingDeg == nil || !approx(*p.HeadingDeg, 200, 1e-6) {
		t.Fatalf("heading %v, want 200 from the source that is fresh", p.HeadingDeg)
	}
}

func TestMotionBackfillStopsWhenTheAppShutsDown(t *testing.T) {
	s := newBackfillStore(t)
	addRecorded(t, s, passageEpoch.Add(time.Hour), nil)
	f := &fakeMotionInflux{data: map[string][]normalMinuteRow{}}
	f.install(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runMotionBackfill(ctx, s, testInflux, backfillNow); !errors.Is(err, context.Canceled) || len(f.calls) != 0 {
		t.Fatalf("err %v, %d queries; want a cancelled run that asked nothing", err, len(f.calls))
	}
}

func TestMotionBackfillSkipsQuietlyWhenInfluxIsNotConfigured(t *testing.T) {
	s := newBackfillStore(t)
	addRecorded(t, s, passageEpoch.Add(time.Hour), nil)
	old := openNormalInflux
	t.Cleanup(func() { openNormalInflux = old })
	openNormalInflux = func(string) (normalInflux, error) { return normalInflux{}, errNormalNoInflux }
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	sum := runMotionBackfillOnce(context.Background(), s, "settings.yaml", backfillNow)
	if sum.Skipped == "" {
		t.Fatal("not configured must be reported as skipped")
	}
	if got := buf.String(); strings.Count(got, "motion backfill skipped") != 1 || !strings.Contains(got, "InfluxDB") {
		t.Fatalf("log %q, want one skipped line naming InfluxDB", got)
	}
}
