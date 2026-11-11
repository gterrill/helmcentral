package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
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
//     share of the run with a positive along-course current. This is used only
//     when the model has a current at every sample of every candidate. A flood
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
// every 5 minutes. The modelled along-course figures (mean and peak, in knots)
// are reported for every candidate in either mode when the model covers its run.

const (
	assistantTidalDepartureMaxHours  = 24
	assistantTidalDepartureMaxWindow = 72 * time.Hour
	assistantTidalDepartureStep      = 15 * time.Minute
	assistantTidalSampleStep         = 5 * time.Minute
	assistantTidalMinSeparation      = 2 * time.Hour
	assistantTidalMaxCandidates      = 3
	assistantTidalMaxSlackOffsetMin  = 180
	// assistantTidalCrossStreamCos is cos(75 degrees): inside it the course is
	// within about 15 degrees of square to the stream.
	assistantTidalCrossStreamCos = 0.26
	assistantTidalBasis          = "The stream is the Open-Meteo model's total current (tide plus ocean currents such as the East Australian Current), " +
		"a forecast on a grid of about 8 km, not an observation. It does not resolve river entrances, narrow straits such as the Great Sandy Strait, or shoals, " +
		"and rates there are likely understated. ranking_basis says how the departures were ranked: modelled_current ranks by the modelled current along the " +
		"course; tide_phase times the stream from the tide station's high and low water and the flood set in flood_set_source."

	assistantTidalMaxDays = 16
	// assistantTidalGridDeg is the grid the route's sample points are rounded to,
	// so a point is fetched once and its answer is shared through the cache. It
	// is the model's own grid, 1/12 of a degree (about 5 nm): Open-Meteo reports
	// the cell centres of the SMOC ocean model, which is 0.08 degrees, so a finer
	// grid would fetch the same cell twice. The currents cache keys on it too.
	assistantTidalGridDeg = 1.0 / 12
	// assistantTidalMaxPoints bounds the distinct model points one call fetches.
	assistantTidalMaxPoints = 60
	// assistantTidalFetchParallel is how many point fetches run at once.
	assistantTidalFetchParallel = 8

	assistantTidalBasisModel = "modelled_current"
	assistantTidalBasisPhase = "tide_phase"
)

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
	MeanAssist   *float64               `json:"mean_assist,omitempty"`
	FairFraction float64                `json:"fair_fraction"`
	Stream       string                 `json:"stream"`
	Phases       []assistantStreamPhase `json:"phases"`

	// The Open-Meteo modelled total current along the course over the run, in
	// knots: positive is with the boat, negative against. The peak is the sample
	// of largest size, with its sign. Both are left out, with the reason, when
	// the model has no current for some part of the run: that is not 0 kn.
	MeanAlongCourseKn  *float64 `json:"mean_along_course_kn,omitempty"`
	PeakAlongCourseKn  *float64 `json:"peak_along_course_kn,omitempty"`
	AlongCourseMissing string   `json:"along_course_missing,omitempty"`
}

type assistantTidalDepartureResult struct {
	Station  assistantStationInfo `json:"station"`
	Provider string               `json:"provider"`
	// HeightDatum and Modelled are as for get_tides: what the tide's heights are
	// measured from, and whether it is modelled for the place with no station.
	HeightDatum string  `json:"height_datum"`
	Modelled    bool    `json:"modelled"`
	TimeBasis   string  `json:"time_basis"`
	CourseDeg   float64 `json:"course_deg"`
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
type tidalCurrents struct {
	points []tidalCurrentPoint
}

// at interpolates the current vector at t between the two hours either side. It
// reports false when t is not bracketed by two hours no more than an hour apart:
// a missing hour is a gap, never slack.
func (c tidalCurrents) at(t time.Time) (e, n float64, ok bool) {
	i := sort.Search(len(c.points), func(i int) bool { return !c.points[i].t.Before(t) })
	if i < len(c.points) && c.points[i].t.Equal(t) {
		return c.points[i].e, c.points[i].n, true
	}
	if i == 0 || i == len(c.points) {
		return 0, 0, false
	}
	a, b := c.points[i-1], c.points[i]
	span := b.t.Sub(a.t)
	if span > time.Hour {
		return 0, 0, false
	}
	f := float64(t.Sub(a.t)) / float64(span)
	return a.e + f*(b.e-a.e), a.n + f*(b.n-a.n), true
}

// tidalGridPoint is a position rounded to assistantTidalGridDeg.
type tidalGridPoint struct{ lat, lon float64 }

func roundToTidalGrid(lat, lon float64) tidalGridPoint {
	r := func(v float64) float64 {
		return math.Round(math.Round(v/assistantTidalGridDeg)*assistantTidalGridDeg*1e4) / 1e4
	}
	return tidalGridPoint{lat: r(lat), lon: r(lon)}
}

func (p tidalGridPoint) String() string { return fmt.Sprintf("%.3f, %.3f", p.lat, p.lon) }

// tidalCurrentDays is the forecast_days to ask for so the last sample, at need,
// is bracketed by two hours. Open-Meteo's hourly series runs from 00:00 local on
// today's date to 23:00 local on the last day, and the local day can start up to
// 24 hours before now, so the series may end 24 hours short of now + N days. One
// more hour covers the bracket after need, and one more day is margin.
func tidalCurrentDays(now, need time.Time) int {
	days := int(math.Ceil((need.Sub(now)+time.Hour).Hours()/24)) + 2
	if days < 1 {
		days = 1
	}
	return days
}

// tidalSample is one dead-reckoned moment of a run: where the boat is, as a
// fraction of the passage, and the course it is on. It does not depend on the
// departure time, so it is worked out once.
type tidalSample struct {
	pt     tidalGridPoint
	course float64
}

// tidalSamples dead-reckons n+1 evenly spaced samples along route, the boat
// covering the polyline's length at a steady speed. The course is the bearing of
// the leg each sample is on (the last leg at the end). With no route it is the
// single position and fallbackCourse.
func tidalSamples(route []assistantLatLon, lat, lon, fallbackCourse float64, n int) []tidalSample {
	out := make([]tidalSample, n+1)
	if len(route) < 2 {
		p := roundToTidalGrid(lat, lon)
		for k := range out {
			out[k] = tidalSample{pt: p, course: fallbackCourse}
		}
		return out
	}
	cum := make([]float64, len(route))
	for i := 1; i < len(route); i++ {
		cum[i] = cum[i-1] + haversineMeters(route[i-1].Lat, route[i-1].Lon, route[i].Lat, route[i].Lon)
	}
	total := cum[len(route)-1]
	// Legs with length. A repeated point makes a zero-length leg, which has no
	// bearing, so the course is taken from the next leg that has one.
	var legs []int
	for i := 0; i+1 < len(route); i++ {
		if cum[i+1] > cum[i] {
			legs = append(legs, i)
		}
	}
	if len(legs) == 0 {
		p := roundToTidalGrid(lat, lon)
		for k := range out {
			out[k] = tidalSample{pt: p, course: fallbackCourse}
		}
		return out
	}
	li := 0
	for k := range out {
		d := total * float64(k) / float64(n)
		for li < len(legs)-1 && d >= cum[legs[li]+1] {
			li++
		}
		leg := legs[li]
		a, b := route[leg], route[leg+1]
		legLen := cum[leg+1] - cum[leg]
		f := 0.0
		if legLen > 0 {
			f = math.Min(1, math.Max(0, (d-cum[leg])/legLen))
		}
		// Interpolate on the coordinates: legs are short against the 0.05 grid
		// error this feeds, and the course is the leg's own bearing.
		out[k] = tidalSample{
			pt:     roundToTidalGrid(a.Lat+f*(b.Lat-a.Lat), a.Lon+f*(b.Lon-a.Lon)),
			course: bearingDeg(a.Lat, a.Lon, b.Lat, b.Lon),
		}
	}
	return out
}

// tidalPointCurrents is the modelled current at each distinct sample point, or
// the reason a point has none.
type tidalPointCurrents struct {
	series map[tidalGridPoint]tidalCurrents
	reason map[tidalGridPoint]string
}

// fetchTidalCurrents reads the modelled current at every distinct point, each
// once, through the wave provider's currents capability. The reason is stored per
// point when it has none: no provider, a plugin without currents, a failed fetch,
// a window beyond the model's range, or a model with no current there.
func (d assistantToolDeps) fetchTidalCurrents(ctx context.Context, points []tidalGridPoint, now, need time.Time) (tidalPointCurrents, error) {
	out := tidalPointCurrents{series: map[tidalGridPoint]tidalCurrents{}, reason: map[tidalGridPoint]string{}}
	failAll := func(reason string) (tidalPointCurrents, error) {
		for _, p := range points {
			out.reason[p] = reason
		}
		return out, nil
	}
	if d.waves == nil {
		return failAll("no wave provider is available on this install, and the Open-Meteo current model comes through it")
	}
	provider, id, err := d.waves()
	if err != nil {
		return failAll("the wave provider is not available: " + firstErrorLine(err))
	}
	cp, ok := provider.(currentsProvider)
	if !ok || !cp.SupportsCurrents() {
		return failAll(fmt.Sprintf("the installed wave plugin %q has no currents; install the updated open-meteo-marine plugin", id))
	}
	days := tidalCurrentDays(now, need)
	if days > assistantTidalMaxDays {
		return failAll(fmt.Sprintf("the passage ends too far ahead for the Open-Meteo current model's %d-day forecast", assistantTidalMaxDays))
	}
	if len(points) > assistantTidalMaxPoints {
		return failAll(fmt.Sprintf("the route needs the model at %d distinct points, more than the %d one call fetches", len(points), assistantTidalMaxPoints))
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, assistantTidalFetchParallel)
launch:
	for _, p := range points {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break launch
		}
		wg.Add(1)
		go func(p tidalGridPoint) {
			defer wg.Done()
			defer func() { <-sem }()
			bundle, ferr := cp.FetchCurrents(ctx, p.lat, p.lon, days)
			var series tidalCurrents
			reason := ""
			switch {
			case ferr != nil:
				reason = fmt.Sprintf("the Open-Meteo current fetch for %s failed: %s", p, firstErrorLine(ferr))
			default:
				for _, h := range bundle.Hourly {
					if h.VelocityMS == nil || h.SetDeg == nil {
						continue
					}
					rad := *h.SetDeg * math.Pi / 180
					series.points = append(series.points, tidalCurrentPoint{t: h.Time, e: *h.VelocityMS * math.Sin(rad), n: *h.VelocityMS * math.Cos(rad)})
				}
				if len(series.points) == 0 {
					reason = fmt.Sprintf("the Open-Meteo model has no current at %s (it is null near shore and over land)", p)
				}
				sort.Slice(series.points, func(i, j int) bool { return series.points[i].t.Before(series.points[j].t) })
			}
			mu.Lock()
			defer mu.Unlock()
			if reason != "" {
				out.reason[p] = reason
			} else {
				out.series[p] = series
			}
		}(p)
	}
	// Stop waiting once ctx is done; a fetch already running finishes on its
	// own and its answer is dropped.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		return tidalPointCurrents{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return tidalPointCurrents{}, err
	}
	cp.PersistCurrents()
	return out, nil
}

// alongRun is the modelled current along the course over one run.
type alongRun struct {
	meanKn, peakKn float64
	fairFraction   float64
	missing        string // set when some sample has no current
}

// alongCourse projects the modelled total current at each sample's point on that
// sample's course, for a run leaving at dep. The peak is the sample of largest
// size, with its sign. It stops at the first sample with no current and says
// which: the point, or the time at it.
func alongCourse(cur tidalPointCurrents, samples []tidalSample, dep time.Time, duration time.Duration, loc *time.Location) alongRun {
	n := len(samples) - 1
	sum, peak := 0.0, 0.0
	fair := 0
	for k, s := range samples {
		if r := cur.reason[s.pt]; r != "" {
			return alongRun{missing: r}
		}
		t := dep.Add(time.Duration(float64(duration) * float64(k) / float64(n)))
		e, nn, ok := cur.series[s.pt].at(t)
		if !ok {
			return alongRun{missing: fmt.Sprintf("the Open-Meteo model has no current at %s for %s", s.pt, boatWeekdayClock(t, loc))}
		}
		rad := s.course * math.Pi / 180
		a := (e*math.Sin(rad) + nn*math.Cos(rad)) * metersPerSecondToKnots
		sum += a
		if a > 0 {
			fair++
		}
		if math.Abs(a) > math.Abs(peak) {
			peak = a
		}
	}
	return alongRun{meanKn: sum / float64(n+1), peakKn: peak, fairFraction: float64(fair) / float64(n+1)}
}

type tidalScored struct {
	dep        time.Time
	mean, fair float64
	run        alongRun
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
	if len(route) >= 2 {
		length := 0.0
		for i := 1; i < len(route); i++ {
			length += haversineMeters(route[i-1].Lat, route[i-1].Lon, route[i].Lat, route[i].Lon)
		}
		if length == 0 {
			return "", fmt.Errorf("plan_tidal_departure: every route point is the same position, so the route has no direction; leave route out to use lat and lon")
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

	earliest := d.now()
	if s := strings.TrimSpace(args.Earliest); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return "", fmt.Errorf("plan_tidal_departure: earliest must be RFC3339: %w", err)
		}
		earliest = t
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
		return "", fmt.Errorf("plan_tidal_departure: latest must be after earliest")
	}
	if latest.Sub(earliest) > assistantTidalDepartureMaxWindow {
		return "", fmt.Errorf("plan_tidal_departure: the departure window may span at most %d hours", int(assistantTidalDepartureMaxWindow/time.Hour))
	}

	st, err := d.resolveTideStation(lat, lon)
	if err != nil {
		return "", fmt.Errorf("plan_tidal_departure: %w", err)
	}

	// Whole minutes keep the arithmetic exact (6.2 h is 372 min, not 372 min
	// less a float rounding error).
	duration := time.Duration(math.Round(hours*60)) * time.Minute
	need := latest.Add(duration)
	cycles, err := tidalHalfCycles(st.chart.Extremes, offset, earliest, need)
	if err != nil {
		return "", fmt.Errorf("plan_tidal_departure: station %q (%s): %w", st.info.Name, st.providerID, err)
	}

	n := int(math.Ceil(float64(duration) / float64(assistantTidalSampleStep)))
	samples := tidalSamples(route, lat, lon, course, n)
	distinct := []tidalGridPoint{}
	seen := map[tidalGridPoint]bool{}
	for _, s := range samples {
		if !seen[s.pt] {
			seen[s.pt] = true
			distinct = append(distinct, s.pt)
		}
	}
	currents, err := d.fetchTidalCurrents(ctx, distinct, d.now(), need)
	if err != nil {
		return "", err
	}

	var deps []time.Time
	for dep := earliest; !dep.After(latest); dep = dep.Add(assistantTidalDepartureStep) {
		deps = append(deps, dep)
	}
	runs := make([]alongRun, len(deps))
	covered := true
	firstMissing := ""
	for i, dep := range deps {
		runs[i] = alongCourse(currents, samples, dep, duration, st.loc)
		if runs[i].missing != "" {
			covered = false
			if firstMissing == "" {
				firstMissing = runs[i].missing
			}
		}
	}

	// One method for every candidate: the model only where it covers them all.
	basis := assistantTidalBasisModel
	missingReason := ""
	if !covered {
		basis = assistantTidalBasisPhase
		missingReason = firstMissing
		if !suppliedSet {
			return "", fmt.Errorf("plan_tidal_departure: the Open-Meteo model has no current for the whole run (%s) and flood_set_deg was not given, so there is nothing to rank with; give flood_set_deg and its flood_set_source from the standing notes or the operator, or say the stream cannot be timed", missingReason)
		}
	}

	var floodSet, along float64
	if basis == assistantTidalBasisPhase {
		floodSet = *args.FloodSetDeg
		along = math.Cos((course - floodSet) * math.Pi / 180)
	}

	scored := make([]tidalScored, len(deps))
	for i, dep := range deps {
		sc := tidalScored{dep: dep, run: runs[i]}
		if basis == assistantTidalBasisModel {
			sc.mean, sc.fair = runs[i].meanKn, runs[i].fairFraction
		} else {
			sum, fair := 0.0, 0
			for k := 0; k <= n; k++ {
				t := dep.Add(time.Duration(float64(duration) * float64(k) / float64(n)))
				a := tidalAssist(cycles, t, along)
				sum += a
				if a > 0 {
					fair++
				}
			}
			sc.mean, sc.fair = sum/float64(n+1), float64(fair)/float64(n+1)
		}
		scored[i] = sc
	}

	// Local maxima (plateau-tolerant, the window edges counting against their
	// one neighbour), best first, kept only if at least two hours from every
	// one already chosen so the list is distinct departures, not adjacent slots.
	var peaks []tidalScored
	for i, c := range scored {
		if (i > 0 && scored[i-1].mean > c.mean) || (i+1 < len(scored) && scored[i+1].mean > c.mean) {
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
	worst := scored[0]
	for _, c := range scored[1:] {
		if c.mean < worst.mean {
			worst = c
		}
	}

	build := func(c tidalScored) assistantTidalCandidate {
		arr := c.dep.Add(duration)
		phases := tidalPhasesCrossed(cycles, c.dep, arr, st.loc)
		names := make([]string, len(phases))
		for i, p := range phases {
			names[i] = p.Phase
		}
		cand := assistantTidalCandidate{
			Departure:    boatDayClock(c.dep, st.loc),
			DepartureISO: c.dep.UTC().Format(time.RFC3339),
			Arrival:      boatDayClock(arr, st.loc),
			ArrivalISO:   arr.UTC().Format(time.RFC3339),
			FairFraction: roundTo2(c.fair),
			Stream:       strings.Join(names, " then "),
			Phases:       phases,
		}
		if basis == assistantTidalBasisPhase {
			m := roundTo2(c.mean)
			cand.MeanAssist = &m
		}
		if c.run.missing != "" {
			cand.AlongCourseMissing = c.run.missing
			return cand
		}
		mean, peak := roundTo2(c.run.meanKn), roundTo2(c.run.peakKn)
		cand.MeanAlongCourseKn, cand.PeakAlongCourseKn = &mean, &peak
		return cand
	}

	result := assistantTidalDepartureResult{
		Station:                st.info,
		Provider:               st.providerID,
		HeightDatum:            st.datum,
		Modelled:               st.followsPos,
		TimeBasis:              st.timeBasis,
		CourseDeg:              course,
		RankingBasis:           basis,
		ModelledCurrentMissing: missingReason,
		RoutePoints:            len(route),
		SlackOffsetMin:         offsetMin,
		PassageHours:           hours,
		Best:                   make([]assistantTidalCandidate, 0, len(chosen)),
		Worst:                  build(worst),
		Basis:                  assistantTidalBasis,
		Reading:                readingTidalDeparture,
	}
	for _, c := range chosen {
		result.Best = append(result.Best, build(c))
	}
	if basis == assistantTidalBasisPhase {
		fs := floodSet
		result.FloodSetDeg, result.FloodSetSource = &fs, args.FloodSetSource
		if math.Abs(along) < assistantTidalCrossStreamCos {
			result.Note = "The course runs mostly across the stream, so departure timing barely changes the help along the track."
		}
	} else if suppliedSet {
		result.Note = "The flood direction given was not used: the modelled current covers the whole run, so the departures are ranked on it."
	}
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
		Description: "Rank departure times by how much of a passage runs with a fair stream. It follows the " +
			"Open-Meteo model's total current (tide plus ocean currents such as the East Australian Current) along the " +
			"route and ranks by it where the model covers the whole run (ranking_basis modelled_current). Where it " +
			"does not, it times the stream from the nearest tide station's high and low water and the direction you " +
			"give for the flood (ranking_basis tide_phase). Call it after estimate_passage, with estimate_passage's " +
			"course_deg as course_deg, the estimated hours as passage_hours, and its route as route when it returned " +
			"one. Give flood_set_deg and flood_set_source only from the operator's standing notes or the operator " +
			"(standing_notes, operator), or general knowledge you are sure of (general_knowledge): it is used only when " +
			"the model cannot cover the run, so omit it when you do not know it. If the model cannot cover the run and " +
			"you gave no direction the call fails and says what is missing: then say the stream cannot be timed. " +
			ruleTidalDeparture +
			" Do not use it for a trip that starts later than the forecast covers or somewhere other than here. " +
			"The result's reading says how to read the stream against the wind.",
		Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"lat": {"type": "number", "description": "Latitude of the departure position, used to find the tide station and, with no route, the point the current is read at."},
						"lon": {"type": "number", "description": "Longitude of the departure position."},
						"course_deg": {"type": "number", "description": "Passage course in degrees true, 0 to 360 (estimate_passage's course_deg, the longest leg's bearing). Used for the tide-phase method, and for the current when there is no route."},
						"passage_hours": {"type": "number", "description": "Passage duration in hours, greater than 0 and at most 24 (from estimate_passage)."},
						"route": {
							"type": "array",
							"description": "The route points in order, as estimate_passage returns them (each with lat and lon, at least two). The boat is taken along it at a steady speed and the current is read at each point with the course of the leg it is on. Omit it to use lat and lon with course_deg.",
							"items": {
								"type": "object",
								"properties": {"lat": {"type": "number"}, "lon": {"type": "number"}},
								"required": ["lat", "lon"]
							}
						},
						"flood_set_deg": {"type": "number", "description": "Optional. Direction in degrees true that the flood stream sets toward, 0 to 360, only from standing notes or the operator, or general knowledge you are sure of. Used only when the model cannot cover the run."},
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
