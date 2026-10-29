package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func chartTestSources(path map[string][]telemetryPoint, baro []telemetryPoint, pathErr error) watchChartSources {
	return watchChartSources{
		Path: func(_ context.Context, p string, _, _ time.Time) ([]telemetryPoint, string, error) {
			if pathErr != nil {
				return nil, "", pathErr
			}
			return path[p], "", nil
		},
		Barometer: func(time.Time) []telemetryPoint { return baro },
	}
}

func chartPoints(from time.Time, n int, step time.Duration, f func(i int) float64) []telemetryPoint {
	out := make([]telemetryPoint, n)
	for i := range out {
		out[i] = telemetryPoint{Value: f(i), Timestamp: from.Add(time.Duration(i) * step)}
	}
	return out
}

func TestResolveWatchChart_BarometerInOperatorUnit(t *testing.T) {
	now, loc := reportBoatTime()
	pts := chartPoints(now.Add(-12*time.Hour), 100, 7*time.Minute, func(i int) float64 { return 101300 - float64(i)*10 })
	spec := &watchChartSpec{Title: "Pressure, last 12 hours", From: now.Add(-12 * time.Hour), To: now,
		Series:    []watchChartSeriesSpec{{Label: "Pressure", Unit: "hPa", Barometer: true, SIUnit: "Pa"}},
		Threshold: &watchChartThreshold{Label: "3 h fall limit", Value: 1006}}
	c, problem := resolveWatchChart(context.Background(), spec, chartTestSources(nil, pts, nil), now, loc)
	if problem != "" {
		t.Fatal(problem)
	}
	if c.Unit != "hPa" || c.Timezone != "AEST" || c.UTCOffsetSeconds != 10*3600 || c.Threshold == nil {
		t.Errorf("chart %+v", c)
	}
	first := c.Series[0].Points[0]
	if first[1] < 1012.9 || first[1] > 1013.1 {
		t.Errorf("first point %v is not in hPa", first)
	}
}

func TestResolveWatchChart_BucketsToTwoHundredPoints(t *testing.T) {
	now, loc := reportBoatTime()
	pts := chartPoints(now.Add(-12*time.Hour), 3000, 14*time.Second, func(i int) float64 { return float64(i % 50) })
	spec := &watchChartSpec{Title: "t", From: now.Add(-12 * time.Hour), To: now, Series: []watchChartSeriesSpec{{Label: "p", Unit: "x", Barometer: true, SIUnit: "Pa"}}}
	c, problem := resolveWatchChart(context.Background(), spec, chartTestSources(nil, pts, nil), now, loc)
	if problem != "" || len(c.Series[0].Points) > 200 || len(c.Series[0].Points) < 100 {
		t.Fatalf("points %d problem %q", len(c.Series[0].Points), problem)
	}
}

func TestResolveWatchChart_PercentConvertsARatio(t *testing.T) {
	now, loc := reportBoatTime()
	pts := chartPoints(now.Add(-time.Hour), 10, 6*time.Minute, func(i int) float64 { return 0.8 - float64(i)*0.01 })
	spec := &watchChartSpec{Title: "Diesel", From: now.Add(-time.Hour), To: now, Series: []watchChartSeriesSpec{{Label: "Diesel", Unit: "%", Path: "tanks.fuel.0.currentLevel", Convert: "percent"}}}
	c, problem := resolveWatchChart(context.Background(), spec, chartTestSources(map[string][]telemetryPoint{"tanks.fuel.0.currentLevel": pts}, nil, nil), now, loc)
	if problem != "" || c.Series[0].Points[0][1] != 80 {
		t.Fatalf("problem %q chart %+v", problem, c)
	}
}

func TestResolveWatchChart_SaysWhyThereIsNoChart(t *testing.T) {
	now, loc := reportBoatTime()
	pathSpec := &watchChartSpec{Title: "x", From: now.Add(-time.Hour), To: now, Series: []watchChartSeriesSpec{{Label: "x", Unit: "A", Path: "a.b"}}}
	baroSpec := &watchChartSpec{Title: "x", From: now.Add(-time.Hour), To: now, Series: []watchChartSeriesSpec{{Label: "x", Unit: "hPa", Barometer: true, SIUnit: "Pa"}}}
	cases := []struct {
		name string
		spec *watchChartSpec
		src  watchChartSources
		want string
	}{
		{"history source down", pathSpec, chartTestSources(nil, nil, errors.New("InfluxDB history is not available on this install")), "No history to chart: InfluxDB history is not available on this install"},
		{"nothing recorded", pathSpec, chartTestSources(nil, nil, nil), "No history to chart: nothing was recorded"},
		{"barometer still filling", baroSpec, chartTestSources(nil, nil, nil), "No history to chart: the barometer history is empty"},
		{"one reading", baroSpec, chartTestSources(nil, chartPoints(now, 1, time.Minute, func(int) float64 { return 1 }), nil), "No history to chart: only 1 reading"},
		{"planned problem", &watchChartSpec{Problem: "the tank's SignalK path is not known"}, chartTestSources(nil, nil, nil), "No history to chart: the tank's SignalK path is not known"},
	}
	for _, c := range cases {
		got, problem := resolveWatchChart(context.Background(), c.spec, c.src, now, loc)
		if !strings.HasPrefix(problem, c.want) || len(got.Series) != 0 {
			t.Errorf("%s: problem %q chart %+v", c.name, problem, got)
		}
	}
}

func TestAttachWatchCharts_KeepsTryingUntilTwoChartsAreDrawn(t *testing.T) {
	now, loc := reportBoatTime()
	pts := chartPoints(now.Add(-time.Hour), 10, 6*time.Minute, func(i int) float64 { return float64(i) })
	spec := func(path string) *watchChartSpec {
		return &watchChartSpec{Title: path, From: now.Add(-time.Hour), To: now, Series: []watchChartSeriesSpec{{Label: path, Unit: "A", Path: path}}}
	}
	findings := []watchFinding{
		{Text: "no chart"},
		{Text: "a", chart: spec("a")},
		{Text: "b", chart: spec("missing")},
		{Text: "c", chart: spec("c")},
		{Text: "d", chart: spec("d")},
		{Text: "e", chart: spec("e")},
	}
	src := chartTestSources(map[string][]telemetryPoint{"a": pts, "c": pts, "d": pts, "e": pts}, nil, nil)
	charts := attachWatchCharts(context.Background(), findings, src, now, loc)
	if len(charts) != 2 || charts[0].FindingIndex != 1 || charts[1].FindingIndex != 3 {
		t.Fatalf("a failed chart does not use up the two: %+v", charts)
	}
	if !strings.HasPrefix(findings[2].ChartProblem, "No history to chart:") {
		t.Errorf("problem %q", findings[2].ChartProblem)
	}
	for _, i := range []int{4, 5} {
		if findings[i].ChartProblem != "Only two charts per report" {
			t.Errorf("finding %d is past the cap and says so: %q", i, findings[i].ChartProblem)
		}
	}
	if findings[0].ChartProblem != "" || findings[1].ChartProblem != "" || findings[3].ChartProblem != "" {
		t.Errorf("%+v", findings)
	}
}

func TestResolveWatchChart_APercentSeriesIsScaledOnceNotPointByPoint(t *testing.T) {
	now, loc := reportBoatTime()
	spec := &watchChartSpec{Title: "t", From: now.Add(-time.Hour), To: now, Series: []watchChartSeriesSpec{{Label: "Tank", Unit: "%", Path: "tank", Convert: "percent"}}}
	vals := func(v ...float64) []telemetryPoint {
		return chartPoints(now.Add(-time.Hour), len(v), 6*time.Minute, func(i int) float64 { return v[i] })
	}
	// Already percent, nearly empty: 0.5 is half a percent, not half full.
	c, problem := resolveWatchChart(context.Background(), spec, chartTestSources(map[string][]telemetryPoint{"tank": vals(0.5, 80, 70)}, nil, nil), now, loc)
	if problem != "" || c.Series[0].Points[0][1] != 0.5 {
		t.Fatalf("problem %q chart %+v", problem, c.Series)
	}
	// A ratio of full, through the tank helper.
	c, _ = resolveWatchChart(context.Background(), spec, chartTestSources(map[string][]telemetryPoint{"tank": vals(0.4, 1, 0.9)}, nil, nil), now, loc)
	if got := []float64{c.Series[0].Points[0][1], c.Series[0].Points[1][1], c.Series[0].Points[2][1]}; got[0] != 40 || got[1] != 100 || got[2] != 90 {
		t.Errorf("ratio series %v", got)
	}
}

func TestResolveWatchChart_AReadingWithNoDisplayUnitIsNotPlottedInSI(t *testing.T) {
	now, loc := reportBoatTime()
	prev := alarmRuleUnitFor
	alarmRuleUnitFor = func(path string) string {
		if path == "electrical.pump.current" {
			return "A"
		}
		return ""
	}
	t.Cleanup(func() { alarmRuleUnitFor = prev })
	pts := chartPoints(now.Add(-time.Hour), 10, 6*time.Minute, func(i int) float64 { return 2 + float64(i) })
	src := chartTestSources(map[string][]telemetryPoint{"electrical.pump.current": pts, "mystery.path": pts}, nil, nil)
	spec := func(path string) *watchChartSpec {
		return &watchChartSpec{Title: "Pump current", From: now.Add(-time.Hour), To: now, Series: []watchChartSeriesSpec{{Label: "Pump current", Path: path, Convert: "display"}}}
	}
	c, problem := resolveWatchChart(context.Background(), spec("electrical.pump.current"), src, now, loc)
	if problem != "" || c.Unit != "A" {
		t.Fatalf("a path with a unit is drawn in it: %q %+v", problem, c)
	}
	c, problem = resolveWatchChart(context.Background(), spec("mystery.path"), src, now, loc)
	if problem != "No history to chart: no display unit for this reading" || len(c.Series) != 0 {
		t.Errorf("problem %q chart %+v", problem, c)
	}
}

func TestWatchReport_RulesCarryChartSpecs(t *testing.T) {
	// Pressure fall: the barometer over 12 hours, with the fall limit as the line.
	in := baseInput()
	in.Status.Surroundings.Pressure = statusValue{Value: fp(100500), Unit: "Pa", Display: fp(1005), DisplayUnit: "hPa"}
	in.Status.Surroundings.PressureChange3h = statusValue{Value: fp(-400), Unit: "Pa", Display: fp(-4), DisplayUnit: "hPa"}
	_, findings := buildWatchReport(baseWatch(), in)
	got := findingsFor(findings, "weather")
	if len(got) != 1 || got[0].chart == nil {
		t.Fatalf("pressure finding %+v", got)
	}
	s := got[0].chart
	if !s.Series[0].Barometer || s.Series[0].Unit != "hPa" || s.Series[0].SIUnit != "Pa" || !s.From.Equal(in.Now.Add(-12*time.Hour)) {
		t.Errorf("spec %+v", s)
	}
	if s.Threshold == nil || s.Threshold.Value < 1005.99 || s.Threshold.Value > 1006.01 {
		t.Errorf("threshold %+v: 3 h ago was 1009, less the 3 hPa limit is 1006", s.Threshold)
	}

	// Tank change: that tank since the previous run, with its warning level.
	prevReport, _ := buildWatchReport(baseWatch(), baseInput())
	in = baseInput()
	in.Previous, in.PreviousAt = &prevReport, in.Now.Add(-5*time.Hour)
	in.Status.Tanks.Tanks[1].Level = val(66, "%")
	in.Status.Tanks.Tanks[1].levelPath = "tanks.fuel.0.currentLevel"
	in.Status.Tanks.Tanks[1].WarnPercent = 25
	_, findings = buildWatchReport(baseWatch(), in)
	got = findingsFor(findings, "tanks")
	if len(got) != 1 || got[0].chart == nil {
		t.Fatalf("tank finding %+v", got)
	}
	s = got[0].chart
	if s.Series[0].Path != "tanks.fuel.0.currentLevel" || s.Series[0].Convert != "percent" || !s.From.Equal(in.PreviousAt) || s.Threshold == nil || s.Threshold.Value != 25 {
		t.Errorf("spec %+v", s)
	}

	// Overnight draw: SoC and current over the night the observation used.
	in = baseInput()
	start := in.Now.Add(-10 * time.Hour)
	in.Status.Observations = []vesselObservation{{Kind: "overnight_house_draw", Summary: "drained", nightStart: start, nightEnd: start.Add(8 * time.Hour), bankPath: "electrical.batteries.house"}}
	_, findings = buildWatchReport(baseWatch(), in)
	got = findingsFor(findings, "power")
	if len(got) != 1 || got[0].chart == nil {
		t.Fatalf("power finding %+v", got)
	}
	s = got[0].chart
	if len(s.Series) != 2 || s.Series[0].Path != "electrical.batteries.house.capacity.stateOfCharge" || s.Series[0].Convert != "percent" ||
		s.Series[1].Path != "electrical.batteries.house.current" || s.Series[1].Unit != "A" || !s.From.Equal(start) || !s.To.Equal(start.Add(8*time.Hour)) {
		t.Errorf("spec %+v", s)
	}
}

func TestWatchTick_ChartsGetTheirOwnTimeWhenTheReviewUsesUpTheRun(t *testing.T) {
	prev := watchRunTimeout
	watchRunTimeout = 50 * time.Millisecond
	t.Cleanup(func() { watchRunTimeout = prev })
	f := newWatchHostFixture(t)
	f.add("Hourly", "0 * * * *")
	from := f.clock.Add(-2 * time.Hour)
	pts := chartPoints(from, 30, 4*time.Minute, func(i int) float64 { return float64(i) })
	f.findings = []watchFinding{{Section: "power", Rule: "overnight_draw", Text: "x", By: watchByRules,
		chart: &watchChartSpec{Title: "Pump", From: from, To: f.clock, Series: []watchChartSeriesSpec{{Label: "Pump", Unit: "A", Path: "p"}}}}}
	f.host.review = func(ctx context.Context, _ watchConfig, _ savedWatch, _ watchReport, _ []watchFinding, _ time.Time, _ *time.Location) watchReviewOutcome {
		<-ctx.Done()
		return watchReviewOutcome{Problem: "Mate ran out of time", CostKnown: true}
	}
	f.host.chartSources = func() watchChartSources {
		return watchChartSources{Path: func(ctx context.Context, _ string, _, _ time.Time) ([]telemetryPoint, string, error) {
			if err := ctx.Err(); err != nil {
				return nil, "", err
			}
			return pts, "", nil
		}}
	}
	f.host.tick(context.Background())
	r := f.runsOf("id-Hourly")[0]
	if len(r.Charts) != 1 || r.Findings[0].ChartProblem != "" {
		t.Fatalf("the run's time was spent by the review, charts %+v problem %q", r.Charts, r.Findings[0].ChartProblem)
	}
}

func TestWatchTick_SavesTheChartsOfRuleAndMateFindingsAndSaysWhenThereAreNone(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("Hourly", "0 * * * *")
	from := f.clock.Add(-2 * time.Hour)
	pts := chartPoints(from, 30, 4*time.Minute, func(i int) float64 { return 100000 - float64(i)*20 })
	baro := &watchChartSpec{Title: "Pressure", From: from, To: f.clock, Series: []watchChartSeriesSpec{{Label: "Pressure", Unit: "hPa", Barometer: true, SIUnit: "Pa"}}}
	path := &watchChartSpec{Title: "Pump", From: from, To: f.clock, Series: []watchChartSeriesSpec{{Label: "Pump", Unit: "A", Path: "electrical.pump.current"}}}
	f.findings = []watchFinding{{Section: "weather", Rule: "pressure_fall", Text: "down", By: watchByRules, chart: baro}}
	f.review.Added = []watchFinding{{Section: "power", Rule: "mate_review", Text: "pump", By: watchByMate, Key: "pump_current", chart: path}}
	f.host.chartSources = func() watchChartSources {
		return chartTestSources(map[string][]telemetryPoint{"electrical.pump.current": pts}, pts, nil)
	}
	f.host.tick(context.Background())
	r := f.runsOf("id-Hourly")[0]
	if len(r.Charts) != 2 || r.Charts[0].FindingIndex != 0 || r.Charts[1].FindingIndex != 1 || r.Charts[0].Unit != "hPa" {
		t.Fatalf("charts %+v", r.Charts)
	}

	// With no history source the finding says why, and nothing is drawn.
	f2 := newWatchHostFixture(t)
	f2.add("Hourly", "0 * * * *")
	f2.findings = f.findings
	f2.host.chartSources = func() watchChartSources {
		return chartTestSources(nil, nil, errors.New("InfluxDB history is not available on this install"))
	}
	f2.host.tick(context.Background())
	r2 := f2.runsOf("id-Hourly")[0]
	if len(r2.Charts) != 0 || !strings.HasPrefix(r2.Findings[0].ChartProblem, "No history to chart: the barometer history is empty") {
		t.Fatalf("run %+v", r2)
	}
}
