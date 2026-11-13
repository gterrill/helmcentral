package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"
)

// The departure ranking behind plan_tidal_departure, as a typed function: the
// tool turns its JSON arguments into a tidalRankQuery and its result into JSON,
// and a passage plan calls rankTidalDepartures directly. Every error here is
// unprefixed; each caller says which tool or page it was working for.

// tidalRankQuery is a ranking's input: the run, and the window of departures to
// try every assistantTidalDepartureStep. Its values are already checked.
type tidalRankQuery struct {
	Lat, Lon, CourseDeg float64
	Hours               float64
	Route               []assistantLatLon
	FloodSetDeg         *float64
	FloodSetSource      string
	SlackOffset         time.Duration
	// Earliest and Latest bound the departures, both included.
	Earliest, Latest time.Time
	// Now is when the ranking is made, which sets how far ahead the current model
	// is asked for.
	Now time.Time
}

// tidalRanking is every departure in the window with its score, and what is
// needed to say how they were ranked.
type tidalRanking struct {
	q        tidalRankQuery
	basis    string
	duration time.Duration
	// noData says what lacked data when the model could not rank the run; empty
	// when it did.
	noData string
	// scored holds every departure on the grid, in time order.
	scored []tidalScored
	notes  []string
	// samples and currents are the run and the model's current along it.
	samples  []tidalSample
	currents tidalPointCurrents

	// The tide's side, set under tide_phase only.
	station    assistantResolvedTideStation
	cycles     []tidalHalfCycle
	mostAcross bool
	loc        *time.Location
	timeBasis  string
}

// planCurrentReading is the modelled current along a leg. Have is false, with
// Why, when the model had none for enough of it.
type planCurrentReading struct {
	Have   bool
	MeanKn float64
	Why    string
}

// currentReading is what a run's modelled current says: the mean along the
// course, positive with the boat. A run the model covers less than half of has no
// reading, and a run it covers none of is not 0 kn.
func currentReading(run alongRun, cur tidalPointCurrents, samples []tidalSample) planCurrentReading {
	switch {
	case run.covered == 0:
		return planCurrentReading{Why: noModelDataReason(cur, samples)}
	case run.coveredFraction() < assistantTidalMinCoveredFraction:
		return planCurrentReading{Why: fmt.Sprintf("the model has a current for only %d%% of the leg", int(math.Round(run.coveredFraction()*100)))}
	}
	return planCurrentReading{Have: true, MeanKn: roundTo2(run.meanKn)}
}

// tidalRunDuration is how long a run of hours takes, in whole minutes so the
// arithmetic is exact (6.2 h is 372 min, not 372 min less a float rounding error).
func tidalRunDuration(hours float64) time.Duration {
	return time.Duration(math.Round(hours*60)) * time.Minute
}

// errTidalNothingToRank is a ranking that has no current from the model and no
// flood direction to time the tide by.
type errTidalNothingToRank struct{ reason string }

func (e errTidalNothingToRank) Error() string {
	return fmt.Sprintf("the Open-Meteo model has no current for the run (%s) and flood_set_deg was not given, so there is nothing to rank with; give flood_set_deg and its flood_set_source from the standing notes, the operator or general knowledge, or say the stream cannot be timed", e.reason)
}

// rankTidalDepartures scores every departure in the query's window. Under the
// model's current it ranks on the mean along-course current; only where the model
// has none for any departure does it time the stream from the tide, which needs
// the flood direction in the query, and fails without it.
func (d assistantToolDeps) rankTidalDepartures(ctx context.Context, q tidalRankQuery) (*tidalRanking, error) {
	route := q.Route
	// Whole minutes keep the arithmetic exact.
	duration := tidalRunDuration(q.Hours)
	need := q.Latest.Add(duration)
	r := &tidalRanking{q: q, duration: duration}

	// The route may start in open water off the creek or berth the boat is in, so
	// a start away from lat/lon is said, not refused, until it is a different passage.
	if len(route) >= 2 {
		off := haversineMeters(q.Lat, q.Lon, route[0].Lat, route[0].Lon) / metersPerNauticalMile
		if off > assistantTidalRouteMaxNm {
			return nil, fmt.Errorf("the route starts at %g, %g, %.1f nm from lat/lon %g, %g; more than %g nm apart is a different passage, so give the route from the departure position",
				route[0].Lat, route[0].Lon, off, q.Lat, q.Lon, assistantTidalRouteMaxNm)
		}
		if off > assistantTidalRouteNoteNm {
			r.notes = append(r.notes, fmt.Sprintf("The route starts %.1f nm from lat/lon, which is not on it. The stream was read along the route.", off))
		}
	}

	n := int(math.Ceil(float64(duration) / float64(assistantTidalSampleStep)))
	samples, err := tidalSamples(route, q.Lat, q.Lon, q.CourseDeg, n)
	if err != nil {
		return nil, err
	}
	r.samples = samples

	// The model is read along a route. One cell is not the whole run, so with no
	// route the model is not used at all.
	noData := "no route was given, so the current along the passage was not read"
	if len(route) >= 2 {
		noData = ""
		r.currents, err = d.fetchTidalCurrents(ctx, samples, q.Now, need)
		if err != nil {
			return nil, err
		}
	}

	// Every departure is ranked on the model over the samples it has data for. The
	// tide is used only when the model has no data for any sample of any departure.
	distNm := make([]float64, len(samples))
	for k, s := range samples {
		distNm[k] = s.distNm
	}
	anyData := false
	for dep := q.Earliest; !dep.After(q.Latest); dep = dep.Add(assistantTidalDepartureStep) {
		var run alongRun
		if noData == "" {
			run = alongCourse(r.currents, samples, distNm, dep, duration)
			anyData = anyData || run.covered > 0
		}
		r.scored = append(r.scored, tidalScored{dep: dep, run: run})
	}
	r.basis = assistantTidalBasisModel
	if !anyData {
		r.basis = assistantTidalBasisPhase
		if noData == "" {
			noData = noModelDataReason(r.currents, samples)
		}
		if q.FloodSetDeg == nil {
			return nil, errTidalNothingToRank{reason: noData}
		}
		log.Printf("assistant: plan_tidal_departure: tide_phase used because the Open-Meteo model has no current for the run: %s", noData)
	}
	r.noData = noData

	// Tide data is read only by the tide-phase method. Times are in the station's
	// zone then, and in the departure position's zone under the model.
	// Each sample is on its own leg, so a route is scored on the leg courses; with
	// no route every sample has course_deg. alongFactor[k] is how much of the stream
	// the sample feels, and mostAcross says whether the whole run is square to it.
	var alongFactor []float64
	r.mostAcross = true
	if r.basis == assistantTidalBasisPhase {
		r.station, err = d.resolveTideStation(q.Lat, q.Lon)
		if err != nil {
			return nil, err
		}
		r.cycles, err = tidalHalfCycles(r.station.chart.Extremes, q.SlackOffset, q.Earliest, need)
		if err != nil {
			return nil, fmt.Errorf("station %q (%s): %w", r.station.info.Name, r.station.providerID, err)
		}
		r.loc, r.timeBasis = r.station.loc, r.station.timeBasis
		alongFactor = make([]float64, len(samples))
		for k, s := range samples {
			alongFactor[k] = math.Cos((s.course - *q.FloodSetDeg) * math.Pi / 180)
			if math.Abs(alongFactor[k]) >= assistantTidalCrossStreamCos {
				r.mostAcross = false
			}
		}
	} else {
		// Times are the boat's, in the zone at the departure. Only this method needs it.
		z, zerr := tidalZoneAt(q.Lat, q.Lon)
		if zerr != nil {
			return nil, fmt.Errorf("the time zone at the departure position is not known: %w", zerr)
		}
		r.loc, r.timeBasis = z.Loc, z.Name
	}
	// A departure covered less than half is judged on too little of its run to
	// stand against a better covered one. When none is covered half, the ones with
	// data are ranked anyway and the answer says every run was mostly uncovered.
	anyWellCovered := false
	for _, sc := range r.scored {
		if sc.run.coveredFraction() >= assistantTidalMinCoveredFraction && sc.run.covered > 0 {
			anyWellCovered = true
		}
	}
	if r.basis == assistantTidalBasisModel && !anyWellCovered {
		r.notes = append(r.notes, "Every departure's run was mostly uncovered by the model, so these figures rest on a small part of the passage.")
	}
	for i := range r.scored {
		sc := &r.scored[i]
		if r.basis == assistantTidalBasisModel {
			sc.mean, sc.fair = sc.run.meanKn, sc.run.fairFraction
			sc.ineligible = sc.run.covered == 0 || (anyWellCovered && sc.run.coveredFraction() < assistantTidalMinCoveredFraction)
			continue
		}
		sum, fair := 0.0, 0
		for k := 0; k <= n; k++ {
			t := sc.dep.Add(time.Duration(float64(duration) * float64(k) / float64(n)))
			a := tidalAssist(r.cycles, t, alongFactor[k])
			sum += a
			if a > 0 {
				fair++
			}
		}
		sc.mean, sc.fair = sum/float64(n+1), float64(fair)/float64(n+1)
	}
	if r.basis == assistantTidalBasisPhase {
		if r.mostAcross {
			r.notes = append(r.notes, "The course runs mostly across the stream, so departure timing barely changes the help along the track.")
		}
	} else if q.FloodSetDeg != nil {
		r.notes = append(r.notes, "The flood direction given was not used: the modelled current covers the run, so the departures are ranked on it.")
	}
	return r, nil
}

// best is the departures worth naming: local maxima (plateau-tolerant, the window
// edges counting against their one neighbour), best first, kept only if at least
// two hours from every one already chosen so the list is distinct departures, not
// adjacent slots.
func (r *tidalRanking) best() []tidalScored {
	scored := r.scored
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
	return chosen
}

// worst is the departure to avoid: the worst one that has data to judge it by.
func (r *tidalRanking) worst() tidalScored {
	var worst tidalScored
	haveWorst := false
	for _, c := range r.scored {
		if c.ineligible {
			continue
		}
		if !haveWorst || c.mean < worst.mean {
			worst, haveWorst = c, true
		}
	}
	return worst
}

// tidalDeparture is one departure a plan may take, with the modelled current
// along the run at that time.
type tidalDeparture struct {
	Depart  time.Time
	Current planCurrentReading
}

// departures is every departure the model had a current for, fairest stream first
// and earlier first among equals. It is the ranking in knots, so it needs the
// model's current: a ranking made from the tide has no current to report, and
// that is an error.
func (r *tidalRanking) departures() ([]tidalDeparture, error) {
	if r.basis != assistantTidalBasisModel {
		return nil, fmt.Errorf("the departures were ranked from the tide, which gives no current in knots")
	}
	var out []tidalDeparture
	order := make([]tidalScored, 0, len(r.scored))
	for _, sc := range r.scored {
		if !sc.ineligible {
			order = append(order, sc)
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return order[i].mean > order[j].mean })
	for _, sc := range order {
		out = append(out, tidalDeparture{Depart: sc.dep, Current: currentReading(sc.run, r.currents, r.samples)})
	}
	return out, nil
}

// candidate is one departure as plan_tidal_departure's result gives it.
func (r *tidalRanking) candidate(c tidalScored) assistantTidalCandidate {
	arr := c.dep.Add(r.duration)
	cand := assistantTidalCandidate{
		Departure:    boatDayClock(c.dep, r.loc),
		DepartureISO: c.dep.UTC().Format(time.RFC3339),
		Arrival:      boatDayClock(arr, r.loc),
		ArrivalISO:   arr.UTC().Format(time.RFC3339),
		FairFraction: roundTo2(c.fair),
	}
	if r.basis == assistantTidalBasisPhase {
		m := roundTo2(c.mean)
		cand.MeanAssist = &m
		cand.Phases = tidalPhasesCrossed(r.cycles, c.dep, arr, r.loc)
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

// result is the ranking as plan_tidal_departure returns it.
func (r *tidalRanking) result() assistantTidalDepartureResult {
	basisText, modelMissing := assistantTidalBasisModelText, ""
	if r.basis == assistantTidalBasisPhase {
		basisText, modelMissing = assistantTidalBasisPhaseText, r.noData
	}
	chosen := r.best()
	result := assistantTidalDepartureResult{
		TimeBasis:              r.timeBasis,
		CourseDeg:              r.q.CourseDeg,
		RankingBasis:           r.basis,
		ModelledCurrentMissing: modelMissing,
		RoutePoints:            len(r.q.Route),
		SlackOffsetMin:         int(r.q.SlackOffset / time.Minute),
		PassageHours:           r.q.Hours,
		Best:                   make([]assistantTidalCandidate, 0, len(chosen)),
		Worst:                  r.candidate(r.worst()),
		Basis:                  basisText,
		Reading:                readingTidalDeparture,
		Note:                   strings.Join(r.notes, " "),
	}
	for _, c := range chosen {
		result.Best = append(result.Best, r.candidate(c))
	}
	if r.basis == assistantTidalBasisPhase {
		result.Station, result.Provider = &r.station.info, r.station.providerID
		result.HeightDatum, result.Modelled = r.station.datum, r.station.followsPos
		result.FloodSetDeg, result.FloodSetSource = r.q.FloodSetDeg, r.q.FloodSetSource
	}
	return result
}
