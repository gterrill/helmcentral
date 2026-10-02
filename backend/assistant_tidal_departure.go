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

// This file implements plan_tidal_departure, the assistant tool that ranks
// departure times by how much of a passage runs with a fair tidal stream.
//
// Tide plugins give high and low water times and heights, never stream
// direction or rate, so the tool models only the stream's PHASE and takes the
// SET direction from the caller (standing notes or general knowledge, labelled
// by flood_set_source). Rate is not modelled.
//
// The model, kept deliberately simple and deterministic:
//
//   - Flood runs from each LW to the next HW, ebb from each HW to the next LW,
//     both shifted by slack_offset_min (the stream turning later or earlier
//     than the water level does).
//   - Inside a half-cycle of length T, strength s(t) = sin(pi * t / T): zero at
//     slack, peak mid-cycle. Positive for flood, negative for ebb.
//   - The along-track factor is a(t) = s(t) * cos(course - flood_set). Positive
//     means fair (the stream carries the boat along its course), negative foul.
//   - Departures are tried every 15 minutes across the window. Each is scored by
//     sampling a(t) about every 5 minutes over the passage: mean_assist (-1..1)
//     and fair_fraction (the share of samples with a > 0).

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
	assistantTidalTimeLayout     = "Mon 2 Jan 15:04"
	assistantTidalBasis          = "Stream phase comes from the station's high and low water times; the set direction is supplied (see flood_set_source); stream rate is not modelled."
)

type assistantTidalDepartureArgs struct {
	Lat            *float64 `json:"lat"`
	Lon            *float64 `json:"lon"`
	CourseDeg      *float64 `json:"course_deg"`
	PassageHours   *float64 `json:"passage_hours"`
	FloodSetDeg    *float64 `json:"flood_set_deg"`
	FloodSetSource string   `json:"flood_set_source"`
	SlackOffsetMin int      `json:"slack_offset_min"`
	Earliest       string   `json:"earliest"`
	Latest         string   `json:"latest"`
}

type assistantStreamPhase struct {
	Phase string `json:"phase"`
	From  string `json:"from"`
	To    string `json:"to"`
}

type assistantTidalCandidate struct {
	Departure    string                 `json:"departure"`
	DepartureISO string                 `json:"departure_iso"`
	Arrival      string                 `json:"arrival"`
	ArrivalISO   string                 `json:"arrival_iso"`
	MeanAssist   float64                `json:"mean_assist"`
	FairFraction float64                `json:"fair_fraction"`
	Stream       string                 `json:"stream"`
	Phases       []assistantStreamPhase `json:"phases"`
}

type assistantTidalDepartureResult struct {
	Station        assistantStationInfo      `json:"station"`
	Provider       string                    `json:"provider"`
	TimeBasis      string                    `json:"time_basis"`
	CourseDeg      float64                   `json:"course_deg"`
	FloodSetDeg    float64                   `json:"flood_set_deg"`
	FloodSetSource string                    `json:"flood_set_source"`
	SlackOffsetMin int                       `json:"slack_offset_min"`
	PassageHours   float64                   `json:"passage_hours"`
	Best           []assistantTidalCandidate `json:"best"`
	Worst          assistantTidalCandidate   `json:"worst"`
	Note           string                    `json:"note,omitempty"`
	Basis          string                    `json:"basis"`
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

type tidalScored struct {
	dep        time.Time
	mean, fair float64
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
	}{{"lat", args.Lat}, {"lon", args.Lon}, {"course_deg", args.CourseDeg}, {"passage_hours", args.PassageHours}, {"flood_set_deg", args.FloodSetDeg}} {
		if req.v == nil {
			return "", fmt.Errorf("plan_tidal_departure: %s is required", req.name)
		}
	}
	lat, lon, course, hours, floodSet := *args.Lat, *args.Lon, *args.CourseDeg, *args.PassageHours, *args.FloodSetDeg
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return "", fmt.Errorf("plan_tidal_departure: lat/lon is not a valid position")
	}
	if course < 0 || course > 360 {
		return "", fmt.Errorf("plan_tidal_departure: course_deg must be between 0 and 360")
	}
	if floodSet < 0 || floodSet > 360 {
		return "", fmt.Errorf("plan_tidal_departure: flood_set_deg must be between 0 and 360")
	}
	// The passage is sampled in whole minutes, so anything under one rounds
	// to zero and the mean divides 0 by 0.
	if !(hours >= 1.0/60) || hours > assistantTidalDepartureMaxHours {
		return "", fmt.Errorf("plan_tidal_departure: passage_hours must be at least one minute and at most %d", assistantTidalDepartureMaxHours)
	}
	switch args.FloodSetSource {
	case "standing_notes", "general_knowledge", "operator":
	default:
		return "", fmt.Errorf("plan_tidal_departure: flood_set_source must be standing_notes, general_knowledge or operator")
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

	st, err := d.resolveTideStation("plan_tidal_departure", lat, lon)
	if err != nil {
		return "", err
	}

	// Whole minutes keep the arithmetic exact (6.2 h is 372 min, not 372 min
	// less a float rounding error).
	duration := time.Duration(math.Round(hours*60)) * time.Minute
	need := latest.Add(duration)
	cycles, err := tidalHalfCycles(st.chart.Extremes, offset, earliest, need)
	if err != nil {
		return "", fmt.Errorf("plan_tidal_departure: station %q (%s): %w", st.info.Name, st.providerID, err)
	}

	along := math.Cos((course - floodSet) * math.Pi / 180)

	var scored []tidalScored
	for dep := earliest; !dep.After(latest); dep = dep.Add(assistantTidalDepartureStep) {
		n := int(math.Ceil(float64(duration) / float64(assistantTidalSampleStep)))
		sum, fair := 0.0, 0
		for k := 0; k <= n; k++ {
			t := dep.Add(time.Duration(float64(duration) * float64(k) / float64(n)))
			a := tidalAssist(cycles, t, along)
			sum += a
			if a > 0 {
				fair++
			}
		}
		scored = append(scored, tidalScored{dep: dep, mean: sum / float64(n+1), fair: float64(fair) / float64(n+1)})
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
		return assistantTidalCandidate{
			Departure:    c.dep.In(st.loc).Format(assistantTidalTimeLayout),
			DepartureISO: c.dep.UTC().Format(time.RFC3339),
			Arrival:      arr.In(st.loc).Format(assistantTidalTimeLayout),
			ArrivalISO:   arr.UTC().Format(time.RFC3339),
			MeanAssist:   roundTo2(c.mean),
			FairFraction: roundTo2(c.fair),
			Stream:       strings.Join(names, " then "),
			Phases:       phases,
		}
	}

	result := assistantTidalDepartureResult{
		Station:        st.info,
		Provider:       st.providerID,
		TimeBasis:      st.timeBasis,
		CourseDeg:      course,
		FloodSetDeg:    floodSet,
		FloodSetSource: args.FloodSetSource,
		SlackOffsetMin: offsetMin,
		PassageHours:   hours,
		Best:           make([]assistantTidalCandidate, 0, len(chosen)),
		Worst:          build(worst),
		Basis:          assistantTidalBasis,
	}
	for _, c := range chosen {
		result.Best = append(result.Best, build(c))
	}
	if math.Abs(along) < assistantTidalCrossStreamCos {
		result.Note = "The course runs mostly across the stream, so departure timing barely changes the help along the track."
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
		out = append(out, assistantStreamPhase{Phase: name, From: from.In(loc).Format("Mon 15:04"), To: to.In(loc).Format("Mon 15:04")})
	}
	return out
}
