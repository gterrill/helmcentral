package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
)

const (
	chartMaxLabelLen = 100
	// chartReadConcurrency bounds the history and source reads of one chart.
	chartReadConcurrency = 8
	chartInstruction     = "A chart card is attached; describe what it shows and what changed — do not list the numbers as a table."
)

type telemetryChartPayload struct {
	Title    string                 `json:"title"`
	View     string                 `json:"view"`
	XY       *telemetryChartXY      `json:"xy"`
	Timezone string                 `json:"timezone"`
	Series   []telemetryChartSeries `json:"series"`
	Range    telemetryChartRange    `json:"range"`
	Compare  []telemetryChartCmp    `json:"compare"`
}

type telemetryChartXY struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type telemetryChartSeries struct {
	Path   string  `json:"path"`
	Source *string `json:"source"`
	Label  string  `json:"label"`
	Unit   string  `json:"unit"`
	SIUnit string  `json:"si_unit"`
	Axis   string  `json:"axis"`
}

type telemetryChartRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type telemetryChartCmp struct {
	Label string `json:"label"`
	From  string `json:"from"`
	To    string `json:"to"`
}

type chartModelResult struct {
	Title       string             `json:"title"`
	View        string             `json:"view"`
	Series      []chartModelSeries `json:"series"`
	XY          *chartModelXY      `json:"xy,omitempty"`
	Instruction string             `json:"instruction"`

	dropped int
}

type chartModelSeries struct {
	Label  string            `json:"label"`
	Unit   string            `json:"unit"`
	Ranges []chartModelRange `json:"ranges"`
}

type chartModelRange struct {
	Range   string   `json:"range"`
	Min     *float64 `json:"min,omitempty"`
	Mean    *float64 `json:"mean,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Buckets int      `json:"buckets"`
}

type chartModelXY struct {
	X      string              `json:"x"`
	Y      string              `json:"y"`
	Ranges []chartModelXYRange `json:"ranges"`
}

type chartModelXYRange struct {
	Range   string          `json:"range"`
	Buckets int             `json:"buckets"`
	Paired  int             `json:"paired"`
	Bins    []chartModelBin `json:"bins"`
}

type chartModelBin struct {
	XFrom float64 `json:"x_from"`
	XTo   float64 `json:"x_to"`
	N     int     `json:"n"`
	MeanY float64 `json:"mean_y"`
}

// shrink is capToolResultJSON's trim: first each series' min and max per
// range, last series first, then the bins of each xy range, last range first.
// The means, the pairing counts and the instruction are never dropped.
func (m *chartModelResult) shrink() bool {
	for m.dropped < m.dropSteps() {
		i := m.dropped
		m.dropped++
		if changed := m.dropStep(i); changed {
			return true
		}
	}
	return false
}

func (m *chartModelResult) rangeCount() int {
	if len(m.Series) == 0 {
		return 0
	}
	return len(m.Series[0].Ranges)
}

func (m *chartModelResult) dropSteps() int {
	n := len(m.Series) * m.rangeCount()
	if m.XY != nil {
		n += len(m.XY.Ranges)
	}
	return n
}

func (m *chartModelResult) dropStep(i int) bool {
	rc := m.rangeCount()
	if sr := len(m.Series) * rc; i < sr {
		s := &m.Series[len(m.Series)-1-i/rc]
		r := &s.Ranges[rc-1-i%rc]
		changed := r.Min != nil || r.Max != nil
		r.Min, r.Max = nil, nil
		return changed
	} else if m.XY != nil {
		r := &m.XY.Ranges[len(m.XY.Ranges)-1-(i-sr)]
		changed := len(r.Bins) > 0
		r.Bins = nil
		return changed
	}
	return false
}

type chartSeriesArg struct {
	Path   string `json:"path"`
	Role   string `json:"role"`
	Engine string `json:"engine"`
	Source string `json:"source"`
	Label  string `json:"label"`
}

type chartCompareArg struct {
	Label string `json:"label"`
	From  string `json:"from"`
	To    string `json:"to"`
}

type chartArgs struct {
	Series  []chartSeriesArg  `json:"series"`
	From    string            `json:"from"`
	To      string            `json:"to"`
	Compare []chartCompareArg `json:"compare"`
	View    string            `json:"view"`
	X       *int              `json:"x"`
	Y       *int              `json:"y"`
	Title   string            `json:"title"`
}

const chartRolesHint = "Settings → SignalK → Roles"

var chartRoles = []string{
	"house_bank_soc", "house_bank_current", "house_bank_voltage",
	"port_alternator_current", "starboard_alternator_current",
	"engine_rpm", "engine_load", "engine_coolant_temperature",
}

// chartRoleLabels are the default labels, and the engine roles' suffixes.
var chartRoleLabels = map[string]string{
	"house_bank_soc":               "House bank state of charge",
	"house_bank_current":           "House bank current",
	"house_bank_voltage":           "House bank voltage",
	"port_alternator_current":      "Port alternator current",
	"starboard_alternator_current": "Starboard alternator current",
	"engine_rpm":                   "RPM",
	"engine_load":                  "load",
	"engine_coolant_temperature":   "coolant temperature",
}

var chartEngineLeaves = map[string]string{
	"engine_rpm": "revolutions", "engine_load": "engineLoad", "engine_coolant_temperature": "temperature",
}

// resolveChartRole turns a role into the logged path and default label,
// through the vessel view. An unassigned role is an error naming it.
func resolveChartRole(v vesselView, a chartSeriesArg) (path, label string, err error) {
	unassigned := func(what string) error {
		return fmt.Errorf("show_telemetry_chart: role %q is not assigned: %s. Choose it in %s", a.Role, what, chartRolesHint)
	}
	switch a.Role {
	case "house_bank_soc", "house_bank_current", "house_bank_voltage":
		bank := v.houseBankPath()
		if bank == "" {
			return "", "", unassigned("no house bank is set")
		}
		leaf := map[string]string{"house_bank_soc": ".capacity.stateOfCharge", "house_bank_current": ".current", "house_bank_voltage": ".voltage"}[a.Role]
		return bank + leaf, chartRoleLabels[a.Role], nil
	case "port_alternator_current":
		if v.Alternators.PortInstance == "" {
			return "", "", unassigned("no port alternator is set")
		}
		return "electrical.alternator." + v.Alternators.PortInstance + ".current", chartRoleLabels[a.Role], nil
	case "starboard_alternator_current":
		if v.Alternators.StarboardInstance == "" {
			return "", "", unassigned("no starboard alternator is set")
		}
		return "electrical.alternator." + v.Alternators.StarboardInstance + ".current", chartRoleLabels[a.Role], nil
	case "engine_rpm", "engine_load", "engine_coolant_temperature":
		if len(v.Engines) == 0 {
			return "", "", unassigned("no engines are set")
		}
		want := strings.TrimSpace(a.Engine)
		var names []string
		for _, e := range v.Engines {
			name := strings.TrimSpace(e.Name)
			if name == "" {
				name = e.Instance
			}
			names = append(names, name)
			if want != "" && (strings.EqualFold(want, e.Instance) || strings.EqualFold(want, strings.TrimSpace(e.Name))) {
				if e.Instance == "" {
					return "", "", fmt.Errorf("show_telemetry_chart: engine %q has no instance assigned; set it in %s", name, chartRolesHint)
				}
				who := name
				if !strings.Contains(strings.ToLower(who), "engine") {
					who += " engine"
				}
				return "propulsion." + e.Instance + "." + chartEngineLeaves[a.Role], who + " " + chartRoleLabels[a.Role], nil
			}
		}
		if want == "" {
			return "", "", fmt.Errorf("show_telemetry_chart: role %q needs engine, one of: %s", a.Role, strings.Join(names, ", "))
		}
		return "", "", fmt.Errorf("show_telemetry_chart: engine %q is not one of this vessel's engines: %s", want, strings.Join(names, ", "))
	}
	return "", "", fmt.Errorf("show_telemetry_chart: unknown role %q; roles are %s", a.Role, strings.Join(chartRoles, ", "))
}

// chartPathWords is the label for a raw path: its last two segments as words.
func chartPathWords(path string) string {
	segs := strings.Split(path, ".")
	if len(segs) > 2 {
		segs = segs[len(segs)-2:]
	}
	var words []string
	for _, seg := range segs {
		var b strings.Builder
		for i, r := range seg {
			if i > 0 && unicode.IsUpper(r) {
				b.WriteByte(' ')
			}
			b.WriteRune(unicode.ToLower(r))
		}
		words = append(words, b.String())
	}
	out := strings.Join(words, " ")
	if out == "" {
		return path
	}
	return strings.ToUpper(out[:1]) + out[1:]
}

// chartRange is one drawn span, with the label the summary and errors use.
type chartRange struct {
	label    string
	from, to time.Time
}

func (d assistantToolDeps) parseChartRange(what, fromS, toS string) (chartRange, error) {
	from, err := time.Parse(time.RFC3339, strings.TrimSpace(fromS))
	if err != nil {
		return chartRange{}, fmt.Errorf("show_telemetry_chart: %s from must be RFC3339 (e.g. 2026-10-12T00:00:00Z)", what)
	}
	to, err := time.Parse(time.RFC3339, strings.TrimSpace(toS))
	if err != nil {
		return chartRange{}, fmt.Errorf("show_telemetry_chart: %s to must be RFC3339 (e.g. 2026-10-19T00:00:00Z)", what)
	}
	if !to.After(from) {
		return chartRange{}, fmt.Errorf("show_telemetry_chart: %s to must be after from", what)
	}
	if to.Sub(from) > telemetrySeriesMaxSpan {
		return chartRange{}, fmt.Errorf("show_telemetry_chart: %s must not exceed %d days", what, int(telemetrySeriesMaxSpan/(24*time.Hour)))
	}
	now := d.now()
	if to.After(now.Add(telemetrySeriesMaxFuture)) {
		return chartRange{}, fmt.Errorf("show_telemetry_chart: %s to must not be in the future", what)
	}
	// Nothing has happened after now: the live edge is not a sensor gap.
	if to.After(now) {
		to = now
	}
	if !to.After(from) {
		return chartRange{}, fmt.Errorf("show_telemetry_chart: %s from must not be in the future", what)
	}
	return chartRange{from: from, to: to}, nil
}

func checkChartText(what, s string, max int) error {
	if len([]rune(s)) > max {
		return fmt.Errorf("show_telemetry_chart: %s must be at most %d characters", what, max)
	}
	return nil
}

// chartPoint is one bucket of one series in display units.
type chartPoint struct {
	t              int64
	min, mean, max *float64
}

// withUnit appends " (unit)" to a label; a series with no unit has none to add.
func withUnit(label, unit string) string {
	if unit == "" {
		return label
	}
	return label + " (" + unit + ")"
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func ptr2(v float64) *float64 { r := round2(v); return &r }

func (d assistantToolDeps) executeShowTelemetryChart(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var args chartArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse show_telemetry_chart arguments: %w", err)
	}
	if d.cards == nil {
		return "", fmt.Errorf("show_telemetry_chart: cards are not available")
	}
	if d.influxPathHistoryStat == nil || d.influxPathSources == nil || d.displayUnits == nil || d.siUnit == nil {
		return "", fmt.Errorf("show_telemetry_chart: history is not available (needs InfluxDB)")
	}

	// Shape first: nothing is read until the request is one chart.
	if n := len(args.Series); n < 1 || n > telemetrySeriesMaxPaths {
		return "", fmt.Errorf("show_telemetry_chart: series needs 1 to %d entries, got %d", telemetrySeriesMaxPaths, n)
	}
	view := strings.TrimSpace(args.View)
	switch view {
	case "":
		view = "time"
	case "time", "xy":
	default:
		return "", fmt.Errorf("show_telemetry_chart: view must be \"time\" or \"xy\", got %q", view)
	}
	var xy *telemetryChartXY
	if view == "xy" {
		if args.X == nil || args.Y == nil {
			return "", fmt.Errorf("show_telemetry_chart: an xy view needs x and y, the indices of the series for each axis")
		}
		n := len(args.Series)
		if *args.X < 0 || *args.X >= n || *args.Y < 0 || *args.Y >= n || *args.X == *args.Y {
			return "", fmt.Errorf("show_telemetry_chart: x and y must be different indices into series (0 to %d)", n-1)
		}
		xy = &telemetryChartXY{X: *args.X, Y: *args.Y}
	} else if args.X != nil || args.Y != nil {
		return "", fmt.Errorf("show_telemetry_chart: x and y are only for view \"xy\"")
	}
	if err := checkChartText("title", args.Title, chartMaxLabelLen); err != nil {
		return "", err
	}
	if len(args.Compare) > 3 {
		return "", fmt.Errorf("show_telemetry_chart: compare takes at most 3 ranges, got %d", len(args.Compare))
	}

	main, err := d.parseChartRange("range", args.From, args.To)
	if err != nil {
		return "", err
	}
	drawn := []chartRange{{label: "selected range", from: main.from, to: main.to}}
	var compare []telemetryChartCmp
	if len(args.Compare) > 0 {
		drawn = nil
		for _, c := range args.Compare {
			label := strings.TrimSpace(c.Label)
			if label == "" {
				return "", fmt.Errorf("show_telemetry_chart: every compare range needs a label")
			}
			if err := checkChartText("compare label", label, chartMaxLabelLen); err != nil {
				return "", err
			}
			r, err := d.parseChartRange("compare range "+label, c.From, c.To)
			if err != nil {
				return "", err
			}
			r.label = label
			drawn = append(drawn, r)
		}
	}

	// Resolve every series to a path, label and source.
	var vessel vesselView
	vesselRead := false
	type resolved struct {
		path, source, label, unit, siUnit, axis string
		convert                                 func(float64) float64
	}
	series := make([]resolved, len(args.Series))
	for i, a := range args.Series {
		path, role := strings.TrimSpace(a.Path), strings.TrimSpace(a.Role)
		if (path == "") == (role == "") {
			return "", fmt.Errorf("show_telemetry_chart: series %d needs exactly one of path or role", i)
		}
		a.Role = role
		label := ""
		if role != "" {
			if !vesselRead {
				if d.vesselView == nil {
					return "", fmt.Errorf("show_telemetry_chart: the vessel's roles are not available")
				}
				v, err := d.vesselView()
				if err != nil {
					return "", fmt.Errorf("show_telemetry_chart: vessel settings are unreadable: %w", err)
				}
				vessel, vesselRead = v, true
			}
			path, label, err = resolveChartRole(vessel, a)
			if err != nil {
				return "", err
			}
		} else {
			label = chartPathWords(path)
		}
		// Every path, typed or resolved from a role, is interpolated into Flux.
		if len(path) > gaugePathMaxLen {
			return "", fmt.Errorf("show_telemetry_chart: path is too long")
		}
		if containsFluxInterpolationSyntax(path) {
			return "", fmt.Errorf("show_telemetry_chart: path must not contain '$' or '{'")
		}
		source := strings.TrimSpace(a.Source)
		if len(source) > gaugePathMaxLen || containsFluxInterpolationSyntax(source) {
			return "", fmt.Errorf("show_telemetry_chart: source must not contain '$' or '{'")
		}
		if l := strings.TrimSpace(a.Label); l != "" {
			label = l
		}
		if err := checkChartText("label", label, chartMaxLabelLen); err != nil {
			return "", err
		}
		series[i] = resolved{path: path, source: source, label: label}
	}

	// Units: the operator's display unit for the unit the boat declares; a
	// path with none declared stays as stored and is labelled so. Axes follow
	// the unit.
	system, err := d.displayUnits()
	if err != nil {
		return "", fmt.Errorf("show_telemetry_chart: units setting is unreadable: %w", err)
	}
	// Axes follow the unit. Each series with no declared unit is a group of
	// its own: two unreported units are not known to be the same.
	axisOf := map[string]string{}
	for i := range series {
		s := &series[i]
		s.siUnit = d.siUnit(s.path)
		unit, conv, err := chartDisplayFor(s.path, s.siUnit, system)
		if err != nil {
			return "", fmt.Errorf("show_telemetry_chart: %w", err)
		}
		s.unit, s.convert = unit, conv
		if s.siUnit == "" {
			s.label += " (unit not reported)"
			if err := checkChartText("label", s.label, chartMaxLabelLen); err != nil {
				return "", fmt.Errorf("%w (including the \" (unit not reported)\" note this series needs)", err)
			}
		}
		group := s.unit
		if group == "" {
			group = fmt.Sprintf("\x00unreported %d", i)
		}
		axis, ok := axisOf[group]
		if !ok {
			switch len(axisOf) {
			case 0:
				axis = "left"
			case 1:
				axis = "right"
			default:
				what := fmt.Sprintf("%q", s.unit)
				if s.unit == "" {
					what = "a series with no reported unit"
				}
				return "", fmt.Errorf("show_telemetry_chart: a chart shows at most two units (a left and a right axis); %s is a third. Chart those series separately", what)
			}
			axisOf[group] = axis
		}
		s.axis = axis
	}

	// A path logged by two devices is never averaged: choose the device. The
	// check is per drawn range, so a device that logged only between two
	// compared ranges is not in the way.
	type srcJob struct{ s, r int }
	var srcJobs []srcJob
	for i, s := range series {
		if s.source != "" {
			continue
		}
		for j := range drawn {
			srcJobs = append(srcJobs, srcJob{i, j})
		}
	}
	srcFound := make([][]string, len(srcJobs))
	srcErrs := runBounded(ctx, len(srcJobs), chartReadConcurrency, func(k int) error {
		job := srcJobs[k]
		srcs, err := ambiguousSources(ctx, d.influxPathSources, series[job.s].path, drawn[job.r].from, drawn[job.r].to)
		srcFound[k] = srcs
		if err != nil {
			return fmt.Errorf("show_telemetry_chart: reading the sources of %s: %w", series[job.s].label, err)
		}
		return nil
	})
	if err := ctx.Err(); err != nil {
		return "", err
	}
	for k, job := range srcJobs {
		if srcErrs[k] != nil {
			return "", srcErrs[k]
		}
		if srcs := srcFound[k]; srcs != nil {
			return "", fmt.Errorf("show_telemetry_chart: %s was logged by more than one source (%s) in %s; give source for it, one of those. Averaging devices would chart a figure neither reported", series[job.s].label, strings.Join(srcs, ", "), drawn[job.r].label)
		}
	}

	// History: every series in every drawn range, read concurrently. Results
	// land by index and the first error is the first by series then range, so
	// the outcome does not depend on which read finished first.
	io := pathHistoryIO{stat: d.influxPathHistoryStat, firstLast: d.influxPathHistoryFirstLast}
	data := make([][][]chartPoint, len(series)) // [series][range]
	for i := range data {
		data[i] = make([][]chartPoint, len(drawn))
	}
	histErrs := runBounded(ctx, len(series)*len(drawn), chartReadConcurrency, func(k int) error {
		i, j := k/len(drawn), k%len(drawn)
		s, r := series[i], drawn[j]
		h, err := queryPathHistoryBuckets(ctx, io, s.path, s.source, r.from, r.to, []string{"min", "mean", "max"}, false, telemetrySeriesBucketWidth)
		if err != nil {
			return fmt.Errorf("show_telemetry_chart: %w", err)
		}
		var pts []chartPoint
		for _, m := range h.Merged {
			if m.Mean == nil {
				continue
			}
			cp := chartPoint{t: m.T.Unix()}
			mean := s.convert(*m.Mean)
			cp.mean = &mean
			if m.Min != nil {
				v := s.convert(*m.Min)
				cp.min = &v
			}
			if m.Max != nil {
				v := s.convert(*m.Max)
				cp.max = &v
			}
			pts = append(pts, cp)
		}
		if len(pts) == 0 {
			return fmt.Errorf("show_telemetry_chart: %s has no logged data in %s (%s to %s)", s.label, r.label,
				r.from.UTC().Format(time.RFC3339), r.to.UTC().Format(time.RFC3339))
		}
		data[i][j] = pts
		return nil
	})
	if err := ctx.Err(); err != nil {
		return "", err
	}
	for _, err := range histErrs {
		if err != nil {
			return "", err
		}
	}

	loc, tz := time.UTC, "UTC"
	if state, verr := d.vesselState(); verr == nil && hasUsableVesselPosition(state.Latitude, state.Longitude) {
		loc, tz = vesselLocalLocation(state.Longitude), vesselLocalTimezoneName(state.Longitude)
	}
	at := func(t time.Time) string { return t.In(loc).Format(time.RFC3339) }

	// The model's summary.
	model := &chartModelResult{View: view, Instruction: chartInstruction, Series: make([]chartModelSeries, len(series))}
	for i, s := range series {
		ms := chartModelSeries{Label: s.label, Unit: s.unit}
		for j, r := range drawn {
			mr := chartModelRange{Range: r.label, Buckets: len(data[i][j])}
			var sum float64
			var lo, hi *float64
			for _, p := range data[i][j] {
				sum += *p.mean
				if p.min != nil && (lo == nil || *p.min < *lo) {
					lo = p.min
				}
				if p.max != nil && (hi == nil || *p.max > *hi) {
					hi = p.max
				}
			}
			mr.Mean = ptr2(sum / float64(len(data[i][j])))
			if lo != nil {
				mr.Min = ptr2(*lo)
			}
			if hi != nil {
				mr.Max = ptr2(*hi)
			}
			ms.Ranges = append(ms.Ranges, mr)
		}
		model.Series[i] = ms
	}
	if xy != nil {
		xs, ys := series[xy.X], series[xy.Y]
		type pair struct{ x, y float64 }
		pairs := make([][]pair, len(drawn))
		buckets := make([]int, len(drawn))
		minX, maxX, any := math.Inf(1), math.Inf(-1), false
		for j := range drawn {
			yAt := map[int64]float64{}
			for _, p := range data[xy.Y][j] {
				yAt[p.t] = *p.mean
			}
			seen := map[int64]bool{}
			for _, p := range data[xy.X][j] {
				seen[p.t] = true
				if y, ok := yAt[p.t]; ok {
					pairs[j] = append(pairs[j], pair{*p.mean, y})
					minX, maxX, any = math.Min(minX, *p.mean), math.Max(maxX, *p.mean), true
				}
			}
			for t := range yAt {
				seen[t] = true
			}
			buckets[j] = len(seen)
			if len(pairs[j]) == 0 {
				return "", fmt.Errorf("show_telemetry_chart: %s and %s have no bucket in common in %s, so there is nothing to plot against each other", xs.label, ys.label, drawn[j].label)
			}
		}
		_ = any
		const nBins = 8
		width := (maxX - minX) / nBins
		mxy := &chartModelXY{X: withUnit(xs.label, xs.unit), Y: withUnit(ys.label, ys.unit)}
		for j, r := range drawn {
			var n [nBins]int
			var sum [nBins]float64
			for _, p := range pairs[j] {
				k := 0
				if width > 0 {
					k = int((p.x - minX) / width)
				}
				if k >= nBins {
					k = nBins - 1
				}
				n[k]++
				sum[k] += p.y
			}
			xr := chartModelXYRange{Range: r.label, Buckets: buckets[j], Paired: len(pairs[j]), Bins: []chartModelBin{}}
			for k := 0; k < nBins; k++ {
				if n[k] == 0 {
					continue
				}
				xr.Bins = append(xr.Bins, chartModelBin{
					XFrom: round2(minX + width*float64(k)), XTo: round2(minX + width*float64(k+1)),
					N: n[k], MeanY: round2(sum[k] / float64(n[k])),
				})
			}
			mxy.Ranges = append(mxy.Ranges, xr)
		}
		model.XY = mxy
	}

	// The card.
	title := strings.TrimSpace(args.Title)
	if title == "" {
		if xy != nil {
			title = series[xy.Y].label + " against " + series[xy.X].label
		} else {
			var ls []string
			for _, s := range series {
				ls = append(ls, s.label)
			}
			title = strings.Join(ls, " and ")
		}
	}
	model.Title = title
	payload := telemetryChartPayload{
		Title: title, View: view, XY: xy, Timezone: tz,
		Series:  make([]telemetryChartSeries, len(series)),
		Range:   telemetryChartRange{From: at(main.from), To: at(main.to)},
		Compare: []telemetryChartCmp{},
	}
	for i, s := range series {
		cs := telemetryChartSeries{Path: s.path, Label: s.label, Unit: s.unit, SIUnit: s.siUnit, Axis: s.axis}
		if s.source != "" {
			src := s.source
			cs.Source = &src
		}
		payload.Series[i] = cs
	}
	if len(args.Compare) > 0 {
		for _, r := range drawn {
			compare = append(compare, telemetryChartCmp{Label: r.label, From: at(r.from), To: at(r.to)})
		}
		payload.Compare = compare
	}
	card, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("show_telemetry_chart card: %w", err)
	}
	text, err := capToolResultJSON(model, model.shrink)
	if err != nil {
		return "", err
	}
	d.cards.put(text, mateCard{Kind: "telemetry_chart", Payload: card})
	return text, nil
}

var mateToolShowTelemetryChart = mateTool{
	name: "show_telemetry_chart",
	function: openRouterFunctionDef{
		Description: "Draw a chart card of recorded vessel telemetry: one to four series over time, or one series " +
			"against another (amps against state of charge, engine RPM against load) as points, one per time bucket. " +
			"Use it for \"show me\", \"chart\", \"compare\" and \"is it getting worse\" questions about house bank, " +
			"alternator and engine figures, or any logged path. Give a role for the boat's own equipment, or a path " +
			"for anything else you know is logged (find it with get_last_recorded). Values are in the operator's " +
			"display units. A path logged by two devices must be given a source; the tool names them if you do not. " +
			"Comparison ranges (before and after a change) must come from the operator, who gives you the dates and " +
			"the labels; never guess a date or search for one. Every series must have data in every range drawn, or " +
			"the tool says which does not. A card is attached to your reply, so describe what it shows and what " +
			"changed from the summary; do not list the numbers as a table.",
		Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"series": {
							"type": "array", "minItems": 1, "maxItems": 4,
							"description": "What to chart. Each entry has exactly one of role or path.",
							"items": {
								"type": "object",
								"properties": {
									"role": {
										"type": "string",
										"enum": ["house_bank_soc", "house_bank_current", "house_bank_voltage", "port_alternator_current", "starboard_alternator_current", "engine_rpm", "engine_load", "engine_coolant_temperature"],
										"description": "The boat's own equipment, resolved from the operator's roles. The engine roles also need engine. An unassigned role is an error."
									},
									"engine": {"type": "string", "description": "For the engine roles only: the engine's name or id, as the vessel lists it (for example Port). The error lists the valid ones."},
									"path": {"type": "string", "description": "A logged SignalK path, when no role fits."},
									"source": {"type": "string", "description": "The device (source label) that logged it. Required when more than one device logged the path; the error names them."},
									"label": {"type": "string", "description": "Optional short plain-words name for the legend. Defaults to the role or path in words."}
								}
							}
						},
						"from": {"type": "string", "description": "Start of the overall range, RFC3339 (e.g. 2026-10-12T00:00:00+10:00). At most 60 days before to."},
						"to": {"type": "string", "description": "End of the overall range, RFC3339. Clamped to now."},
						"compare": {
							"type": "array", "maxItems": 3,
							"description": "Optional labelled ranges to draw side by side, such as before and after a change. The operator must give you these dates and labels; never guess or search for them. When given, the chart draws these and the overall range is the span around them.",
							"items": {
								"type": "object",
								"properties": {
									"label": {"type": "string", "description": "What the range is, in the operator's words (for example Before regulator change)."},
									"from": {"type": "string", "description": "RFC3339."},
									"to": {"type": "string", "description": "RFC3339."}
								},
								"required": ["label", "from", "to"]
							}
						},
						"view": {"type": "string", "enum": ["time", "xy"], "description": "time (default) draws each series against time; xy draws series y against series x."},
						"x": {"type": "integer", "minimum": 0, "description": "For xy: index in series of the x axis."},
						"y": {"type": "integer", "minimum": 0, "description": "For xy: index in series of the y axis. Different from x."},
						"title": {"type": "string", "description": "Optional short title in plain words, no paths."}
					},
					"required": ["series", "from", "to"]
				}`),
	},
	execute: assistantToolDeps.executeShowTelemetryChart,
	describe: func(args json.RawMessage) string {
		return "Drawing a chart of the logged readings…"
	},
}

// buildInfluxPathSourcesFlux lists the distinct $source tag values that logged
// a path in [start, stop).
func buildInfluxPathSourcesFlux(bucket, field, path string, start, stop time.Time) (string, error) {
	pathLiteral, err := fluxStringLiteral(path)
	if err != nil {
		return "", fmt.Errorf("path: %w", err)
	}
	bucketLiteral, err := fluxStringLiteral(bucket)
	if err != nil {
		return "", fmt.Errorf("bucket: %w", err)
	}
	fieldLiteral, err := fluxStringLiteral(field)
	if err != nil {
		return "", fmt.Errorf("field: %w", err)
	}
	return fmt.Sprintf(
		`from(bucket: %s) |> range(start: time(v: %q), stop: time(v: %q)) |> filter(fn: (r) => r._measurement == %s and r._field == %s) |> keep(columns: ["source"]) |> group() |> distinct(column: "source")`,
		bucketLiteral, start.UTC().Format(time.RFC3339), stop.UTC().Format(time.RFC3339), pathLiteral, fieldLiteral,
	), nil
}

// queryInfluxPathSources is show_telemetry_chart's source check.
func queryInfluxPathSources(ctx context.Context, path string, start, stop time.Time) ([]string, error) {
	client, org, bucket, ok := newInfluxClient()
	if !ok {
		return nil, fmt.Errorf("influxdb is not configured")
	}
	field := trimEnvValue(getEnv("INFLUX_DEPTH_FIELD", "value"))
	flux, err := buildInfluxPathSourcesFlux(bucket, field, path, start, stop)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	result, err := client.QueryAPI(org).Query(ctx, flux)
	if err != nil {
		return nil, err
	}
	defer result.Close()
	var out []string
	for result.Next() {
		if s, ok := result.Record().Value().(string); ok && s != "" {
			out = append(out, s)
		}
	}
	if result.Err() != nil {
		return nil, result.Err()
	}
	return out, nil
}

// displayUnitsFromSettingsFile reads the Settings units choice fresh on every
// call: "imperial", or "metric" when unset, as the dashboard treats it.
func displayUnitsFromSettingsFile(settingsPath string) func() (string, error) {
	return func() (string, error) {
		settings, err := readSettings(settingsPath)
		if err != nil {
			return "", err
		}
		if u := strings.ToLower(strings.TrimSpace(coerceString(settings["units"]))); u == "imperial" {
			return u, nil
		}
		return "metric", nil
	}
}
