package main

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"
)

func TestAngleKindOf(t *testing.T) {
	cases := []struct {
		path, si string
		kind     angleKind
		label    string
	}{
		{"environment.wind.directionTrue", "rad", angleBearing, "°T"},
		{"environment.wind.directionMagnetic", "rad", angleBearing, "°M"},
		{"navigation.headingMagnetic", "rad", angleBearing, "°M"},
		{"navigation.courseOverGroundTrue", "rad", angleBearing, "°T"},
		{"navigation.courseRhumbline.nextPoint.bearingTrue", "rad", angleBearing, "°T"},
		{"environment.wind.angleApparent", "rad", angleRelative, "°"},
		{"steering.rudderAngle", "rad", angleRelative, "°"},
		{"environment.wind.directionTrue", "", angleNone, ""},    // undeclared: not guessed
		{"environment.wind.speedTrue", "m/s", angleNone, ""},     // not an angle
		{"navigation.rateOfTurn", "rad/s", angleNone, ""},        // a rate, not an angle
		{"environment.wind.directionTrue", "deg", angleNone, ""}, // only rad is converted
	}
	for _, c := range cases {
		if got := angleKindOf(c.path, c.si); got != c.kind {
			t.Errorf("%s (%q): kind %q, want %q", c.path, c.si, got, c.kind)
		}
		if c.kind != angleNone {
			if got := angleUnitLabel(c.path); got != c.label {
				t.Errorf("%s: label %q, want %q", c.path, got, c.label)
			}
		}
	}
}

func TestAngleDegrees(t *testing.T) {
	near := func(got, want float64) bool { return math.Abs(got-want) < 1e-9 }
	if got := angleDegrees(angleBearing, -math.Pi/2); !near(got, 270) {
		t.Errorf("bearing -90 deg = %v, want 270", got)
	}
	if got := angleDegrees(angleBearing, 2*math.Pi); !near(got, 0) {
		t.Errorf("bearing 360 deg = %v, want 0", got)
	}
	if got := angleDegrees(angleRelative, 3*math.Pi/2); !near(got, -90) {
		t.Errorf("relative 270 deg = %v, want -90", got)
	}
	if got := angleDegrees(angleRelative, math.Pi); !near(got, 180) {
		t.Errorf("relative 180 deg = %v, want 180", got)
	}
}

func rad(deg float64) float64 { return deg * math.Pi / 180 }

func TestCircularMeanAcrossNorth(t *testing.T) {
	// 350 and 10 degrees: a linear mean says 180, the vector mean says north.
	s := (math.Sin(rad(350)) + math.Sin(rad(10))) / 2
	c := (math.Cos(rad(350)) + math.Cos(rad(10))) / 2
	m, r, ok := circularMean(s, c)
	if !ok {
		t.Fatalf("350 and 10 should have a mean, r=%v", r)
	}
	deg := m * 180 / math.Pi
	if deg > 0.001 && deg < 359.999 {
		t.Errorf("mean of 350 and 10 = %v deg, want north", deg)
	}
	if r < 0.98 {
		t.Errorf("resultant = %v, want close to 1", r)
	}
}

func TestCircularMeanOfOpposingVectorsIsUndefined(t *testing.T) {
	s := (math.Sin(rad(90)) + math.Sin(rad(270))) / 2
	c := (math.Cos(rad(90)) + math.Cos(rad(270))) / 2
	if _, _, ok := circularMean(s, c); ok {
		t.Error("east and west cancel out: there must be no mean")
	}
	if _, _, ok := circularMean(0, 0); ok {
		t.Error("a zero vector has no direction")
	}
}

func TestCircularSpreadGrowsAsTheVectorShrinks(t *testing.T) {
	if got := circularSpreadDegrees(1); got != 0 {
		t.Errorf("spread of a perfect vector = %v, want 0", got)
	}
	if circularSpreadDegrees(0.9) >= circularSpreadDegrees(0.5) {
		t.Error("a shorter resultant must mean a wider spread")
	}
}

func TestCombineCircularWeightsBySpeed(t *testing.T) {
	t0 := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	sub := func(min int, deg, w float64) circularSample {
		return circularSample{T: t0.Add(time.Duration(min) * time.Minute), Sin: math.Sin(rad(deg)), Cos: math.Cos(rad(deg)), Weight: w, HasWeight: true}
	}
	// One hour bucket: 30 minutes of a 2 kn breeze from the east, 30 of a 20 kn
	// blow from the south. Unweighted, the mean sits midway (135); weighted, the
	// blow dominates.
	subs := []circularSample{sub(0, 90, 1), sub(30, 180, 1)}
	un := combineCircular(subs, time.Hour, t0)
	if len(un) != 1 || !un[0].Defined {
		t.Fatalf("unweighted = %+v", un)
	}
	if got := un[0].Mean * 180 / math.Pi; math.Abs(got-135) > 0.01 {
		t.Errorf("unweighted mean = %v, want 135", got)
	}
	subs = []circularSample{sub(0, 90, 2), sub(30, 180, 20)}
	w := combineCircular(subs, time.Hour, t0)
	if got := w[0].Mean * 180 / math.Pi; got < 165 || got > 175 {
		t.Errorf("speed-weighted mean = %v, want near 174", got)
	}
}

func TestCombineCircularCancellationAndCalmAreUndefined(t *testing.T) {
	t0 := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	opposed := []circularSample{
		{T: t0, Sin: 1, Cos: 0, Weight: 1, HasWeight: true},
		{T: t0.Add(30 * time.Minute), Sin: -1, Cos: 0, Weight: 1, HasWeight: true},
	}
	if b := combineCircular(opposed, time.Hour, t0); len(b) != 1 || b[0].Defined {
		t.Errorf("opposed vectors = %+v, want one undefined bucket", b)
	}
	calm := []circularSample{{T: t0, Sin: 1, Cos: 0, Weight: 0, HasWeight: true}}
	if b := combineCircular(calm, time.Hour, t0); len(b) != 1 || b[0].Defined {
		t.Errorf("a calm bucket (zero weight) = %+v, want one undefined bucket", b)
	}
}

func TestCombineCircularGroupsByBucketAndSorts(t *testing.T) {
	t0 := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	subs := []circularSample{
		{T: t0.Add(61 * time.Minute), Sin: 0, Cos: 1, Weight: 1},
		{T: t0, Sin: 0, Cos: 1, Weight: 1},
	}
	b := combineCircular(subs, time.Hour, t0)
	if len(b) != 2 || !b[0].T.Before(b[1].T) {
		t.Fatalf("buckets = %+v", b)
	}
}

func TestCircularSubWidthIsASixthOfTheBucketAndDividesIt(t *testing.T) {
	cases := []struct {
		width time.Duration
		want  time.Duration
	}{
		{time.Minute, time.Minute},
		{5 * time.Minute, time.Minute},
		{time.Hour, 10 * time.Minute},
		{4 * time.Hour, 30 * time.Minute},
		{12 * time.Hour, 2 * time.Hour},
		{48 * time.Hour, 6 * time.Hour},
	}
	for _, c := range cases {
		label, got := circularSubWidth(c.width)
		if got != c.want || label == "" {
			t.Errorf("%v: sub width %v (%q), want %v", c.width, got, label, c.want)
		}
		if c.width%got != 0 {
			t.Errorf("%v: %v does not divide it", c.width, got)
		}
	}
}

func TestWeightPathFor(t *testing.T) {
	if got := weightPathFor("environment.wind.directionTrue"); got != "environment.wind.speedTrue" {
		t.Errorf("true wind direction weight = %q", got)
	}
	if got := weightPathFor("environment.wind.directionMagnetic"); got != "environment.wind.speedTrue" {
		t.Errorf("magnetic wind direction weight = %q", got)
	}
	if got := weightPathFor("navigation.headingMagnetic"); got != "" {
		t.Errorf("a heading has no weight, got %q", got)
	}
}

// circularFake serves a direction as sin/cos means and the true wind speed as a
// mean, the way the two Influx aggregates would.
func circularFake(dirDeg, speed func(i int) float64, n int, step time.Duration, start time.Time) func(ctx context.Context, path, source string, a, b time.Time, every, fn string) ([]telemetryPoint, error) {
	return func(_ context.Context, path, _ string, _, _ time.Time, _, fn string) ([]telemetryPoint, error) {
		var out []telemetryPoint
		for i := 0; i < n; i++ {
			ts := start.Add(time.Duration(i) * step)
			switch {
			case path == windSpeedTruePath && (fn == "mean" || fn == "max"):
				out = append(out, telemetryPoint{Timestamp: ts, Value: speed(i)})
			case path != windSpeedTruePath && fn == "sin_mean":
				out = append(out, telemetryPoint{Timestamp: ts, Value: math.Sin(rad(dirDeg(i)))})
			case path != windSpeedTruePath && fn == "cos_mean":
				out = append(out, telemetryPoint{Timestamp: ts, Value: math.Cos(rad(dirDeg(i)))})
			}
		}
		return out, nil
	}
}

func TestQueryCircularHistoryBucketsAveragesAcrossNorthAndUsesSpeedWeights(t *testing.T) {
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	// 12 ten-minute samples in two one-hour buckets. Bucket one swings across
	// north; in bucket two the strong wind blows from the south.
	dir := func(i int) float64 {
		if i < 6 {
			return []float64{350, 10, 350, 10, 350, 10}[i]
		}
		return []float64{90, 90, 90, 180, 180, 180}[i-6]
	}
	speed := func(i int) float64 {
		if i < 6 {
			return 5
		}
		return []float64{1, 1, 1, 10, 10, 10}[i-6]
	}
	io := pathHistoryIO{stat: circularFake(dir, speed, 12, 10*time.Minute, start)}
	widthFor := func(time.Duration) (string, time.Duration) { return "1h", time.Hour }
	got, err := queryCircularHistoryBuckets(context.Background(), io, "environment.wind.directionTrue", "", windSpeedTruePath, "", start, start.Add(2*time.Hour), false, widthFor)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Weighted {
		t.Error("speed is logged, so the mean is speed-weighted")
	}
	if len(got.Merged) != 2 || got.Merged[0].Mean == nil || got.Merged[1].Mean == nil {
		t.Fatalf("merged = %+v", got.Merged)
	}
	d0 := *got.Merged[0].Mean * 180 / math.Pi
	if d0 > 0.01 && d0 < 359.99 {
		t.Errorf("bucket 0 = %v deg, want north, not the linear 180", d0)
	}
	d1 := *got.Merged[1].Mean * 180 / math.Pi
	if d1 < 150 || d1 > 180 {
		t.Errorf("bucket 1 = %v deg, want pulled towards the 10 kn south wind", d1)
	}
	if got.Merged[0].Min != nil || got.Merged[0].Max != nil {
		t.Error("an angle has no linear min or max")
	}
}

func TestQueryCircularHistoryBucketsWithoutSpeedIsUnweightedAndSaysSo(t *testing.T) {
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	io := pathHistoryIO{stat: func(_ context.Context, path, _ string, _, _ time.Time, _, fn string) ([]telemetryPoint, error) {
		if path == windSpeedTruePath {
			return nil, nil
		}
		v := math.Sin(rad(45))
		if fn == "cos_mean" {
			v = math.Cos(rad(45))
		}
		return []telemetryPoint{{Timestamp: start, Value: v}}, nil
	}}
	widthFor := func(time.Duration) (string, time.Duration) { return "1h", time.Hour }
	got, err := queryCircularHistoryBuckets(context.Background(), io, "environment.wind.directionTrue", "", windSpeedTruePath, "", start, start.Add(time.Hour), false, widthFor)
	if err != nil {
		t.Fatal(err)
	}
	if got.Weighted {
		t.Error("no speed logged: the mean is not weighted, and the result must say so")
	}
	if len(got.Merged) != 1 || got.Merged[0].Mean == nil {
		t.Fatalf("merged = %+v", got.Merged)
	}
	if d := *got.Merged[0].Mean * 180 / math.Pi; math.Abs(d-45) > 0.01 {
		t.Errorf("mean = %v, want 45", d)
	}
}

func TestBuildInfluxPathStatFluxCircularAggregatesMeanTheSineAndCosine(t *testing.T) {
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	for fn, trig := range map[string]string{"sin_mean": "math.sin(x: r._value)", "cos_mean": "math.cos(x: r._value)"} {
		flux, err := buildInfluxPathStatFlux("SignalK_Data", "value", "environment.wind.directionTrue", "", start, start.Add(time.Hour), "10m", fn)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`import "math"`, trig, "aggregateWindow(every: 10m, fn: mean,", `timeSrc: "_start"`} {
			if !strings.Contains(flux, want) {
				t.Errorf("%s: flux %q should contain %q", fn, flux, want)
			}
		}
		// Pushdown: nothing but from, range and filter may come before the first
		// aggregateWindow, or InfluxDB streams every raw point through Flux. That
		// first window takes the last sample of each short window, which copies a
		// value and never averages across north; the trig runs on those.
		first := strings.Index(flux, "aggregateWindow(")
		if m := strings.Index(flux, "map("); m < first {
			t.Errorf("%s: a map() runs before the first aggregateWindow, which stops it being pushed down: %q", fn, flux)
		}
		if !strings.Contains(flux[first:], "fn: last,") || !strings.Contains(flux[first:strings.Index(flux, "map(")], "createEmpty: false") {
			t.Errorf("%s: the first aggregateWindow must sample with last: %q", fn, flux)
		}
	}
	linear, _ := buildInfluxPathStatFlux("SignalK_Data", "value", "a.b", "", start, start.Add(time.Hour), "10m", "mean")
	if strings.Contains(linear, "math") {
		t.Errorf("a linear mean needs no math import: %q", linear)
	}
}

func TestQueryCircularHistoryBucketsWithNothingLoggedIsNotFound(t *testing.T) {
	io := pathHistoryIO{stat: func(context.Context, string, string, time.Time, time.Time, string, string) ([]telemetryPoint, error) {
		return nil, nil
	}}
	widthFor := func(time.Duration) (string, time.Duration) { return "1h", time.Hour }
	got, err := queryCircularHistoryBuckets(context.Background(), io, "a.b", "", "", "", time.Now(), time.Now().Add(time.Hour), false, widthFor)
	if err != nil {
		t.Fatal(err)
	}
	if got.Found {
		t.Error("nothing logged")
	}
}

func TestCircularSampleWidthKeepsARangeToAFewTensOfThousandsOfPoints(t *testing.T) {
	day := 24 * time.Hour
	cases := []struct {
		span time.Duration
		want string
	}{
		{day, "1m"}, {7 * day, "1m"}, {14 * day, "2m"}, {30 * day, "5m"}, {60 * day, "5m"}, {90 * day, "10m"},
	}
	for _, c := range cases {
		label, w := circularSampleWidth(c.span)
		if label != c.want {
			t.Errorf("%v: sample width %s, want %s", c.span, label, c.want)
		}
		if n := int(c.span / w); n > circularMaxSamples {
			t.Errorf("%v at %s is %d points, over %d", c.span, label, n, circularMaxSamples)
		}
	}
}

func TestBuildInfluxPathStatFluxSamplesAtNoMoreThanTheWindowItAverages(t *testing.T) {
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	// A 90 day range samples every 10m, but a 5m window cannot average 10m samples.
	flux, err := buildInfluxPathStatFlux("b", "value", "a.b", "", start, start.Add(90*24*time.Hour), "5m", "sin_mean")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(flux, "aggregateWindow(every: 5m, fn: last,") {
		t.Errorf("sample width must not exceed the window: %q", flux)
	}
}

func TestBuildInfluxPathStatFluxForAThreeDayDirectionQuery(t *testing.T) {
	start := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	flux, err := buildInfluxPathStatFlux("SignalK_Data", "value", "environment.wind.directionTrue", "", start, start.Add(72*time.Hour), "10m", "sin_mean")
	if err != nil {
		t.Fatal(err)
	}
	want := `import "math"
from(bucket: "SignalK_Data") |> range(start: time(v: "2026-10-25T00:00:00Z"), stop: time(v: "2026-10-28T00:00:00Z")) |> filter(fn: (r) => r._measurement == "environment.wind.directionTrue" and r._field == "value") |> aggregateWindow(every: 1m, fn: last, createEmpty: false, timeSrc: "_start") |> map(fn: (r) => ({r with _value: math.sin(x: r._value)})) |> aggregateWindow(every: 10m, fn: mean, createEmpty: false, timeSrc: "_start") |> keep(columns: ["_time", "_value"])`
	if flux != want {
		t.Errorf("flux =\n%s\nwant\n%s", flux, want)
	}
}
