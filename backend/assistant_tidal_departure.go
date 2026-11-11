package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"
)

// This file implements plan_tidal_departure, the assistant tool that ranks
// departure times by how much of a passage runs with a fair stream.
//
// It has two methods and uses one for the whole result, named by
// ranking_basis:
//
//   - modelled_current: the Open-Meteo model gives the TOTAL current (tide plus
//     ocean currents such as the East Australian Current) as an hourly vector.
//     The tool dead-reckons the boat along the route, takes the course from the
//     route leg it is on, projects the modelled current at that point on it, and
//     ranks departures by the mean along-course current. fair_fraction is the
//     share of the covered samples with a positive along-course current. With a
//     route it is used whenever the model has a current for any sample of any
//     candidate; a sample with none is a gap, listed per candidate, never a 0. A flood
//     set is never derived from the total: offshore the ocean current swamps
//     the tide, so the vector says nothing about which way the flood sets.
//   - tide_phase: the original method. Tide plugins give high and low water
//     times, never stream direction or rate, so it models only the stream's
//     PHASE and takes the SET direction from the caller (flood_set_deg, labelled
//     by flood_set_source):
//   - Flood runs from each LW to the next HW, ebb from each HW to the next LW,
//     both shifted by slack_offset_min.
//   - Inside a half-cycle of length T, strength s(t) = sin(pi * t / T): zero at
//     slack, peak mid-cycle. Positive for flood, negative for ebb.
//   - The along-track factor is a(t) = s(t) * cos(course - flood_set).
//   - Each departure is scored by sampling a(t): mean_assist (-1..1) and
//     fair_fraction (the share of samples with a > 0).
//
// Departures are tried every 15 minutes across the window and sampled about
// every 5 minutes. tide_phase is used only with no route, or when the model has
// no current at all for the run.

const (
	assistantTidalDepartureMaxHours  = 24
	assistantTidalDepartureMaxWindow = 72 * time.Hour
	assistantTidalDepartureStep      = 15 * time.Minute
	assistantTidalSampleStep         = 5 * time.Minute
	assistantTidalMinSeparation      = 2 * time.Hour
	assistantTidalMaxCandidates      = 3
	assistantTidalMinCoveredFraction = 0.5
	assistantTidalMaxSlackOffsetMin  = 180
	// assistantTidalCrossStreamCos is cos(75 degrees): inside it the course is
	// within about 15 degrees of square to the stream.
	assistantTidalCrossStreamCos = 0.26
	// What the result itself says about how it was ranked. The caveats that hold
	// for every result are in ruleTidalDeparture, in the tool's description.
	assistantTidalBasisModelText = "Ranked on the Open-Meteo model's total current along the route; no tide data was used."
	assistantTidalBasisPhaseText = "Ranked by timing the stream from the tide station's high and low water and the flood direction in flood_set_source."

	assistantTidalMaxDays = 16
	// assistantTidalRouteNoteNm is how far the route's first point may be from
	// lat/lon before the answer says how far it is. The route may start in open
	// water off a creek or berth. assistantTidalRouteMaxNm is where it is clearly
	// a different passage, and an error.
	assistantTidalRouteNoteNm = 2.0
	assistantTidalRouteMaxNm  = 20.0
	// assistantTidalGridDeg is the grid the route's samples are grouped into, so
	// a cell is asked about once. It is the ocean model's own grid, 1/12 of a
	// degree (about 5 nm, or 9 km), with cell centres at k/12 + 1/24 degrees: live
	// Open-Meteo answers report -24.958336, 152.62502 and -25.458336, 153.79167.
	// One host cell is then one model cell. Open-Meteo itself prefers the nearest
	// sea cell, so a point near shore may be answered from a neighbour.
	assistantTidalGridDeg = 1.0 / 12

	assistantTidalBasisModel = "modelled_current"
	assistantTidalBasisPhase = "tide_phase"
)

// tidalZoneAt finds the time zone at the departure position. It is a variable so
// a test can count or fail the lookup.
var tidalZoneAt = zoneAtCached

type assistantTidalDepartureArgs struct {
	Lat            *float64          `json:"lat"`
	Lon            *float64          `json:"lon"`
	CourseDeg      *float64          `json:"course_deg"`
	PassageHours   *float64          `json:"passage_hours"`
	Route          []assistantLatLon `json:"route"`
	FloodSetDeg    *float64          `json:"flood_set_deg"`
	FloodSetSource string            `json:"flood_set_source"`
	SlackOffsetMin int               `json:"slack_offset_min"`
	Earliest       string            `json:"earliest"`
	Latest         string            `json:"latest"`
}

type assistantStreamPhase struct {
	Phase string `json:"phase"`
	From  string `json:"from"`
	To    string `json:"to"`
}

type assistantTidalCandidate struct {
	Departure    string `json:"departure"`
	DepartureISO string `json:"departure_iso"`
	Arrival      string `json:"arrival"`
	ArrivalISO   string `json:"arrival_iso"`
	// MeanAssist (-1..1) is the tide_phase score and is left out under
	// modelled_current, where the score is MeanAlongCourseKn.
	MeanAssist   *float64 `json:"mean_assist,omitempty"`
	FairFraction float64  `json:"fair_fraction"`
	// Stream and Phases come from the tide and appear only under tide_phase.
	Stream string                 `json:"stream,omitempty"`
	Phases []assistantStreamPhase `json:"phases,omitempty"`

	// The Open-Meteo modelled total current along the course over the run, in
	// knots: positive is with the boat, negative against. The peak is the sample
	// of largest size, with its sign. Both are left out, with the reason, when
	// the model has no current for some part of the run: that is not 0 kn.
	MeanAlongCourseKn  *float64 `json:"mean_along_course_kn,omitempty"`
	PeakAlongCourseKn  *float64 `json:"peak_along_course_kn,omitempty"`
	AlongCourseMissing string   `json:"along_course_missing,omitempty"`
	// CoveredFraction is the share of the run's samples the model had a current
	// for, and Uncovered the stretches of the route that were left out.
	CoveredFraction *float64                    `json:"covered_fraction,omitempty"`
	Uncovered       []assistantUncoveredStretch `json:"uncovered,omitempty"`
}

// assistantUncoveredStretch is a stretch of the route, in nautical miles from
// its start, that the model had no current for at this departure.
type assistantUncoveredStretch struct {
	FromNm float64 `json:"from_nm"`
	ToNm   float64 `json:"to_nm"`
}

type assistantTidalDepartureResult struct {
	// Station, Provider, HeightDatum and Modelled describe the tide data and
	// appear only under tide_phase, the one method that reads it. HeightDatum and
	// Modelled are as for get_tides: what the tide's heights are measured from,
	// and whether it is modelled for the place with no station. TimeBasis is the
	// zone the times are in, the station's under tide_phase and the departure
	// position's under modelled_current.
	Station     *assistantStationInfo `json:"station,omitempty"`
	Provider    string                `json:"provider,omitempty"`
	HeightDatum string                `json:"height_datum,omitempty"`
	Modelled    bool                  `json:"modelled,omitempty"`
	TimeBasis   string                `json:"time_basis"`
	CourseDeg   float64               `json:"course_deg"`
	// RankingBasis is modelled_current or tide_phase; every candidate uses the
	// same one. ModelledCurrentMissing says what lacked data when the model could
	// not rank the run, and is left out when it could.
	RankingBasis           string `json:"ranking_basis"`
	ModelledCurrentMissing string `json:"modelled_current_missing,omitempty"`
	// RoutePoints is the number of route points the stream was followed along,
	// 0 when the single position and course_deg were used.
	RoutePoints int `json:"route_points"`
	// FloodSetDeg and FloodSetSource are given only under tide_phase, which is
	// the only method that uses them.
	FloodSetDeg    *float64                  `json:"flood_set_deg,omitempty"`
	FloodSetSource string                    `json:"flood_set_source,omitempty"`
	SlackOffsetMin int                       `json:"slack_offset_min"`
	PassageHours   float64                   `json:"passage_hours"`
	Best           []assistantTidalCandidate `json:"best"`
	Worst          assistantTidalCandidate   `json:"worst"`
	Note           string                    `json:"note,omitempty"`
	Basis          string                    `json:"basis"`
	Reading        string                    `json:"reading"`
}

// tidalHalfCycle is one flood or ebb, after the slack offset is applied.
type tidalHalfCycle struct {
	start, end time.Time
	flood      bool
}

// tidalAssist returns a(t) for the half-cycle t falls in. cycles are in time
// order and contiguous, and t is inside [cycles[0].start, cycles[last].end].
func tidalAssist(cycles []tidalHalfCycle, t time.Time, alongFactor float64) float64 {
	i := sort.Search(len(cycles), func(i int) bool { return cycles[i].end.After(t) })
	if i == len(cycles) {
		return 0 // exactly on the last extreme: slack
	}
	c := cycles[i]
	frac := float64(t.Sub(c.start)) / float64(c.end.Sub(c.start))
	s := math.Sin(math.Pi * frac)
	if !c.flood {
		s = -s
	}
	return s * alongFactor
}

// tidalCurrentPoint is one hour of the modelled current as east and north
// components in m/s.
type tidalCurrentPoint struct {
	t    time.Time
	e, n float64
}

// tidalCurrents is the modelled current series at one point, in time order,
// holding only the hours the model gave.
type tidalCurrents []tidalCurrentPoint

// at interpolates the current vector at t between the two hours either side. It
// reports false when t is not bracketed by two hours no more than an hour apart:
// a missing hour is a gap, never slack.
func (c tidalCurrents) at(t time.Time) (e, n float64, ok bool) {
	i := sort.Search(len(c), func(i int) bool { return !c[i].t.Before(t) })
	if i < len(c) && c[i].t.Equal(t) {
		return c[i].e, c[i].n, true
	}
	if i == 0 || i == len(c) {
		return 0, 0, false
	}
	a, b := c[i-1], c[i]
	span := b.t.Sub(a.t)
	if span > time.Hour {
		return 0, 0, false
	}
	f := float64(t.Sub(a.t)) / float64(span)
	return a.e + f*(b.e-a.e), a.n + f*(b.n-a.n), true
}

// tidalGridPoint is the centre of the model cell a position falls in.
type tidalGridPoint struct{ lat, lon float64 }

// roundToTidalGrid is the model cell containing a position: the centre k/12 +
// 1/24 nearest to it, a point on the boundary going to the higher centre, as
// Open-Meteo does.
func roundToTidalGrid(lat, lon float64) tidalGridPoint {
	const half = assistantTidalGridDeg / 2
	r := func(v float64) float64 {
		k := math.Floor((v-half)/assistantTidalGridDeg + 0.5 + 1e-9)
		return math.Round((k*assistantTidalGridDeg+half)*1e6) / 1e6
	}
	return tidalGridPoint{lat: r(lat), lon: r(lon)}
}

// String is the cell centre to two decimals, the precision the operator reads.
func (p tidalGridPoint) String() string { return fmt.Sprintf("%.2f, %.2f", p.lat, p.lon) }

// tidalCurrentDays is the forecast_days to ask for so the last sample, at need,
// is bracketed by two hours. The request is in GMT with past_days=1, so the
// hourly series runs from 00:00 UTC yesterday to 23:00 UTC on the last forecast
// day, and today's UTC midnight can be up to 24 hours before now: the series may
// end 24 hours short of now + N days. One more hour covers the bracket after
// need, and one more day is margin. The answer is then raised to 7 days, or 16
// past that, so nearby windows ask for the same thing. A figure above
// assistantTidalMaxDays means the passage ends beyond the model's forecast.
func tidalCurrentDays(now, need time.Time) int {
	days := int(math.Ceil((need.Sub(now)+time.Hour).Hours()/24)) + 2
	switch {
	case days > assistantTidalMaxDays:
		return days
	case days <= 7:
		return 7
	default:
		return assistantTidalMaxDays
	}
}

// tidalSample is one dead-reckoned moment of a run: where the boat is, as a
// fraction of the passage, and the course it is on. It does not depend on the
// departure time, so it is worked out once.
type tidalSample struct {
	// pos is where the boat is, and cell the grid cell it falls in. The request
	// carries the first pos seen in each cell, not the cell's centre, which could
	// be on land when the sample is in water near shore.
	pos    assistantLatLon
	distNm float64 // distance along the route from its start
	cell   tidalGridPoint
	course float64
}

// tidalSamples dead-reckons n+1 evenly spaced samples along route, the boat
// covering the polyline's length at a steady speed. The course is the bearing of
// the leg each sample is on (the last leg at the end), and a repeated point,
// which makes a leg with no length and no bearing, takes the next leg's. A
// longitude difference over 180 degrees is the short way round the antimeridian.
// With no route it is the single position and fallbackCourse. A route whose
// points are all one position has no direction and is an error.
func tidalSamples(route []assistantLatLon, lat, lon, fallbackCourse float64, n int) ([]tidalSample, error) {
	out := make([]tidalSample, n+1)
	if len(route) < 2 {
		for k := range out {
			out[k] = tidalSample{pos: assistantLatLon{Lat: lat, Lon: lon}, cell: roundToTidalGrid(lat, lon), course: fallbackCourse}
		}
		return out, nil
	}
	// A repeated point makes a leg with no length and no bearing, so it is left
	// out and the sample takes the next leg's course.
	walk := routeWalk{}
	for i := 1; i < len(route); i++ {
		a, b := route[i-1], route[i]
		length := haversineMeters(a.Lat, a.Lon, b.Lat, b.Lon)
		if length > 0 {
			walk.segs = append(walk.segs, passageSegment{
				from:    passageRoutePoint{Lat: a.Lat, Lon: a.Lon},
				to:      passageRoutePoint{Lat: b.Lat, Lon: b.Lon},
				startM:  walk.totalM,
				lengthM: length,
				bearing: bearingDeg(a.Lat, a.Lon, b.Lat, b.Lon),
			})
		}
		walk.totalM += length
	}
	if len(walk.segs) == 0 {
		return nil, fmt.Errorf("every route point is the same position, so the route has no direction; leave route out to use lat and lon")
	}
	for k := range out {
		sLat, sLon, seg := walk.at(float64(k) / float64(n))
		out[k] = tidalSample{
			pos:    assistantLatLon{Lat: sLat, Lon: sLon},
			distNm: float64(k) / float64(n) * walk.totalM / metersPerNauticalMile,
			cell:   roundToTidalGrid(sLat, sLon),
			course: seg.bearing,
		}
	}
	return out, nil
}

// tidalPointCurrents is the modelled current at each distinct sample point, or
// the reason a point has none.
type tidalPointCurrents struct {
	series map[tidalGridPoint]tidalCurrents
	reason map[tidalGridPoint]string
}

// fetchTidalCurrents asks the wave provider for the modelled current along the
// route in ONE request: the first sample position in each distinct cell, in
// route order. It returns the series per cell, or the reason a cell has none.
// A gap that is not a failed request leaves every cell with that reason, so the
// run is not covered: no wave provider, a plugin without currents, a passage
// beyond the model's forecast. A route over maxCurrentsPoints cells, and a
// request that fails, are errors; the tool never splits the route or drops to
// another method because a request failed.
func (d assistantToolDeps) fetchTidalCurrents(ctx context.Context, samples []tidalSample, now, need time.Time) (tidalPointCurrents, error) {
	var cells []tidalGridPoint
	var query []currentsPoint
	seen := map[tidalGridPoint]bool{}
	for _, s := range samples {
		if !seen[s.cell] {
			seen[s.cell] = true
			cells = append(cells, s.cell)
			query = append(query, currentsPoint{Lat: s.pos.Lat, Lon: s.pos.Lon})
		}
	}
	if len(cells) > maxCurrentsPoints {
		return tidalPointCurrents{}, fmt.Errorf("the route needs the model at %d distinct cells, more than the %d one request carries; give a shorter route", len(cells), maxCurrentsPoints)
	}
	gapAll := func(reason string) (tidalPointCurrents, error) {
		out := tidalPointCurrents{series: map[tidalGridPoint]tidalCurrents{}, reason: map[tidalGridPoint]string{}}
		for _, c := range cells {
			out.reason[c] = reason
		}
		return out, nil
	}
	if d.waves == nil {
		return gapAll("no wave provider is available on this install, and the Open-Meteo current model comes through it")
	}
	// A wave provider that cannot be resolved (unreadable settings, a plugin named
	// in ui.wave_provider that is not installed) is a configuration fault to
	// report, not a place with no data.
	provider, id, err := d.waves()
	if errors.Is(err, errNoWaveProviderInstalled) {
		return gapAll("no wave plugin is installed, and the Open-Meteo current model comes through it")
	}
	if err != nil {
		return tidalPointCurrents{}, fmt.Errorf("the wave provider could not be resolved: %w", err)
	}
	cp, ok := provider.(currentsProvider)
	if !ok {
		return gapAll(fmt.Sprintf("plugin %q: %v", id, errNoCurrentsExport))
	}
	days := tidalCurrentDays(now, need)
	if days > assistantTidalMaxDays {
		return gapAll(fmt.Sprintf("the passage ends too far ahead for the Open-Meteo current model's %d-day forecast", assistantTidalMaxDays))
	}
	bundle, err := cp.FetchCurrents(ctx, query, days)
	if errors.Is(err, errNoCurrentsExport) {
		return gapAll(err.Error())
	}
	if err != nil {
		return tidalPointCurrents{}, fmt.Errorf("the Open-Meteo current request failed: %w", err)
	}
	// Any provider may answer, so the count is checked here before it is indexed.
	if len(bundle.Points) != len(cells) {
		return tidalPointCurrents{}, fmt.Errorf("the Open-Meteo current request answered for %d points, asked for %d", len(bundle.Points), len(cells))
	}
	out := tidalPointCurrents{series: map[tidalGridPoint]tidalCurrents{}, reason: map[tidalGridPoint]string{}}
	for i, c := range cells {
		series := currentsSeries(bundle.Points[i])
		if len(series) == 0 {
			out.reason[c] = fmt.Sprintf("the Open-Meteo model has no current at %s (it is null near shore and over land)", c)
			continue
		}
		out.series[c] = series
	}
	return out, nil
}

// currentsSeries turns one point's hours into a time-ordered east and north
// series, leaving out the hours with no current.
func currentsSeries(p currentPointSeries) tidalCurrents {
	var series tidalCurrents
	for _, h := range p.Hourly {
		if h.VelocityMS == nil || h.SetDeg == nil {
			continue
		}
		rad := *h.SetDeg * math.Pi / 180
		series = append(series, tidalCurrentPoint{t: h.Time, e: *h.VelocityMS * math.Sin(rad), n: *h.VelocityMS * math.Cos(rad)})
	}
	sort.Slice(series, func(i, j int) bool { return series[i].t.Before(series[j].t) })
	return series
}

// alongRun is the modelled current along the course over one run, over the
// samples that have data. A sample has none when its cell is null or the model's
// hours do not bracket its time; that is a gap in the run, never a 0.
type alongRun struct {
	meanKn, peakKn float64
	fairFraction   float64 // share of the covered samples with a positive current
	covered, total int
	uncovered      []assistantUncoveredStretch
}

// coveredFraction is the share of the run's samples that have a current.
func (r alongRun) coveredFraction() float64 { return float64(r.covered) / float64(r.total) }

// alongCourse projects the modelled total current at each sample's cell on that
// sample's course, for a run leaving at dep. The peak is the sample of largest
// size, with its sign.
func alongCourse(cur tidalPointCurrents, samples []tidalSample, distNm []float64, dep time.Time, duration time.Duration) alongRun {
	n := len(samples) - 1
	sum, peak := 0.0, 0.0
	fair := 0
	covered := make([]bool, len(samples))
	run := alongRun{total: len(samples)}
	for k, s := range samples {
		t := dep.Add(time.Duration(float64(duration) * float64(k) / float64(n)))
		e, nn, ok := cur.series[s.cell].at(t)
		if !ok {
			continue
		}
		covered[k] = true
		rad := s.course * math.Pi / 180
		a := (e*math.Sin(rad) + nn*math.Cos(rad)) * metersPerSecondToKnots
		sum += a
		run.covered++
		if a > 0 {
			fair++
		}
		if math.Abs(a) > math.Abs(peak) {
			peak = a
		}
	}
	run.uncovered = uncoveredStretches(distNm, covered, samples[n].distNm)
	if run.covered > 0 {
		run.meanKn, run.peakKn, run.fairFraction = sum/float64(run.covered), peak, float64(fair)/float64(run.covered)
	}
	return run
}

// uncoveredStretches turns uncovered samples into stretches of the route, in
// nautical miles from its start. Each uncovered sample stands for half a sample
// spacing either side of it, clamped to the route, and touching ones merge, so a
// single sample in the middle is one spacing wide and one at either end of the
// route is half of one.
func uncoveredStretches(distNm []float64, covered []bool, totalNm float64) []assistantUncoveredStretch {
	var out []assistantUncoveredStretch
	half := 0.0
	if len(distNm) > 1 {
		half = totalNm / float64(len(distNm)-1) / 2
	}
	for i, ok := range covered {
		if ok {
			continue
		}
		from, to := math.Max(0, distNm[i]-half), math.Min(totalNm, distNm[i]+half)
		if n := len(out); n > 0 && from <= out[n-1].ToNm+1e-9 {
			out[n-1].ToNm = to
			continue
		}
		out = append(out, assistantUncoveredStretch{FromNm: from, ToNm: to})
	}
	for i := range out {
		out[i].FromNm, out[i].ToNm = roundTo1(out[i].FromNm), roundTo1(out[i].ToNm)
	}
	return out
}

// noModelDataReason says why no sample of the run has a current, from the cells
// the model was asked about.
func noModelDataReason(cur tidalPointCurrents, samples []tidalSample) string {
	if len(cur.series) > 0 {
		return "the Open-Meteo model has no current at the hours of the run"
	}
	seen := map[tidalGridPoint]bool{}
	var reasons []string
	have := map[string]bool{}
	for _, smp := range samples {
		if seen[smp.cell] {
			continue
		}
		seen[smp.cell] = true
		if r := cur.reason[smp.cell]; r != "" && !have[r] {
			have[r] = true
			reasons = append(reasons, r)
		}
	}
	return fmt.Sprintf("no current at any of the %d route cells (%s)", len(seen), strings.Join(reasons, "; "))
}

type tidalScored struct {
	dep        time.Time
	mean, fair float64
	run        alongRun
	// ineligible departures are not best or worst: no data, or too little of the
	// run covered to rank against departures with more.
	ineligible bool
}

func (d assistantToolDeps) executePlanTidalDeparture(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var args assistantTidalDepartureArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse plan_tidal_departure arguments: %w", err)
	}
	for _, req := range []struct {
		name string
		v    *float64
	}{{"lat", args.Lat}, {"lon", args.Lon}, {"course_deg", args.CourseDeg}, {"passage_hours", args.PassageHours}} {
		if req.v == nil {
			return "", fmt.Errorf("plan_tidal_departure: %s is required", req.name)
		}
	}
	lat, lon, course, hours := *args.Lat, *args.Lon, *args.CourseDeg, *args.PassageHours
	if !validLatLon(lat, lon) {
		return "", fmt.Errorf("plan_tidal_departure: lat/lon is not a valid position")
	}
	if course < 0 || course > 360 {
		return "", fmt.Errorf("plan_tidal_departure: course_deg must be between 0 and 360")
	}
	suppliedSet := args.FloodSetDeg != nil
	if suppliedSet && (*args.FloodSetDeg < 0 || *args.FloodSetDeg > 360) {
		return "", fmt.Errorf("plan_tidal_departure: flood_set_deg must be between 0 and 360")
	}
	// The passage is sampled in whole minutes, so anything under one rounds
	// to zero and the mean divides 0 by 0.
	if !(hours >= 1.0/60) || hours > assistantTidalDepartureMaxHours {
		return "", fmt.Errorf("plan_tidal_departure: passage_hours must be at least one minute and at most %d", assistantTidalDepartureMaxHours)
	}
	if suppliedSet {
		switch args.FloodSetSource {
		case "standing_notes", "general_knowledge", "operator":
		default:
			return "", fmt.Errorf("plan_tidal_departure: flood_set_source must be standing_notes, general_knowledge or operator")
		}
	} else if args.FloodSetSource != "" {
		return "", fmt.Errorf("plan_tidal_departure: flood_set_deg is required with flood_set_source %q", args.FloodSetSource)
	}
	route := args.Route
	if len(route) == 1 {
		return "", fmt.Errorf("plan_tidal_departure: route needs at least two points, as estimate_passage returns them; leave it out to use lat and lon")
	}
	for i, p := range route {
		if !validLatLon(p.Lat, p.Lon) {
			return "", fmt.Errorf("plan_tidal_departure: route point %d is not a valid position", i+1)
		}
	}
	offsetMin := args.SlackOffsetMin
	if offsetMin > assistantTidalMaxSlackOffsetMin {
		offsetMin = assistantTidalMaxSlackOffsetMin
	}
	if offsetMin < -assistantTidalMaxSlackOffsetMin {
		offsetMin = -assistantTidalMaxSlackOffsetMin
	}
	offset := time.Duration(offsetMin) * time.Minute

	// A departure in the past is not one to plan, so the window starts no earlier
	// than now, and a default latest follows that start.
	now := d.now()
	earliest := now
	if s := strings.TrimSpace(args.Earliest); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return "", fmt.Errorf("plan_tidal_departure: earliest must be RFC3339: %w", err)
		}
		if t.After(earliest) {
			earliest = t
		}
	}
	latest := earliest.Add(24 * time.Hour)
	if s := strings.TrimSpace(args.Latest); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return "", fmt.Errorf("plan_tidal_departure: latest must be RFC3339: %w", err)
		}
		latest = t
	}
	if !latest.After(earliest) {
		if !latest.After(now) {
			return "", fmt.Errorf("plan_tidal_departure: the departure window is already past: latest is not after now")
		}
		return "", fmt.Errorf("plan_tidal_departure: latest must be after earliest")
	}
	if latest.Sub(earliest) > assistantTidalDepartureMaxWindow {
		return "", fmt.Errorf("plan_tidal_departure: the departure window may span at most %d hours", int(assistantTidalDepartureMaxWindow/time.Hour))
	}

	// Whole minutes keep the arithmetic exact (6.2 h is 372 min, not 372 min
	// less a float rounding error).
	duration := time.Duration(math.Round(hours*60)) * time.Minute
	need := latest.Add(duration)

	// The route may start in open water off the creek or berth the boat is in, so
	// a start away from lat/lon is said, not refused, until it is a different passage.
	var notes []string
	if len(route) >= 2 {
		off := haversineMeters(lat, lon, route[0].Lat, route[0].Lon) / metersPerNauticalMile
		if off > assistantTidalRouteMaxNm {
			return "", fmt.Errorf("plan_tidal_departure: the route starts at %g, %g, %.1f nm from lat/lon %g, %g; more than %g nm apart is a different passage, so give the route from the departure position",
				route[0].Lat, route[0].Lon, off, lat, lon, assistantTidalRouteMaxNm)
		}
		if off > assistantTidalRouteNoteNm {
			notes = append(notes, fmt.Sprintf("The route starts %.1f nm from lat/lon, which is not on it. The stream was read along the route.", off))
		}
	}

	n := int(math.Ceil(float64(duration) / float64(assistantTidalSampleStep)))
	samples, err := tidalSamples(route, lat, lon, course, n)
	if err != nil {
		return "", fmt.Errorf("plan_tidal_departure: %w", err)
	}

	// The model is read along a route. One cell is not the whole run, so with no
	// route the model is not used at all.
	var currents tidalPointCurrents
	noData := "no route was given, so the current along the passage was not read"
	if len(route) >= 2 {
		noData = ""
		currents, err = d.fetchTidalCurrents(ctx, samples, now, need)
		if err != nil {
			return "", fmt.Errorf("plan_tidal_departure: %w", err)
		}
	}

	// Every departure is ranked on the model over the samples it has data for. The
	// tide is used only when the model has no data for any sample of any departure.
	distNm := make([]float64, len(samples))
	for k, s := range samples {
		distNm[k] = s.distNm
	}
	var scored []tidalScored
	anyData := false
	for dep := earliest; !dep.After(latest); dep = dep.Add(assistantTidalDepartureStep) {
		var run alongRun
		if noData == "" {
			run = alongCourse(currents, samples, distNm, dep, duration)
			anyData = anyData || run.covered > 0
		}
		scored = append(scored, tidalScored{dep: dep, run: run})
	}
	basis := assistantTidalBasisModel
	if !anyData {
		basis = assistantTidalBasisPhase
		if noData == "" {
			noData = noModelDataReason(currents, samples)
		}
		if !suppliedSet {
			return "", fmt.Errorf("plan_tidal_departure: the Open-Meteo model has no current for the run (%s) and flood_set_deg was not given, so there is nothing to rank with; give flood_set_deg and its flood_set_source from the standing notes, the operator or general knowledge, or say the stream cannot be timed", noData)
		}
		log.Printf("assistant: plan_tidal_departure: tide_phase used because the Open-Meteo model has no current for the run: %s", noData)
	}

	// Tide data is read only by the tide-phase method. Times are in the station's
	// zone then, and in the departure position's zone under the model.
	var st assistantResolvedTideStation
	var cycles []tidalHalfCycle
	var loc *time.Location
	var timeBasis string
	// Each sample is on its own leg, so a route is scored on the leg courses; with
	// no route every sample has course_deg. alongFactor[k] is how much of the stream
	// the sample feels, and mostAcross says whether the whole run is square to it.
	var alongFactor []float64
	mostAcross := true
	if basis == assistantTidalBasisPhase {
		st, err = d.resolveTideStation(lat, lon)
		if err != nil {
			return "", fmt.Errorf("plan_tidal_departure: %w", err)
		}
		cycles, err = tidalHalfCycles(st.chart.Extremes, offset, earliest, need)
		if err != nil {
			return "", fmt.Errorf("plan_tidal_departure: station %q (%s): %w", st.info.Name, st.providerID, err)
		}
		loc, timeBasis = st.loc, st.timeBasis
		alongFactor = make([]float64, len(samples))
		for k, s := range samples {
			alongFactor[k] = math.Cos((s.course - *args.FloodSetDeg) * math.Pi / 180)
			if math.Abs(alongFactor[k]) >= assistantTidalCrossStreamCos {
				mostAcross = false
			}
		}
	} else {
		// Times are the boat's, in the zone at the departure. Only this method needs it.
		z, zerr := tidalZoneAt(lat, lon)
		if zerr != nil {
			return "", fmt.Errorf("plan_tidal_departure: the time zone at the departure position is not known: %w", zerr)
		}
		loc, timeBasis = z.Loc, z.Name
	}
	// A departure covered less than half is judged on too little of its run to
	// stand against a better covered one. When none is covered half, the ones with
	// data are ranked anyway and the answer says every run was mostly uncovered.
	anyWellCovered := false
	for _, sc := range scored {
		if sc.run.coveredFraction() >= assistantTidalMinCoveredFraction && sc.run.covered > 0 {
			anyWellCovered = true
		}
	}
	if basis == assistantTidalBasisModel && !anyWellCovered {
		notes = append(notes, "Every departure's run was mostly uncovered by the model, so these figures rest on a small part of the passage.")
	}
	for i := range scored {
		sc := &scored[i]
		if basis == assistantTidalBasisModel {
			sc.mean, sc.fair = sc.run.meanKn, sc.run.fairFraction
			sc.ineligible = sc.run.covered == 0 || (anyWellCovered && sc.run.coveredFraction() < assistantTidalMinCoveredFraction)
			continue
		}
		sum, fair := 0.0, 0
		for k := 0; k <= n; k++ {
			t := sc.dep.Add(time.Duration(float64(duration) * float64(k) / float64(n)))
			a := tidalAssist(cycles, t, alongFactor[k])
			sum += a
			if a > 0 {
				fair++
			}
		}
		sc.mean, sc.fair = sum/float64(n+1), float64(fair)/float64(n+1)
	}

	// Local maxima (plateau-tolerant, the window edges counting against their
	// one neighbour), best first, kept only if at least two hours from every
	// one already chosen so the list is distinct departures, not adjacent slots.
	var peaks []tidalScored
	for i, c := range scored {
		if c.ineligible {
			continue
		}
		if (i > 0 && !scored[i-1].ineligible && scored[i-1].mean > c.mean) || (i+1 < len(scored) && !scored[i+1].ineligible && scored[i+1].mean > c.mean) {
			continue
		}
		peaks = append(peaks, c)
	}
	sort.SliceStable(peaks, func(i, j int) bool { return peaks[i].mean > peaks[j].mean })
	var chosen []tidalScored
	for _, p := range peaks {
		apart := true
		for _, c := range chosen {
			gap := p.dep.Sub(c.dep)
			if gap < 0 {
				gap = -gap
			}
			if gap < assistantTidalMinSeparation {
				apart = false
				break
			}
		}
		if apart {
			chosen = append(chosen, p)
			if len(chosen) == assistantTidalMaxCandidates {
				break
			}
		}
	}
	// The one to avoid is the worst departure that has data to judge it by.
	var worst tidalScored
	haveWorst := false
	for _, c := range scored {
		if c.ineligible {
			continue
		}
		if !haveWorst || c.mean < worst.mean {
			worst, haveWorst = c, true
		}
	}

	build := func(c tidalScored) assistantTidalCandidate {
		arr := c.dep.Add(duration)
		cand := assistantTidalCandidate{
			Departure:    boatDayClock(c.dep, loc),
			DepartureISO: c.dep.UTC().Format(time.RFC3339),
			Arrival:      boatDayClock(arr, loc),
			ArrivalISO:   arr.UTC().Format(time.RFC3339),
			FairFraction: roundTo2(c.fair),
		}
		if basis == assistantTidalBasisPhase {
			m := roundTo2(c.mean)
			cand.MeanAssist = &m
			cand.Phases = tidalPhasesCrossed(cycles, c.dep, arr, loc)
			names := make([]string, len(cand.Phases))
			for i, p := range cand.Phases {
				names[i] = p.Phase
			}
			cand.Stream = strings.Join(names, " then ")
			// modelled_current_missing in the result is the one reason.
			return cand
		}
		frac := roundTo2(c.run.coveredFraction())
		cand.CoveredFraction, cand.Uncovered = &frac, c.run.uncovered
		if c.run.covered == 0 {
			cand.AlongCourseMissing = "the model has no current at any point of this run"
			return cand
		}
		mean, peak := roundTo2(c.run.meanKn), roundTo2(c.run.peakKn)
		cand.MeanAlongCourseKn, cand.PeakAlongCourseKn = &mean, &peak
		return cand
	}

	basisText, modelMissing := assistantTidalBasisModelText, ""
	if basis == assistantTidalBasisPhase {
		basisText, modelMissing = assistantTidalBasisPhaseText, noData
	}
	result := assistantTidalDepartureResult{
		TimeBasis:              timeBasis,
		CourseDeg:              course,
		RankingBasis:           basis,
		ModelledCurrentMissing: modelMissing,
		RoutePoints:            len(route),
		SlackOffsetMin:         offsetMin,
		PassageHours:           hours,
		Best:                   make([]assistantTidalCandidate, 0, len(chosen)),
		Worst:                  build(worst),
		Basis:                  basisText,
		Reading:                readingTidalDeparture,
	}
	for _, c := range chosen {
		result.Best = append(result.Best, build(c))
	}
	if basis == assistantTidalBasisPhase {
		result.Station, result.Provider = &st.info, st.providerID
		result.HeightDatum, result.Modelled = st.datum, st.followsPos
		result.FloodSetDeg, result.FloodSetSource = args.FloodSetDeg, args.FloodSetSource
		if mostAcross {
			notes = append(notes, "The course runs mostly across the stream, so departure timing barely changes the help along the track.")
		}
	} else if suppliedSet {
		notes = append(notes, "The flood direction given was not used: the modelled current covers the run, so the departures are ranked on it.")
	}
	result.Note = strings.Join(notes, " ")
	return capToolResultJSON(&result, func() bool { return false })
}

// tidalHalfCycles turns the station's extremes into contiguous flood and ebb
// half-cycles (shifted by offset) that bracket [from, to]. It never guesses:
// when the data does not reach both ends, or the extremes do not alternate
// high and low across the range, it says what is missing.
func tidalHalfCycles(extremes []tideExtremePoint, offset time.Duration, from, to time.Time) ([]tidalHalfCycle, error) {
	if len(extremes) == 0 {
		return nil, fmt.Errorf("the tide provider returned no extremes")
	}
	ext := append([]tideExtremePoint(nil), extremes...)
	sort.Slice(ext, func(i, j int) bool { return ext[i].Time.Before(ext[j].Time) })
	shifted := func(i int) time.Time { return ext[i].Time.Add(offset) }

	first := sort.Search(len(ext), func(i int) bool { return shifted(i).After(from) }) - 1
	if first < 0 {
		return nil, fmt.Errorf("tide extremes start at %s, after the first possible departure %s; the extreme just before it is missing",
			shifted(0).UTC().Format(time.RFC3339), from.UTC().Format(time.RFC3339))
	}
	last := sort.Search(len(ext), func(i int) bool { return !shifted(i).Before(to) })
	if last == len(ext) {
		return nil, fmt.Errorf("tide extremes end at %s, before the passage ends at %s; the extreme just after it is missing",
			shifted(len(ext)-1).UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
	}

	cycles := make([]tidalHalfCycle, 0, last-first)
	for i := first; i < last; i++ {
		if ext[i].High == ext[i+1].High {
			return nil, fmt.Errorf("tide extremes at %s and %s do not alternate high and low water",
				ext[i].Time.UTC().Format(time.RFC3339), ext[i+1].Time.UTC().Format(time.RFC3339))
		}
		cycles = append(cycles, tidalHalfCycle{start: shifted(i), end: shifted(i + 1), flood: !ext[i].High})
	}
	return cycles, nil
}

// tidalPhasesCrossed lists the flood and ebb spells that overlap [dep, arr],
// clipped to the passage, in order.
func tidalPhasesCrossed(cycles []tidalHalfCycle, dep, arr time.Time, loc *time.Location) []assistantStreamPhase {
	out := []assistantStreamPhase{}
	for _, c := range cycles {
		from, to := c.start, c.end
		if from.Before(dep) {
			from = dep
		}
		if to.After(arr) {
			to = arr
		}
		if !to.After(from) {
			continue
		}
		name := "ebb"
		if c.flood {
			name = "flood"
		}
		out = append(out, assistantStreamPhase{Phase: name, From: boatWeekdayClock(from, loc), To: boatWeekdayClock(to, loc)})
	}
	return out
}

var mateToolPlanTidalDeparture = mateTool{
	name:      "plan_tidal_departure",
	effect:    effectReads,
	audiences: audienceMCPOnly,
	function: openRouterFunctionDef{
		Description: "Rank departure times by how much of a passage runs with a fair stream. Call it after estimate_passage, " +
			"with estimate_passage's course_deg as course_deg, the estimated hours as passage_hours, and its route as route. " +
			"The Open-Meteo current model is read along the route, and without one it is not used at all, " +
			"so the stream is timed from the tide: if estimate_passage left the route out (route_omitted), say that is why. " +
			"With a route the departures are always ranked on the model (ranking_basis modelled_current), over the points it has a current for. " +
			"Only where the model has no current at all for the run, or there is no route, does it time the stream from the nearest tide station's " +
			"high and low water and the direction you give for the flood (ranking_basis tide_phase, the only one that reports the station, stream and phases). " +
			"Give flood_set_deg and flood_set_source only from the operator's standing notes or the operator " +
			"(standing_notes, operator), or general knowledge you are sure of (general_knowledge): it is used only for tide_phase, " +
			"so omit it when you do not know it. If tide_phase is needed and you gave no direction, the call fails and says what is missing: then say the stream cannot be timed. " +
			"A failed request to the model is an error too, and is never made up for with the tide. " +
			ruleTidalDeparture +
			" Do not use it for a trip that starts later than the forecast covers or somewhere other than here. " +
			"The result's reading says how to read the stream against the wind.",
		Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"lat": {"type": "number", "description": "Latitude of the departure position, used to find the tide station and the time zone."},
						"lon": {"type": "number", "description": "Longitude of the departure position."},
						"course_deg": {"type": "number", "description": "Passage course in degrees true, 0 to 360 (estimate_passage's course_deg, the longest leg's bearing). Used by the tide-phase method; with a route, each leg's own course is used."},
						"passage_hours": {"type": "number", "description": "Passage duration in hours, greater than 0 and at most 24 (from estimate_passage)."},
						"route": {
							"type": "array",
							"description": "The route points in order, as estimate_passage returns them (each with lat and lon, at least two, starting near lat and lon: one starting more than 20 nm away is refused). The boat is taken along it at a steady speed and the current is read at each point with the course of the leg it is on. Pass it whenever estimate_passage returned one.",
							"items": {
								"type": "object",
								"properties": {"lat": {"type": "number"}, "lon": {"type": "number"}},
								"required": ["lat", "lon"]
							}
						},
						"flood_set_deg": {"type": "number", "description": "Optional. Direction in degrees true that the flood stream sets toward, 0 to 360, only from standing notes or the operator, or general knowledge you are sure of. Used only for tide_phase."},
						"flood_set_source": {
							"type": "string",
							"enum": ["standing_notes", "general_knowledge", "operator"],
							"description": "Where flood_set_deg came from, required with it; it is echoed back so the answer can label it."
						},
						"slack_offset_min": {
							"type": "integer",
							"description": "Minutes the turn of the stream lags (positive) or leads (negative) local high and low water, -180 to 180 (default 0). Only when standing notes or knowledge say so. Used by the tide-phase method."
						},
						"earliest": {"type": "string", "description": "Earliest departure, RFC3339. Default now."},
						"latest": {"type": "string", "description": "Latest departure, RFC3339. Default 24 hours after earliest (or now)."}
					},
					"required": ["lat", "lon", "course_deg", "passage_hours"]
				}`),
	},
	execute: plainTool(assistantToolDeps.executePlanTidalDeparture),
	describe: func(args json.RawMessage) string {
		return fmt.Sprintf("Timing the departure to the tide near %s…", assistantLocationLabel(args))
	},
}
