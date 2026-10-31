package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

// Review finding 2: a bucket's label is the range start when Flux clipped the
// first window, as it is for a scalar. It is never earlier than the range.
func TestCombineCircularLabelsTheClippedFirstBucketWithTheRangeStart(t *testing.T) {
	start := time.Date(2026, 10, 20, 0, 25, 0, 0, time.UTC)
	subs := []circularSample{{T: start, Sin: 0, Cos: 1, Weight: 1, HasWeight: true}}
	b := combineCircular(subs, time.Hour, start)
	if len(b) != 1 || !b[0].T.Equal(start) {
		t.Fatalf("bucket label = %+v, want the range start %v, not the hour before it", b, start)
	}
	later := []circularSample{{T: start.Add(40 * time.Minute), Sin: 0, Cos: 1, Weight: 1, HasWeight: true}}
	if b := combineCircular(later, time.Hour, start); !b[0].T.Equal(time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)) {
		t.Errorf("a later bucket keeps its boundary label, got %v", b[0].T)
	}
}

// Review finding 3: a missing speed never hides a direction that was logged.
func TestCombineCircularKeepsABucketWhoseSpeedIsMissingAndCountsItUnweighted(t *testing.T) {
	t0 := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	subs := []circularSample{
		{T: t0, Sin: 1, Cos: 0, Weight: 1}, // no speed logged for this window
		{T: t0.Add(time.Hour), Sin: 0, Cos: 1, Weight: 5, HasWeight: true},
	}
	b := combineCircular(subs, time.Hour, t0)
	if len(b) != 2 || !b[0].Defined || !b[0].Unweighted {
		t.Fatalf("buckets = %+v, want the direction kept and marked unweighted", b)
	}
	if b[1].Unweighted {
		t.Error("a bucket with speed is weighted")
	}
}

func TestQueryCircularHistoryBucketsReportsBucketsThatHadNoSpeed(t *testing.T) {
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	io := pathHistoryIO{stat: func(_ context.Context, path, _ string, _, _ time.Time, _, fn string) ([]telemetryPoint, error) {
		if path == windSpeedTruePath {
			// Speed only in the second hour.
			return []telemetryPoint{{Timestamp: start.Add(time.Hour), Value: 5}}, nil
		}
		v := math.Sin(rad(45))
		if fn == "cos_mean" {
			v = math.Cos(rad(45))
		}
		return []telemetryPoint{{Timestamp: start, Value: v}, {Timestamp: start.Add(time.Hour), Value: v}}, nil
	}}
	widthFor := func(time.Duration) (string, time.Duration) { return "1h", time.Hour }
	got, err := queryCircularHistoryBuckets(context.Background(), io, windDirectionTruePath, "", windSpeedTruePath, "", start, start.Add(2*time.Hour), false, widthFor)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Weighted || got.UnweightedBuckets != 1 || got.Undefined != 0 {
		t.Errorf("weighted %v unweighted buckets %d undefined %d, want weighted with 1 unweighted bucket and no gap", got.Weighted, got.UnweightedBuckets, got.Undefined)
	}
	if len(got.Merged) != 2 || got.Merged[0].Mean == nil {
		t.Fatalf("a speed outage must not drop the direction: %+v", got.Merged)
	}
	if m := circularMethod(windSpeedTruePath, false, got.Weighted, got.UnweightedBuckets); !strings.Contains(m, "1 bucket") || !strings.Contains(m, "no wind speed") {
		t.Errorf("method = %q, want it to say 1 bucket had no wind speed", m)
	}
}

// Review finding 6: the weight uses the speed source chosen in the same chart.
func TestShowTelemetryChartWeightsDirectionByTheSpeedSourceChosenInTheChart(t *testing.T) {
	var mu sync.Mutex
	weightSources := map[string]bool{}
	f := tcWindFake(func(int) float64 { return 90 }, 24)
	f.series[windSpeedTruePath+"|derived"] = f.series[windSpeedTruePath]
	d := tcDeps(f, tcVessel(), "metric")
	base := f.stat
	d.influxPathHistoryStat = func(ctx context.Context, path, source string, a, b time.Time, every, fn string) ([]telemetryPoint, error) {
		if path == windSpeedTruePath {
			mu.Lock()
			weightSources[source] = true
			mu.Unlock()
		}
		return base(ctx, path, source, a, b, every, fn)
	}
	// Two devices log the speed: only the chosen one may be read.
	d.influxPathSources = func(_ context.Context, p string, _, _ time.Time) ([]string, error) {
		if p == windSpeedTruePath {
			return []string{"nmea", "derived"}, nil
		}
		return nil, nil
	}
	res, err := tcRun(t, d, fmt.Sprintf(`{"series":[{"path":%q,"source":"derived"},{"path":%q}],%s}`, windSpeedTruePath, windDirectionTruePath, tcRange))
	if err != nil {
		t.Fatal(err)
	}
	if weightSources[""] || !weightSources["derived"] {
		t.Errorf("speed read with sources %v, want only derived", weightSources)
	}
	var m chartModelResult
	if err := json.Unmarshal([]byte(res), &m); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.Series[1].Method, "weighted by wind speed") || strings.Contains(m.Series[1].Method, "not weighted") {
		t.Errorf("method = %q", m.Series[1].Method)
	}
}

// Review finding 7: with no way to check the speed's sources, say so.
func TestGetPathHistoryOnAnAngleWithoutSourceCheckingFailsFast(t *testing.T) {
	now := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	deps := assistantToolDeps{
		now:         func() time.Time { return now },
		vesselState: func() (vesselStateData, error) { return vesselStateData{}, errNoVesselState },
		influxPathHistoryStat: func(context.Context, string, string, time.Time, time.Time, string, string) ([]telemetryPoint, error) {
			return nil, nil
		},
		influxPathHistoryFirstLast: stubInfluxPathHistoryFirstLast(now, now, true),
		siUnit:                     func(string) string { return "rad" },
	}
	_, _, err := deps.execute(context.Background(), "get_path_history", json.RawMessage(`{"path":"environment.wind.directionTrue","hours_back":3}`))
	if err == nil || !strings.Contains(err.Error(), "sources") {
		t.Errorf("err = %v, want a refusal naming the missing source check", err)
	}
}

// Review findings 5, 9: 32 sectors are named by degrees, 16 or fewer by compass point.
func TestWindRoseSectorNames(t *testing.T) {
	if got := windRoseSectorName(90, 16); got != "E" {
		t.Errorf("16: %q", got)
	}
	if got := windRoseSectorName(45, 8); got != "NE" {
		t.Errorf("8: %q", got)
	}
	a, b := windRoseSectorName(11.25, 32), windRoseSectorName(33.75, 32)
	if a == b || a != "11°" {
		t.Errorf("32 sectors must not share names: %q %q", a, b)
	}
}

// Review finding 10: the summary comes from the tally.
func TestBuildWindRoseTalliesEachSectorsCountAndMeanSpeed(t *testing.T) {
	deg := func(d float64) *float64 { return &d }
	tally := buildWindRose([]windRoseSample{{Dir: deg(90), Speed: 6}, {Dir: deg(91), Speed: 10}, {Dir: deg(180), Speed: 20}}, 16, []float64{5, 10}, 1)
	if tally.SectorN[4] != 2 || math.Abs(tally.SectorSpeed[4]-16) > 1e-9 || tally.SectorN[8] != 1 {
		t.Errorf("tally = %+v", tally)
	}
}
