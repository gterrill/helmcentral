package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// show_wind_rose (ADR 0221): where the wind has come from over a range, as a
// card. The server counts the logged true wind into direction sectors and
// speed bands. The model reads a short summary of the same counts and never
// writes the card.

const (
	// windRoseMaxBuckets bounds the samples a rose is counted from, so a 60-day
	// rose is hourly samples and a day is minutes.
	windRoseMaxBuckets = 2000
	// windRoseCalmBelow is the speed, in knots, under which the wind is calm.
	// A calm has no direction worth plotting, so it is counted apart.
	windRoseCalmBelow = 1.0
	windRoseMaxBins   = 8
	windRoseInstr     = "A wind rose card is attached; describe where the wind came from and how strong it was, and do not list the counts as a table."
)

var windRoseDefaultEdges = []float64{5, 10, 15, 20, 25}

type windRoseBin struct {
	From float64  `json:"from"`
	To   *float64 `json:"to"` // null for the top, open-ended band
}

type windRoseStrongest struct {
	Speed float64 `json:"speed"`
	At    string  `json:"at"`
}

// windRosePayload is the stored card: a snapshot of the counts, not a
// reference. The counts are small (sectors by bands) and the model's prose
// was written about exactly these figures.
type windRosePayload struct {
	Title     string              `json:"title"`
	Timezone  string              `json:"timezone"`
	Range     telemetryChartRange `json:"range"`
	Reference string              `json:"reference"` // "true": bearings the wind blows from, in °T
	Sectors   int                 `json:"sectors"`
	SpeedUnit string              `json:"speed_unit"`
	Bins      []windRoseBin       `json:"bins"`
	// Counts[s][b] is the number of samples from sector s, centred on s*360/n
	// degrees (sector 0 is north), in speed band b.
	Counts        [][]int            `json:"counts"`
	Samples       int                `json:"samples"`
	Calm          int                `json:"calm"`
	CalmBelow     float64            `json:"calm_below"`
	Variable      int                `json:"variable"`
	Unpaired      int                `json:"unpaired"`
	BucketMinutes int                `json:"bucket_minutes"`
	MeanSpeed     *float64           `json:"mean_speed"`
	Strongest     *windRoseStrongest `json:"strongest"`
}

// windRoseSample is one time bucket of the wind: its mean direction (nil when
// the samples cancelled out) and its mean speed, in display units.
type windRoseSample struct {
	Dir   *float64
	Speed float64
}

type windRoseTally struct {
	Counts   [][]int
	Samples  int
	Calm     int
	Variable int
}

// windRoseSectorOf is the sector a bearing falls in. Sector 0 is centred on
// north and covers [-half, +half); a bearing exactly on an edge belongs to the
// sector clockwise of it.
func windRoseSectorOf(deg float64, sectors int) int {
	width := 360 / float64(sectors)
	d := math.Mod(deg, 360)
	if d < 0 {
		d += 360
	}
	return int(math.Floor((d+width/2)/width)) % sectors
}

// windRoseBinOf is the speed band of a speed: bands are [0,e0), [e0,e1) ...
// [eN,∞).
func windRoseBinOf(speed float64, edges []float64) int {
	return sort.Search(len(edges), func(i int) bool { return speed < edges[i] })
}

// buildWindRose counts samples into sectors and bands. A calm sample is counted
// apart, whatever its direction, and a sample with wind but no mean direction is
// counted as variable. Neither is put in a sector.
func buildWindRose(samples []windRoseSample, sectors int, edges []float64, calmBelow float64) windRoseTally {
	t := windRoseTally{Counts: make([][]int, sectors)}
	for i := range t.Counts {
		t.Counts[i] = make([]int, len(edges)+1)
	}
	for _, s := range samples {
		t.Samples++
		switch {
		case s.Speed < calmBelow:
			t.Calm++
		case s.Dir == nil:
			t.Variable++
		default:
			t.Counts[windRoseSectorOf(*s.Dir, sectors)][windRoseBinOf(s.Speed, edges)]++
		}
	}
	return t
}

// windRoseBucketWidth is the finest allowlisted width that keeps a span at or
// under windRoseMaxBuckets samples.
func windRoseBucketWidth(span time.Duration) (string, time.Duration) {
	for _, w := range telemetrySeriesBucketWidths {
		if int((span+w.width-1)/w.width) <= windRoseMaxBuckets {
			return w.label, w.width
		}
	}
	last := telemetrySeriesBucketWidths[len(telemetrySeriesBucketWidths)-1]
	return last.label, last.width
}

type windRoseArgs struct {
	From            string    `json:"from"`
	To              string    `json:"to"`
	Sectors         *int      `json:"sectors"`
	SpeedBins       []float64 `json:"speed_bins"`
	DirectionSource string    `json:"direction_source"`
	SpeedSource     string    `json:"speed_source"`
	Title           string    `json:"title"`
}

type windRoseModelSector struct {
	Compass string  `json:"compass"`
	Degrees float64 `json:"degrees"`
	Percent float64 `json:"percent"`
	// MeanSpeed is the mean wind speed in this sector, in the card's unit.
	MeanSpeed float64 `json:"mean_speed"`
}

type windRoseModelBand struct {
	Label   string  `json:"label"`
	Percent float64 `json:"percent"`
}

type windRoseModelResult struct {
	Title           string                `json:"title"`
	Range           string                `json:"range"`
	Unit            string                `json:"unit"`
	Samples         int                   `json:"samples"`
	BucketMinutes   int                   `json:"bucket_minutes"`
	Sectors         []windRoseModelSector `json:"top_sectors"`
	Bands           []windRoseModelBand   `json:"speed_bands"`
	CalmPercent     float64               `json:"calm_percent"`
	VariablePercent float64               `json:"variable_percent,omitempty"`
	MeanSpeed       *float64              `json:"mean_speed,omitempty"`
	StrongestGust   *windRoseStrongest    `json:"strongest_gust,omitempty"`
	Unpaired        int                   `json:"unpaired_buckets,omitempty"`
	Note            string                `json:"note"`
	Instruction     string                `json:"instruction"`
}

func windRoseBandLabel(edges []float64, i int) string {
	f := func(v float64) string { return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", v), "0"), ".") }
	switch {
	case i == 0:
		return "0-" + f(edges[0])
	case i == len(edges):
		return f(edges[i-1]) + "+"
	}
	return f(edges[i-1]) + "-" + f(edges[i])
}

func (d assistantToolDeps) executeShowWindRose(ctx context.Context, raw json.RawMessage) (string, error) {
	const tool = "show_wind_rose"
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var args windRoseArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse %s arguments: %w", tool, err)
	}
	if d.cards == nil {
		return "", fmt.Errorf("%s: cards are not available", tool)
	}
	if d.influxPathHistoryStat == nil || d.influxPathSources == nil || d.displayUnits == nil || d.siUnit == nil {
		return "", fmt.Errorf("%s: history is not available (needs InfluxDB)", tool)
	}

	sectors := 16
	if args.Sectors != nil {
		sectors = *args.Sectors
	}
	if sectors != 8 && sectors != 16 && sectors != 32 {
		return "", fmt.Errorf("%s: sectors must be 8, 16 or 32, got %d", tool, sectors)
	}
	edges := args.SpeedBins
	if len(edges) == 0 {
		edges = windRoseDefaultEdges
	}
	if len(edges) > windRoseMaxBins {
		return "", fmt.Errorf("%s: speed_bins takes at most %d edges, got %d", tool, windRoseMaxBins, len(edges))
	}
	for i, e := range edges {
		if math.IsNaN(e) || math.IsInf(e, 0) || e <= 0 || (i > 0 && e <= edges[i-1]) {
			return "", fmt.Errorf("%s: speed_bins must be positive and strictly increasing, in knots", tool)
		}
	}
	if err := checkChartText("title", args.Title, chartMaxLabelLen); err != nil {
		return "", fmt.Errorf("%s: title must be at most %d characters", tool, chartMaxLabelLen)
	}
	for what, src := range map[string]string{"direction_source": args.DirectionSource, "speed_source": args.SpeedSource} {
		if len(src) > gaugePathMaxLen || containsFluxInterpolationSyntax(src) {
			return "", fmt.Errorf("%s: %s must not contain '$' or '{'", tool, what)
		}
	}
	dirSource, speedSource := strings.TrimSpace(args.DirectionSource), strings.TrimSpace(args.SpeedSource)

	rng, err := d.parseChartRange(tool, "range", args.From, args.To)
	if err != nil {
		return "", err
	}

	// The wind is the true wind the boat logs, and it must declare its units: a
	// direction not known to be radians is not guessed to be, and a speed that
	// is not metres a second cannot be put in knots.
	system, err := d.displayUnits()
	if err != nil {
		return "", fmt.Errorf("%s: units setting is unreadable: %w", tool, err)
	}
	if si := d.siUnit(windDirectionTruePath); si != "rad" {
		return "", fmt.Errorf("%s: the boat does not report a radian unit for the true wind direction (it reports %q), so the rose cannot be drawn truthfully", tool, si)
	}
	speedSI := d.siUnit(windSpeedTruePath)
	if speedSI != "m/s" {
		return "", fmt.Errorf("%s: the true wind speed must be reported in m/s to be shown in knots; the boat reports %q", tool, speedSI)
	}
	unit, convert, err := chartDisplayFor(windSpeedTruePath, speedSI, system)
	if err != nil {
		return "", fmt.Errorf("%s: %w", tool, err)
	}
	if unit != "kn" {
		return "", fmt.Errorf("%s: wind speed bands are in knots; this setting shows %q", tool, unit)
	}

	// Two devices logging a wind are never averaged: the operator chooses one.
	for _, p := range []struct{ what, path, source, arg string }{
		{"true wind direction", windDirectionTruePath, dirSource, "direction_source"},
		{"true wind speed", windSpeedTruePath, speedSource, "speed_source"},
	} {
		if p.source != "" {
			continue
		}
		srcs, err := ambiguousSources(ctx, d.influxPathSources, p.path, rng.from, rng.to)
		if err != nil {
			return "", fmt.Errorf("%s: reading the sources of the %s: %w", tool, p.what, err)
		}
		if srcs != nil {
			return "", fmt.Errorf("%s: the %s was logged by more than one source (%s); give %s, one of those. Averaging devices would chart a wind neither reported", tool, p.what, strings.Join(srcs, ", "), p.arg)
		}
	}

	io := pathHistoryIO{stat: d.influxPathHistoryStat, firstLast: d.influxPathHistoryFirstLast}
	var dir, speed pathHistoryBuckets
	errs := runBounded(ctx, 2, 2, func(i int) error {
		var err error
		if i == 0 {
			dir, err = queryCircularHistoryBuckets(ctx, io, windDirectionTruePath, dirSource, windSpeedTruePath, speedSource, rng.from, rng.to, false, windRoseBucketWidth)
		} else {
			speed, err = queryPathHistoryBuckets(ctx, io, windSpeedTruePath, speedSource, rng.from, rng.to, []string{"mean", "max"}, false, windRoseBucketWidth)
		}
		return err
	})
	if err := ctx.Err(); err != nil {
		return "", err
	}
	for _, err := range errs {
		if err != nil {
			return "", fmt.Errorf("%s: %w", tool, err)
		}
	}
	span := fmt.Sprintf("%s to %s", rng.from.UTC().Format(time.RFC3339), rng.to.UTC().Format(time.RFC3339))
	if !dir.Found {
		return "", fmt.Errorf("%s: there is no logged true wind direction in %s", tool, span)
	}
	if !speed.Found {
		return "", fmt.Errorf("%s: there is no logged true wind speed in %s", tool, span)
	}

	speedAt := map[int64]assistantPathHistoryPoint{}
	for _, p := range speed.Merged {
		if p.Mean != nil {
			speedAt[p.T.Unix()] = p
		}
	}
	var samples []windRoseSample
	var sum float64
	var strongest *windRoseStrongest
	paired := map[int64]bool{}
	for _, p := range dir.Merged {
		sp, ok := speedAt[p.T.Unix()]
		if !ok {
			continue
		}
		paired[p.T.Unix()] = true
		s := windRoseSample{Speed: convert(*sp.Mean)}
		if p.Mean != nil {
			deg := angleDegrees(angleBearing, *p.Mean)
			s.Dir = &deg
		}
		samples = append(samples, s)
		sum += s.Speed
		if sp.Max != nil {
			if g := convert(*sp.Max); strongest == nil || g > strongest.Speed {
				strongest = &windRoseStrongest{Speed: g, At: sp.T.Format(time.RFC3339)}
			}
		}
	}
	if len(samples) == 0 {
		return "", fmt.Errorf("%s: the true wind direction and speed have no time bucket in common in %s, so there is nothing to count", tool, span)
	}
	unpaired := len(dir.Merged) + len(speedAt) - 2*len(samples)

	loc, tz := time.UTC, "UTC"
	if state, verr := d.vesselState(); verr == nil && hasUsableVesselPosition(state.Latitude, state.Longitude) {
		loc, tz = vesselLocalLocation(state.Longitude), vesselLocalTimezoneName(state.Longitude)
	}
	if strongest != nil {
		t, _ := time.Parse(time.RFC3339, strongest.At)
		strongest.At = t.In(loc).Format(time.RFC3339)
		strongest.Speed = round2(strongest.Speed)
	}

	tally := buildWindRose(samples, sectors, edges, windRoseCalmBelow)
	bins := make([]windRoseBin, len(edges)+1)
	for i := range bins {
		if i > 0 {
			bins[i].From = edges[i-1]
		}
		if i < len(edges) {
			e := edges[i]
			bins[i].To = &e
		}
	}
	_, width := windRoseBucketWidth(rng.to.Sub(rng.from))
	mean := round2(sum / float64(len(samples)))

	title := strings.TrimSpace(args.Title)
	if title == "" {
		title = "Where the wind came from, " + rng.from.In(loc).Format("2 Jan") + " to " + rng.to.In(loc).Format("2 Jan")
	}
	payload := windRosePayload{
		Title: title, Timezone: tz,
		Range:     telemetryChartRange{From: rng.from.In(loc).Format(time.RFC3339), To: rng.to.In(loc).Format(time.RFC3339)},
		Reference: "true", Sectors: sectors, SpeedUnit: unit, Bins: bins, Counts: tally.Counts,
		Samples: tally.Samples, Calm: tally.Calm, CalmBelow: windRoseCalmBelow, Variable: tally.Variable,
		Unpaired: unpaired, BucketMinutes: int(width / time.Minute), MeanSpeed: &mean, Strongest: strongest,
	}
	card, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("%s card: %w", tool, err)
	}

	// The model's summary: the same counts, as percentages of every sample.
	pct := func(n int) float64 { return round2(100 * float64(n) / float64(tally.Samples)) }
	type sectorSum struct {
		n, idx int
		speed  float64
	}
	sec := make([]sectorSum, sectors)
	for s := range sec {
		sec[s].idx = s
	}
	band := make([]int, len(edges)+1)
	// Sector mean speeds need the samples again; count them from the buckets.
	for _, smp := range samples {
		if smp.Speed < windRoseCalmBelow || smp.Dir == nil {
			continue
		}
		s := windRoseSectorOf(*smp.Dir, sectors)
		sec[s].n++
		sec[s].speed += smp.Speed
	}
	for _, row := range tally.Counts {
		for b, n := range row {
			band[b] += n
		}
	}
	sort.SliceStable(sec, func(i, j int) bool { return sec[i].n > sec[j].n })
	model := &windRoseModelResult{
		Title: title, Unit: unit, Samples: tally.Samples, BucketMinutes: int(width / time.Minute),
		Range:       rng.from.In(loc).Format("Mon 2 Jan 15:04") + " to " + rng.to.In(loc).Format("Mon 2 Jan 15:04"),
		CalmPercent: pct(tally.Calm), VariablePercent: pct(tally.Variable), MeanSpeed: &mean, StrongestGust: strongest,
		Unpaired:    unpaired,
		Note:        fmt.Sprintf("wind direction is where the wind blows from, in degrees true. Each sample is a %d-minute average. Percentages are of all samples; calm is under %g %s and has no direction.", int(width/time.Minute), windRoseCalmBelow, unit),
		Instruction: windRoseInstr,
	}
	for _, s := range sec {
		if s.n == 0 || len(model.Sectors) == 4 {
			break
		}
		centre := float64(s.idx) * 360 / float64(sectors)
		model.Sectors = append(model.Sectors, windRoseModelSector{
			Compass: compassPoint(centre), Degrees: round2(centre), Percent: pct(s.n), MeanSpeed: round2(s.speed / float64(s.n)),
		})
	}
	for b, n := range band {
		model.Bands = append(model.Bands, windRoseModelBand{Label: windRoseBandLabel(edges, b) + " " + unit, Percent: pct(n)})
	}
	text, err := capToolResultJSON(model, func() bool { return false })
	if err != nil {
		return "", err
	}
	d.cards.put(text, mateCard{Kind: "wind_rose", Payload: card})
	return text, nil
}

var mateToolShowWindRose = mateTool{
	name: "show_wind_rose",
	function: openRouterFunctionDef{
		Description: "Draw a wind rose card: where the true wind came from over a time range, counted from the " +
			"logged wind into compass sectors and speed bands in the operator's units (knots). Use it for \"where has " +
			"the wind been coming from\", \"what is the prevailing wind\", \"show me the wind rose\", and as the " +
			"answer to a wind trend over days, beside or instead of a speed-and-direction chart. A calm is counted " +
			"apart, and a wind with no steady direction is counted as variable. The card is attached to your reply, " +
			"and you get the sector and band percentages: describe where the wind came from, how strong it blew and " +
			"how often it was calm, and do not list the counts as a table. If the wind direction or speed is not " +
			"logged in the range, or two devices logged it, the tool says so.",
		Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"from": {"type": "string", "description": "Start of the range, RFC3339 (e.g. 2026-10-12T00:00:00+10:00). At most 60 days before to."},
						"to": {"type": "string", "description": "End of the range, RFC3339. Clamped to now."},
						"sectors": {"type": "integer", "enum": [8, 16, 32], "description": "Compass sectors. Default 16."},
						"speed_bins": {
							"type": "array", "maxItems": 8, "items": {"type": "number"},
							"description": "Optional band edges in knots, strictly increasing. [5,10,15,20,25] by default, which makes 0-5, 5-10 ... 25+."
						},
						"direction_source": {"type": "string", "description": "The device (source label) that logged the wind direction. Required only when more than one did; the error names them."},
						"speed_source": {"type": "string", "description": "The device that logged the wind speed. Required only when more than one did."},
						"title": {"type": "string", "description": "Optional short title in plain words, no paths."}
					},
					"required": ["from", "to"]
				}`),
	},
	execute: assistantToolDeps.executeShowWindRose,
	describe: func(args json.RawMessage) string {
		return "Drawing a wind rose from the logged wind…"
	},
}
