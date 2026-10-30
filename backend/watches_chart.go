package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"log"
	"math"
	"sort"
	"time"
)

// watchChart is a finding's chart as it is stored and sent (ADR 0228): the
// points, never an image. The PNG is drawn from it when something needs one.
type watchChart struct {
	// FindingIndex is the finding in the run's findings the chart belongs to.
	FindingIndex int    `json:"finding_index"`
	Title        string `json:"title"`
	// Unit is the unit of the first series, which the threshold is in.
	Unit string `json:"unit"`
	// Timezone is the boat clock's offset label ("UTC+12") and UTCOffsetSeconds
	// its offset, so the time axis reads in the boat's time.
	Timezone         string               `json:"timezone"`
	UTCOffsetSeconds int                  `json:"utc_offset_seconds"`
	Window           watchChartWindow     `json:"window"`
	Series           []watchChartSeries   `json:"series"`
	Threshold        *watchChartThreshold `json:"threshold,omitempty"`
	// AlarmThreshold is a second dashed line, for a tank whose warn and alarm
	// levels are both drawn (ADR 0235). Threshold is the warn level.
	AlarmThreshold *watchChartThreshold `json:"alarm_threshold,omitempty"`
	// PNGBase64 and Problem are set only in what is sent: a webhook payload
	// carries the picture, or says why the chart was left out.
	PNGBase64 string `json:"png_base64,omitempty"`
	Problem   string `json:"problem,omitempty"`
}

// MarshalJSON writes a chart that was left out before it was ever read (an
// alarm's, ADR 0235: no series) as its title and problem alone, since the empty
// window, unit and zone would only suggest there is something to draw.
func (c watchChart) MarshalJSON() ([]byte, error) {
	if len(c.Series) == 0 && c.Problem != "" {
		return json.Marshal(struct {
			Title   string `json:"title"`
			Problem string `json:"problem"`
		}{c.Title, c.Problem})
	}
	type plain watchChart // no methods, so no recursion
	return json.Marshal(plain(c))
}

// thresholds are the dashed lines the chart draws, warn first.
func (c watchChart) thresholds() []watchChartThreshold {
	var out []watchChartThreshold
	for _, t := range []*watchChartThreshold{c.Threshold, c.AlarmThreshold} {
		if t != nil {
			out = append(out, *t)
		}
	}
	return out
}

type watchChartWindow struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type watchChartSeries struct {
	Label string `json:"label"`
	Unit  string `json:"unit"`
	// Points are [unix milliseconds, value in Unit].
	Points [][2]float64 `json:"points"`
}

type watchChartThreshold struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

var watchChartColours = []color.RGBA{{0, 114, 178, 255}, {213, 94, 0, 255}}
var watchChartThresholdColour = color.RGBA{204, 121, 167, 255}

type watchChartSources struct {
	Path      func(ctx context.Context, path string, from, to time.Time) ([]telemetryPoint, string, error)
	Barometer func(since time.Time) []telemetryPoint
}
type watchChartSpec struct {
	Title     string
	From, To  time.Time
	Series    []watchChartSeriesSpec
	Threshold *watchChartThreshold
	// AlarmThreshold is drawn beside Threshold; see watchChart.
	AlarmThreshold *watchChartThreshold
	Problem        string
}
type watchChartSeriesSpec struct {
	Label, Unit, Path, Convert string
	Barometer                  bool
	// Role is a path-role rule's role: it names the unit the figures are in
	// when the path alone does not (ADR 0187).
	Role string
	// SIUnit is the unit a barometer series is stored in. It is converted to
	// the operator's unit with the same table alarms use.
	SIUnit string
}

const (
	watchChartMaxPoints = 200
	watchChartMaxCharts = 2
	watchChartNoHistory = "No history to chart: "
)

func chartProblem(reason string) string { return watchChartNoHistory + reason }

// bucketChartPoints averages points into at most watchChartMaxPoints equal
// time buckets over [from, to]. Buckets with no points are left out, so a gap
// in the record stays a gap.
func bucketChartPoints(pts []telemetryPoint, from, to time.Time) []telemetryPoint {
	if len(pts) <= watchChartMaxPoints {
		return pts
	}
	width := to.Sub(from) / watchChartMaxPoints
	if width <= 0 {
		width = time.Second
	}
	type acc struct {
		sum float64
		n   int
	}
	buckets := map[int]*acc{}
	var order []int
	for _, p := range pts {
		i := int(p.Timestamp.Sub(from) / width)
		if i >= watchChartMaxPoints {
			i = watchChartMaxPoints - 1
		}
		if i < 0 {
			i = 0
		}
		b := buckets[i]
		if b == nil {
			b = &acc{}
			buckets[i] = b
			order = append(order, i)
		}
		b.sum += p.Value
		b.n++
	}
	sort.Ints(order)
	out := make([]telemetryPoint, 0, len(order))
	for _, i := range order {
		b := buckets[i]
		out = append(out, telemetryPoint{Value: b.sum / float64(b.n), Timestamp: from.Add(time.Duration(i)*width + width/2)})
	}
	return out
}

func chartRound(v float64) float64 { return math.Round(v*1000) / 1000 }

// resolveWatchChart reads a finding's chart spec into a chart. It returns the
// reason there is no chart instead of drawing anything it could not read.
func resolveWatchChart(ctx context.Context, spec *watchChartSpec, src watchChartSources, now time.Time, loc *time.Location) (watchChart, string) {
	if spec.Problem != "" {
		return watchChart{}, chartProblem(spec.Problem)
	}
	zone := watchBoatZone(now, loc)
	if zone == "" {
		zone = "UTC"
		loc = time.UTC
	}
	_, off := now.In(loc).Zone()
	c := watchChart{
		Title: spec.Title, Timezone: zone, UTCOffsetSeconds: off, Threshold: spec.Threshold, AlarmThreshold: spec.AlarmThreshold,
		Window: watchChartWindow{From: spec.From.UTC().Format(time.RFC3339), To: spec.To.UTC().Format(time.RFC3339)},
	}
	for _, ss := range spec.Series {
		var pts []telemetryPoint
		unit := ss.Unit
		switch {
		case ss.Barometer:
			if src.Barometer == nil {
				return watchChart{}, chartProblem("the barometer history is not wired")
			}
			if _, _, _, ok := operatorUnit(ss.SIUnit); !ok {
				return watchChart{}, chartProblem("no display unit for " + ss.Label)
			}
			pts = src.Barometer(spec.From)
			if len(pts) == 0 {
				return watchChart{}, chartProblem("the barometer history is empty (it starts empty when Helmcentral restarts)")
			}
		default:
			if src.Path == nil {
				return watchChart{}, chartProblem("history is not available on this install")
			}
			var already string
			var err error
			pts, already, err = src.Path(ctx, ss.Path, spec.From, spec.To)
			if err != nil {
				return watchChart{}, chartProblem(firstErrorLine(err))
			}
			if len(pts) == 0 {
				return watchChart{}, chartProblem("nothing was recorded for " + ss.Label + " in this window")
			}
			if already != "" {
				unit = already
			}
			ss.Convert = chartConvertFor(ss, already != "")
			if ss.Convert == "display" {
				disp, known := alarmDisplayFor(ss.Path, ss.Role)
				if !known {
					return watchChart{}, chartProblem("no display unit for this reading")
				}
				unit = disp.label
			}
		}
		var in []telemetryPoint
		for _, p := range pts {
			if !p.Timestamp.Before(spec.From) && !p.Timestamp.After(spec.To) {
				in = append(in, p)
			}
		}
		if len(in) < 2 {
			return watchChart{}, chartProblem(fmt.Sprintf("only %d %s of %s in the window", len(in), watchPlural(len(in), "reading", "readings"), ss.Label))
		}
		in = bucketChartPoints(in, spec.From, spec.To)
		// A percent series is a ratio of full or already a percent, and which
		// is decided once for the whole series: a tank at 0.5 % must not turn
		// into 50 % because one point happens to be small.
		ratio := ss.Convert == "percent"
		if ratio {
			for _, p := range in {
				if p.Value > 1 {
					ratio = false
					break
				}
			}
		}
		_, baroConv, _, _ := operatorUnit(ss.SIUnit)
		series := watchChartSeries{Label: ss.Label, Unit: unit, Points: make([][2]float64, 0, len(in))}
		for _, p := range in {
			v := p.Value
			switch {
			case ss.Barometer:
				v = baroConv(v)
			case ratio:
				percent, ok := tankLevelPercent(v)
				if !ok {
					continue // a reading a tank sender cannot give is left out
				}
				v = percent
			case ss.Convert == "display":
				if disp, known := alarmDisplayFor(ss.Path, ss.Role); known {
					v = disp.toShown(v)
				}
			}
			series.Points = append(series.Points, [2]float64{float64(p.Timestamp.UnixMilli()), chartRound(v)})
		}
		if len(series.Points) < 2 {
			return watchChart{}, chartProblem(fmt.Sprintf("only %d %s of %s in the window", len(series.Points), watchPlural(len(series.Points), "usable reading", "usable readings"), ss.Label))
		}
		c.Series = append(c.Series, series)
	}
	if len(c.Series) == 0 {
		return watchChart{}, chartProblem("there is nothing to plot")
	}
	c.Unit = c.Series[0].Unit
	return c, ""
}

// chartConvertFor is how a path's figures reach the operator's unit: as given
// when the reader already returned them in one (an angle), else as the spec says.
func chartConvertFor(ss watchChartSeriesSpec, alreadyConverted bool) string {
	if alreadyConverted {
		return ""
	}
	return ss.Convert
}

// watchChartCapProblem is what a finding says when two charts are already made.
const watchChartCapProblem = "Only two charts per report"

// attachWatchCharts resolves the charts findings ask for, in finding order,
// until two are drawn. A chart that cannot be read leaves its reason on the
// finding and does not use up one of the two. A finding after the second chart
// says only two are made.
func attachWatchCharts(ctx context.Context, findings []watchFinding, src watchChartSources, now time.Time, loc *time.Location) []watchChart {
	var charts []watchChart
	for i := range findings {
		if findings[i].chart == nil {
			continue
		}
		if len(charts) >= watchChartMaxCharts {
			findings[i].ChartProblem = watchChartCapProblem
			continue
		}
		c, problem := resolveWatchChart(ctx, findings[i].chart, src, now, loc)
		if problem != "" {
			findings[i].ChartProblem = problem
			log.Printf("watches: finding %d has no chart: %s", i, problem)
			continue
		}
		c.FindingIndex = i
		charts = append(charts, c)
	}
	return charts
}

// productionWatchChartSources reads history the way the report's own figures
// are read: the barometer from the live ring buffer, every other path from
// InfluxDB through get_path_history's query. With no InfluxDB the path reader
// says so; it never draws from anything else.
func productionWatchChartSources(settingsPath string) watchChartSources {
	d := assistantProductionToolDeps(settingsPath)
	return watchChartSources{
		Barometer: func(since time.Time) []telemetryPoint { return barometerHistory.since(since) },
		Path: func(ctx context.Context, path string, from, to time.Time) ([]telemetryPoint, string, error) {
			return watchPathHistory(ctx, d, path, from, to)
		},
	}
}

// watchPathHistory is one path's bucket means over a window, unrounded. The
// unit it returns is set only for an angle, which comes back in degrees.
func watchPathHistory(ctx context.Context, d assistantToolDeps, path string, from, to time.Time) ([]telemetryPoint, string, error) {
	if d.influxPathHistoryStat == nil || d.influxPathHistoryFirstLast == nil || d.vesselState == nil {
		return nil, "", errors.New("InfluxDB history is not available on this install")
	}
	args, err := json.Marshal(assistantGetPathHistoryArgs{Path: path, Start: from.UTC().Format(time.RFC3339), End: to.UTC().Format(time.RFC3339)})
	if err != nil {
		return nil, "", err
	}
	hp, _, err := d.pathHistory(ctx, args, false)
	if err != nil {
		return nil, "", err
	}
	var pts []telemetryPoint
	for _, b := range hp.Buckets {
		if b.Mean == nil {
			continue
		}
		t, err := time.Parse(time.RFC3339, b.T)
		if err != nil {
			return nil, "", fmt.Errorf("a bucket time is unreadable: %w", err)
		}
		pts = append(pts, telemetryPoint{Value: *b.Mean, Timestamp: t})
	}
	return pts, hp.Unit, nil
}

// renderChartsForSend draws each chart for a channel that sends a picture. A
// chart that cannot be drawn does not stop the report: its copy carries the
// reason in Problem, has no picture, and the channel's note says it was left
// out. The report goes without it.
func renderChartsForSend(charts []watchChart, channel string, note func(string)) ([]watchChart, [][]byte) {
	out := make([]watchChart, len(charts))
	pngs := make([][]byte, len(charts))
	for i, c := range charts {
		if c.Problem != "" {
			// Left out before it was queued (an alarm's chart, ADR 0235): there is
			// nothing to draw and the reason is already on it.
			out[i] = c
			continue
		}
		data, err := renderWatchChartPNG(c)
		if err != nil {
			c.Problem = fmt.Sprintf("the chart %q could not be drawn: %s", c.Title, firstErrorLine(err))
			log.Printf("watch report: %s: %s, so it is sent without it", channel, c.Problem)
			if note != nil {
				note(channel + ": sent without the chart (" + c.Problem + ")")
			}
		} else {
			pngs[i] = data
		}
		out[i] = c
	}
	return out, pngs
}
