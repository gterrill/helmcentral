package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// assess_passage_window (ADR 0191) judges an itinerary of legs against the
// operator's own passage limits, one row per candidate departure date and one
// cell per leg on its scheduled date. The assessment is a pure function; the
// tool wraps it with the configured providers and lifts the result into a
// passage_window card.

type passageRoutePoint struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Name string  `json:"name"`
}

type passageLegArgs struct {
	Name         string              `json:"name"`
	Route        []passageRoutePoint `json:"route"`
	PassageHours *float64            `json:"passage_hours"`
	SpeedKn      *float64            `json:"speed_kn"`
	FloodSetDeg  *float64            `json:"flood_set_deg"`
	DepartTime   string              `json:"depart_time"`
}

type passageWindowArgs struct {
	Legs           []passageLegArgs `json:"legs"`
	StaysNights    []int            `json:"stays_nights"`
	DepartureDates []string         `json:"departure_dates"`
	DepartTime     string           `json:"depart_time"`
}

type passageWindowLimiting struct {
	Factor string  `json:"factor"`
	Sector *string `json:"sector"`
	Text   string  `json:"text"`
}

type passageWindowCell struct {
	Leg        int                    `json:"leg"`
	Date       string                 `json:"date"`
	Depart     string                 `json:"depart"`
	Arrive     string                 `json:"arrive"`
	State      string                 `json:"state"`
	Limiting   *passageWindowLimiting `json:"limiting"`
	Note       string                 `json:"note,omitempty"`
	NotChecked []string               `json:"not_checked"`
}

type passageWindowRow struct {
	DepartDate string              `json:"depart_date"`
	Verdict    string              `json:"verdict"`
	Cells      []passageWindowCell `json:"cells"`
}

type passageWindowLeg struct {
	Name            string `json:"name"`
	StayNightsAfter *int   `json:"stay_nights_after"`
}

type passageWindowPayload struct {
	Timezone          string             `json:"timezone"`
	ForecastFetchedAt string             `json:"forecast_fetched_at"`
	Provider          string             `json:"provider"`
	WaveProvider      string             `json:"wave_provider"`
	Legs              []passageWindowLeg `json:"legs"`
	Rows              []passageWindowRow `json:"rows"`
}

// passageHour is one forecast hour at one point, in SI units.
type passageHour struct {
	WindMS, GustMS, WindDirDeg float64
	HasWave                    bool
	WaveM, WaveDirDeg          float64
	HasWaveDir                 bool
	HasWavePeriod              bool
	WavePeriodS                float64
}

// passageForecast is one point's hourly forecast keyed by the unix time of the
// hour, and when the provider data it came from was fetched.
type passageForecast struct {
	Hours     map[int64]passageHour
	FetchedAt time.Time
}

// forecastSource yields hourly forecasts.
type forecastSource struct {
	Provider, WaveProvider string
	Fetch                  func(ctx context.Context, lat, lon float64) (passageForecast, error)
}

type tideSource func(ctx context.Context, lat, lon float64) ([]tideExtremePoint, error)

const (
	passageWindowMaxLegs        = 6
	passageWindowMaxStayNights  = 14
	passageWindowSampleSplitNM  = 30.0
	passageWindowTideMinAssist  = 0.5
	passageWindowTideWindowDeg  = 60.0
	passageWindowNoForecastNote = "No forecast this far ahead"
	passageWindowInstruction    = "A card showing this grid is attached; do not repeat it as a table — say which days look best and why."
)

var passageClockRE = regexp.MustCompile(`^([01]\d|2[0-3]):([0-5]\d)$`)

func passageClock(s string) (h, m int, ok bool) {
	g := passageClockRE.FindStringSubmatch(s)
	if g == nil {
		return 0, 0, false
	}
	fmt.Sscanf(g[1]+" "+g[2], "%d %d", &h, &m)
	return h, m, true
}

// parsePassageWindowArgs decodes strictly (an unknown argument is an error)
// and validates; every message names the argument.
func parsePassageWindowArgs(raw json.RawMessage) (passageWindowArgs, error) {
	var a passageWindowArgs
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return passageWindowArgs{}, fmt.Errorf("assess_passage_window: arguments: %w", err)
	}
	if err := a.validate(); err != nil {
		return passageWindowArgs{}, err
	}
	return a, nil
}

// legRoutes is each leg's route points as given, kept with the card so a later
// match can find the boat on the planned track (ADR 0211).
func legRoutes(a passageWindowArgs) [][]passageRoutePoint {
	out := make([][]passageRoutePoint, len(a.Legs))
	for i, l := range a.Legs {
		out[i] = l.Route
	}
	return out
}

func (a passageWindowArgs) validate() error {
	fail := func(format string, v ...any) error {
		return fmt.Errorf("assess_passage_window: "+format, v...)
	}
	if n := len(a.Legs); n < 1 || n > passageWindowMaxLegs {
		return fail("legs must hold 1 to %d legs, got %d", passageWindowMaxLegs, n)
	}
	for i, l := range a.Legs {
		if n := len(l.Route); n < 2 || n > 10 {
			return fail("legs[%d].route must hold 2 to 10 points (start, any vias, end), got %d", i, n)
		}
		for j, p := range l.Route {
			if p.Lat < -90 || p.Lat > 90 {
				return fail("legs[%d].route[%d].lat must be between -90 and 90", i, j)
			}
			if p.Lon < -180 || p.Lon > 180 {
				return fail("legs[%d].route[%d].lon must be between -180 and 180", i, j)
			}
		}
		if l.PassageHours == nil && l.SpeedKn == nil {
			return fail("legs[%d]: give passage_hours or speed_kn", i)
		}
		if l.PassageHours != nil && !(*l.PassageHours >= 0.5 && *l.PassageHours <= 36) {
			return fail("legs[%d].passage_hours must be between 0.5 and 36", i)
		}
		// With both given, passage_hours is used and speed_kn is not read, so
		// it is not range-checked either (ignoredSpeedNotes says so).
		if l.PassageHours == nil && !(*l.SpeedKn >= 1 && *l.SpeedKn <= 25) {
			return fail("legs[%d].speed_kn must be between 1 and 25", i)
		}
		if l.FloodSetDeg != nil && !(*l.FloodSetDeg >= 0 && *l.FloodSetDeg <= 360) {
			return fail("legs[%d].flood_set_deg must be between 0 and 360", i)
		}
		if l.DepartTime != "" {
			if _, _, ok := passageClock(l.DepartTime); !ok {
				return fail("legs[%d].depart_time must be HH:MM, 24-hour local", i)
			}
		}
	}
	if len(a.StaysNights) != len(a.Legs)-1 {
		return fail("stays_nights must have one entry per gap between legs (%d), got %d", len(a.Legs)-1, len(a.StaysNights))
	}
	for i, n := range a.StaysNights {
		if n < 0 || n > passageWindowMaxStayNights {
			return fail("stays_nights[%d] must be between 0 and %d", i, passageWindowMaxStayNights)
		}
	}
	if n := len(a.DepartureDates); n < 1 || n > 7 {
		return fail("departure_dates must hold 1 to 7 dates, got %d", n)
	}
	seenDates := map[string]bool{}
	for i, d := range a.DepartureDates {
		if _, err := time.Parse("2006-01-02", d); err != nil {
			return fail("departure_dates[%d] must be YYYY-MM-DD", i)
		}
		if seenDates[d] {
			return fail("departure_dates[%d] is a duplicate of %s; list each date once", i, d)
		}
		seenDates[d] = true
	}
	if a.DepartTime == "" {
		return fail("depart_time is required (HH:MM, 24-hour local)")
	}
	if _, _, ok := passageClock(a.DepartTime); !ok {
		return fail("depart_time must be HH:MM, 24-hour local")
	}
	return nil
}

// passageRowVerdict is a row's worst cell: no_go, then no_data, marginal, go.
func passageRowVerdict(states []string) string {
	has := map[string]bool{}
	for _, s := range states {
		has[s] = true
	}
	switch {
	case has["no_go"]:
		return "no_go"
	case has["no_data"]:
		return "no_data"
	case has["marginal"]:
		return "marginal"
	}
	return "go"
}

func (l passageLimits) sector(name string) passageSectorLimits {
	switch name {
	case "ahead":
		return l.Ahead
	case "abeam":
		return l.Abeam
	}
	return l.Astern
}

func (l passageLimits) anyWindNoGo() bool {
	return l.Ahead.WindKnNoGo != nil || l.Abeam.WindKnNoGo != nil || l.Astern.WindKnNoGo != nil
}

// passageSector is the true wind angle's sector: ahead under 60 degrees,
// abeam to 120, astern beyond.
func passageSector(angle float64) string {
	switch {
	case angle < 60:
		return "ahead"
	case angle < 120:
		return "abeam"
	}
	return "astern"
}

// lerpLon is the longitude u of the way from a to b by the shorter way round,
// so a route over the 180th meridian does not swing through Greenwich.
func lerpLon(a, b, u float64) float64 {
	d := math.Mod(b-a, 360)
	if d > 180 {
		d -= 360
	} else if d < -180 {
		d += 360
	}
	lon := math.Mod(a+d*u, 360)
	if lon > 180 {
		lon -= 360
	} else if lon < -180 {
		lon += 360
	}
	return lon
}

// angleBetween is the smallest angle between two compass directions, 0-180.
func angleBetween(a, b float64) float64 {
	d := math.Mod(math.Abs(a-b), 360)
	if d > 180 {
		d = 360 - d
	}
	return d
}

type passageSegment struct {
	from, to        passageRoutePoint
	startM, lengthM float64
	bearing         float64
	label           string // "from → to", or the leg name when a point is unnamed
}

type passageLegPlan struct {
	name     string
	args     passageLegArgs
	segs     []passageSegment
	totalM   float64
	hours    float64
	samples  []passageRoutePoint
	dailyEnd passageRoutePoint
}

func planPassageLeg(i int, l passageLegArgs) passageLegPlan {
	first, last := l.Route[0], l.Route[len(l.Route)-1]
	name := strings.TrimSpace(l.Name)
	if name == "" {
		if first.Name != "" && last.Name != "" {
			name = first.Name + " → " + last.Name
		} else {
			name = fmt.Sprintf("Leg %d", i+1)
		}
	}
	p := passageLegPlan{name: name, args: l, dailyEnd: last}
	for j := 0; j+1 < len(l.Route); j++ {
		a, b := l.Route[j], l.Route[j+1]
		seg := passageSegment{from: a, to: b, startM: p.totalM, lengthM: haversineMeters(a.Lat, a.Lon, b.Lat, b.Lon), bearing: bearingDeg(a.Lat, a.Lon, b.Lat, b.Lon), label: name}
		if a.Name != "" && b.Name != "" {
			seg.label = a.Name + " → " + b.Name
		}
		p.totalM += seg.lengthM
		p.segs = append(p.segs, seg)
		p.samples = append(p.samples, a)
		if seg.lengthM/metersPerNauticalMile > passageWindowSampleSplitNM {
			p.samples = append(p.samples, passageRoutePoint{Lat: (a.Lat + b.Lat) / 2, Lon: lerpLon(a.Lon, b.Lon, 0.5)})
		}
	}
	p.samples = append(p.samples, last)
	if l.PassageHours != nil {
		p.hours = *l.PassageHours
	} else {
		p.hours = p.totalM / metersPerNauticalMile / *l.SpeedKn
	}
	return p
}

// at is the position and segment a fraction of the way along the route.
func (p passageLegPlan) at(frac float64) (lat, lon float64, seg passageSegment) {
	d := math.Max(0, math.Min(1, frac)) * p.totalM
	idx := len(p.segs) - 1
	for i, s := range p.segs {
		if d < s.startM+s.lengthM {
			idx = i
			break
		}
	}
	s := p.segs[idx]
	u := 0.0
	if s.lengthM > 0 {
		u = math.Min(1, (d-s.startM)/s.lengthM)
	}
	return s.from.Lat + (s.to.Lat-s.from.Lat)*u, lerpLon(s.from.Lon, s.to.Lon, u), s
}

func (p passageLegPlan) nearestSample(lat, lon float64) passageRoutePoint {
	best, bd := p.samples[0], math.MaxFloat64
	for _, s := range p.samples {
		if d := haversineMeters(lat, lon, s.Lat, s.Lon); d < bd {
			best, bd = s, d
		}
	}
	return best
}

type passageCandidate struct {
	sev    int
	ratio  float64
	factor string
	sector string
	text   string
}

func (c passageCandidate) beats(o passageCandidate) bool {
	if c.sev != o.sev {
		return c.sev > o.sev
	}
	return c.ratio > o.ratio
}

// judge compares a value with a no-go threshold: 2 no-go, 1 marginal (from
// 70% of no-go), else 0, with the ratio of value to the threshold that
// applied. shown is the value rounded as the card prints it and decides the
// level; raw only orders candidates of the same level, so a 12.4 kn reading
// still outranks a 12.0 kn one.
func judge(shown, raw, noGo float64) (sev int, ratio float64) {
	ratioTo := func(th float64) float64 {
		if th <= 0 {
			return 1e9
		}
		return raw / th
	}
	marginal := marginalOf(noGo)
	switch {
	case shown >= noGo:
		return 2, ratioTo(noGo)
	case shown >= marginal:
		return 1, ratioTo(marginal)
	}
	return 0, ratioTo(marginal)
}

// marginalNote names the threshold that made a reading marginal, so the card
// says what decided it. A no-go or go reading needs no note.
func marginalNote(sev int, noGo float64) string {
	if sev != 1 {
		return ""
	}
	return fmt.Sprintf(" (marginal from %s)", strconv.FormatFloat(marginalOf(noGo), 'f', -1, 64))
}

type passageTideResult struct {
	ext []tideExtremePoint
	err error
}

type passageAssessor struct {
	limits passageLimits
	fc     forecastSource
	tides  tideSource
	sun    func(date time.Time, lat, lon float64) (rise, set time.Time, ok bool)
	loc    *time.Location

	// cache holds every sample point's hours, fetched up front and read-only
	// after. fetchedAt is the oldest source time among them.
	cache     map[string]map[int64]passageHour
	fetchedAt time.Time
	// tideCache holds what each tide start point's one fetch returned. A
	// provider returns the station's own predicted window, not a span asked for.
	tideCache map[string]passageTideResult
	tideGaps  map[string]bool
}

func passagePointKey(pt passageRoutePoint) string { return fmt.Sprintf("%.2f,%.2f", pt.Lat, pt.Lon) }

const passageWindowFetchWorkers = 4

// prefetch fetches the forecast at every distinct point, at most
// passageWindowFetchWorkers at a time. Results land by index, so the outcome
// and the first error reported do not depend on which fetch finished first.
// ctx is checked before every provider call: once it is done no further call
// is made and its error is returned.
func (as *passageAssessor) prefetch(ctx context.Context, pts []passageRoutePoint) error {
	type result struct {
		f   passageForecast
		err error
	}
	out := make([]result, len(pts))
	sem := make(chan struct{}, passageWindowFetchWorkers)
	var wg sync.WaitGroup
launch:
	for i, pt := range pts {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break launch
		}
		if ctx.Err() != nil {
			<-sem
			break
		}
		wg.Add(1)
		go func(i int, pt passageRoutePoint) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := ctx.Err(); err != nil {
				out[i].err = err
				return
			}
			f, err := as.fc.Fetch(ctx, pt.Lat, pt.Lon)
			if err != nil {
				out[i].err = fmt.Errorf("assess_passage_window: forecast at %.2f, %.2f: %w", pt.Lat, pt.Lon, err)
				return
			}
			out[i].f = f
		}(i, pt)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("assess_passage_window: %w", err)
	}
	for i, r := range out {
		if r.err != nil {
			return r.err
		}
		as.cache[passagePointKey(pts[i])] = r.f.Hours
		if !r.f.FetchedAt.IsZero() && (as.fetchedAt.IsZero() || r.f.FetchedAt.Before(as.fetchedAt)) {
			as.fetchedAt = r.f.FetchedAt
		}
	}
	return nil
}

func (as *passageAssessor) hoursAt(pt passageRoutePoint) (map[int64]passageHour, error) {
	h, ok := as.cache[passagePointKey(pt)]
	if !ok {
		return nil, fmt.Errorf("assess_passage_window: no forecast was fetched at %.2f, %.2f", pt.Lat, pt.Lon)
	}
	return h, nil
}

// tideExtremes fetches a start point's tide once. A failure is kept too, so it is not retried per cell.
func (as *passageAssessor) tideExtremes(ctx context.Context, pt passageRoutePoint) ([]tideExtremePoint, error) {
	key := passagePointKey(pt)
	if r, ok := as.tideCache[key]; ok {
		return r.ext, r.err
	}
	ext, err := as.tides(ctx, pt.Lat, pt.Lon)
	as.tideCache[key] = passageTideResult{ext, err}
	return ext, err
}

var passageNotCheckedOrder = []string{"gust", "wave", "wave_direction", "wind_against_tide", "daylight"}

// wavesWithoutDirection judges a wave height against each sector's no-go and
// returns how many sectors have a limit. The candidate is non-nil only when all
// three are set and agree on severity; it is the one with the worst ratio, with
// no sector in its text.
func (as *passageAssessor) wavesWithoutDirection(waveM float64, h passageHour, label string) (*passageCandidate, int) {
	set := 0
	var worst *passageCandidate
	sev := -1
	agree := true
	for _, lim := range []*float64{as.limits.Ahead.WaveMNoGo, as.limits.Abeam.WaveMNoGo, as.limits.Astern.WaveMNoGo} {
		if lim == nil {
			continue
		}
		set++
		s, ratio := judge(waveM, h.WaveM, *lim)
		if sev >= 0 && s != sev {
			agree = false
		}
		sev = s
		c := passageCandidate{s, ratio, "wave", "",
			fmt.Sprintf("Waves %s%s · %s", waveFigures(waveM, h), marginalNote(s, *lim), label)}
		if worst == nil || c.beats(*worst) {
			worst = &c
		}
	}
	if set < 3 || !agree {
		return nil, set
	}
	return worst, set
}

// waveFigures is the wave part of a limiting line: the height, and the period
// beside it when the provider gave one.
func waveFigures(heightM float64, h passageHour) string {
	if h.HasWavePeriod && h.WavePeriodS > 0 {
		return fmt.Sprintf("%.1f m · %d s", heightM, int(math.Round(h.WavePeriodS)))
	}
	return fmt.Sprintf("%.1f m", heightM)
}

func (as *passageAssessor) cell(ctx context.Context, idx int, plan passageLegPlan, depart time.Time) (passageWindowCell, error) {
	arrive := depart.Add(time.Duration(plan.hours * float64(time.Hour)))
	cell := passageWindowCell{
		Leg: idx, Date: depart.Format("2006-01-02"),
		Depart: depart.Format(time.RFC3339), Arrive: arrive.Format(time.RFC3339),
	}
	notChecked := map[string]bool{}
	var worst passageCandidate
	haveWorst := false
	consider := func(c passageCandidate) {
		if !haveWorst || c.beats(worst) {
			worst, haveWorst = c, true
		}
	}
	missing := false
	anyGust := false

	// Wind against tide: only for a leg with a known flood set and a band.
	var cycles []tidalHalfCycle
	tideOn := false
	if plan.args.FloodSetDeg != nil && as.limits.WindAgainstTideOffsetKn != nil {
		ext, err := as.tideExtremes(ctx, plan.args.Route[0])
		if cerr := ctx.Err(); cerr != nil {
			return cell, fmt.Errorf("assess_passage_window: %w", cerr)
		}
		if err == nil {
			cycles, err = tidalHalfCycles(ext, 0, depart.Truncate(time.Hour), arrive)
			if key := passagePointKey(plan.args.Route[0]); err != nil && !as.tideGaps[key+err.Error()] {
				as.tideGaps[key+err.Error()] = true
				log.Printf("assess_passage_window: wind against tide not checked for %s: %v", plan.name, err)
			}
		}
		tideOn = err == nil
	}
	if !tideOn {
		notChecked["wind_against_tide"] = true
	}

	// Forecast values are stamped on the hour, so the two stamps around the
	// arrival both bear on it: the last one at or before and the first one at
	// or after.
	lastStamp := arrive.Truncate(time.Hour)
	if lastStamp.Before(arrive) {
		lastStamp = lastStamp.Add(time.Hour)
	}
	for t := depart.Truncate(time.Hour); !t.After(lastStamp); t = t.Add(time.Hour) {
		frac := 0.0
		if arrive.After(depart) {
			frac = float64(t.Sub(depart)) / float64(arrive.Sub(depart))
		}
		lat, lon, seg := plan.at(frac)
		hours, err := as.hoursAt(plan.nearestSample(lat, lon))
		if err != nil {
			return cell, err
		}
		h, ok := hours[t.Unix()]
		if !ok {
			missing = true
			continue
		}
		// Judged at the precision the card prints, so what the operator
		// reads is what was compared with the limit.
		rawWindKn := h.WindMS * metersPerSecondToKnots
		rawGustKn := h.GustMS * metersPerSecondToKnots
		windKn, gustKn, waveM := math.Round(rawWindKn), math.Round(rawGustKn), roundTo1(h.WaveM)
		sector := passageSector(angleBetween(h.WindDirDeg, seg.bearing))
		lim := as.limits.sector(sector)
		if lim.WindKnNoGo == nil {
			return cell, fmt.Errorf("assess_passage_window: Passage limits have no wind no-go for the %s sector, and %s runs in it. Set it in Settings → Mate → Passage limits", sector, plan.name)
		}
		windNoGo := *lim.WindKnNoGo
		sev, ratio := judge(windKn, rawWindKn, windNoGo)
		consider(passageCandidate{sev, ratio, "wind", sector,
			fmt.Sprintf("Wind %s %d kn %s%s · %s", degreesToDirection(h.WindDirDeg), int(windKn), sector, marginalNote(sev, windNoGo), seg.label)})

		// Providers leave a gust they do not have at 0. An hour at 0 beside
		// hours with a figure is a calm hour (0 cannot exceed a positive
		// limit); only a leg with no gust figure at all is listed as not
		// checked, below.
		if h.GustMS > 0 {
			anyGust = true
			gustNoGo := windNoGo + passageGustAboveWindKn
			sev, ratio = judge(gustKn, rawGustKn, gustNoGo)
			consider(passageCandidate{sev, ratio, "gust", sector,
				fmt.Sprintf("Gusts %d kn %s%s · %s", int(gustKn), sector, marginalNote(sev, gustNoGo), seg.label)})
		}

		switch {
		case !h.HasWave:
			notChecked["wave"] = true
		case h.HasWaveDir:
			waveSector := passageSector(angleBetween(h.WaveDirDeg, seg.bearing))
			if waveNoGo := as.limits.sector(waveSector).WaveMNoGo; waveNoGo != nil {
				sev, ratio := judge(waveM, h.WaveM, *waveNoGo)
				consider(passageCandidate{sev, ratio, "wave", waveSector,
					fmt.Sprintf("Waves %s %s%s · %s", waveFigures(waveM, h), waveSector, marginalNote(sev, *waveNoGo), seg.label)})
			} else {
				notChecked["wave"] = true
			}
		default:
			// Height without a direction. The sector is unknown, so judge the
			// height against every sector's limit. If all three give the same
			// severity, direction cannot change the answer and the worst of
			// them stands. Anything else would be a guess at the direction.
			cand, set := as.wavesWithoutDirection(waveM, h, seg.label)
			switch {
			case set == 0:
				notChecked["wave"] = true
			case set == 3 && cand != nil:
				consider(*cand)
			default:
				notChecked["wave_direction"] = true
			}
		}

		if tideOn {
			tt := t
			if tt.Before(depart) {
				tt = depart
			}
			a := tidalAssist(cycles, tt, 1)
			if math.Abs(a) >= passageWindowTideMinAssist {
				set, phase := *plan.args.FloodSetDeg, "flood"
				if a < 0 {
					set, phase = math.Mod(set+180, 360), "ebb"
				}
				if angleBetween(set, h.WindDirDeg) <= passageWindowTideWindowDeg {
					tideNoGo := windNoGo - *as.limits.WindAgainstTideOffsetKn
					if tideNoGo <= 0 {
						return cell, fmt.Errorf("assess_passage_window: the wind against tide reduction (%g kn) is not below the %s no-go wind (%g kn). Fix it in Settings → Mate → Passage limits", *as.limits.WindAgainstTideOffsetKn, sector, windNoGo)
					}
					sev, ratio := judge(windKn, rawWindKn, tideNoGo)
					consider(passageCandidate{sev, ratio, "wind_against_tide", "",
						fmt.Sprintf("Wind %d kn against the %s%s · %s", int(windKn), phase, marginalNote(sev, tideNoGo), plan.name)})
				}
			}
		}
	}

	if as.limits.DaylightArrival {
		end := plan.dailyEnd
		rise, set, ok := as.sun(arrive.In(as.loc), end.Lat, end.Lon)
		switch {
		case !ok:
			notChecked["daylight"] = true
		case arrive.Before(rise):
			consider(passageCandidate{2, 1, "daylight", "",
				fmt.Sprintf("Arrives %s, before sunrise %s · %s", arrive.In(as.loc).Format("15:04"), rise.In(as.loc).Format("15:04"), plan.name)})
		case arrive.After(set):
			consider(passageCandidate{2, 1, "daylight", "",
				fmt.Sprintf("Arrives %s, after sunset %s · %s", arrive.In(as.loc).Format("15:04"), set.In(as.loc).Format("15:04"), plan.name)})
		}
	}

	switch {
	case haveWorst && worst.sev == 2:
		cell.State = "no_go"
	case missing:
		cell.State = "no_data"
	case haveWorst && worst.sev == 1:
		cell.State = "marginal"
	default:
		cell.State = "go"
	}
	if cell.State == "no_data" {
		cell.Note = passageWindowNoForecastNote
	} else if haveWorst {
		l := &passageWindowLimiting{Factor: worst.factor, Text: worst.text}
		if worst.sector != "" {
			sec := worst.sector
			l.Sector = &sec
		}
		cell.Limiting = l
	}
	if haveWorst && !anyGust {
		notChecked["gust"] = true
	}
	cell.NotChecked = []string{}
	for _, k := range passageNotCheckedOrder {
		if notChecked[k] {
			cell.NotChecked = append(cell.NotChecked, k)
		}
	}
	return cell, nil
}

// assessItinerary schedules every leg for each candidate departure date and
// judges it against the operator's limits. It never fills a gap: a leg with no
// forecast is no_data, and a measure that could not be read is listed in the
// cell's not_checked.
func assessItinerary(ctx context.Context, args passageWindowArgs, limits passageLimits, now time.Time, fc forecastSource, tides tideSource, sun func(date time.Time, lat, lon float64) (rise, set time.Time, ok bool)) (passageWindowPayload, error) {
	if err := args.validate(); err != nil {
		return passageWindowPayload{}, err
	}
	if !limits.anyWindNoGo() {
		return passageWindowPayload{}, fmt.Errorf("assess_passage_window: Passage limits aren't set: Settings → Mate → Passage limits")
	}
	firstPoint := args.Legs[0].Route[0]
	firstZone, err := zoneAt(firstPoint.Lat, firstPoint.Lon)
	if err != nil {
		return passageWindowPayload{}, fmt.Errorf("assess_passage_window: %w", err)
	}
	loc := firstZone.Loc
	as := &passageAssessor{limits: limits, fc: fc, tides: tides, sun: sun, loc: loc,
		cache: map[string]map[int64]passageHour{}, tideCache: map[string]passageTideResult{}, tideGaps: map[string]bool{}}

	plans := make([]passageLegPlan, len(args.Legs))
	out := passageWindowPayload{
		Timezone:     firstZone.Name,
		Provider:     fc.Provider,
		WaveProvider: fc.WaveProvider,
		Legs:         make([]passageWindowLeg, len(args.Legs)),
		Rows:         make([]passageWindowRow, 0, len(args.DepartureDates)),
	}
	for i, l := range args.Legs {
		plans[i] = planPassageLeg(i, l)
		out.Legs[i].Name = plans[i].name
		if i < len(args.StaysNights) {
			n := args.StaysNights[i]
			out.Legs[i].StayNightsAfter = &n
		}
	}

	departAt := func(date time.Time, clock string) time.Time {
		h, m, _ := passageClock(clock)
		return time.Date(date.Year(), date.Month(), date.Day(), h, m, 0, 0, loc)
	}
	clockFor := func(i int) string {
		if c := args.Legs[i].DepartTime; c != "" {
			return c
		}
		return args.DepartTime
	}

	// Schedule first: every leg's departure on every candidate date. The
	// schedule does not depend on any forecast, so the forecast and tide
	// spans the whole itinerary needs are known before the first fetch.
	departs := make([][]time.Time, len(args.DepartureDates))
	for r, ds := range args.DepartureDates {
		day, _ := time.ParseInLocation("2006-01-02", ds, loc)
		depart := departAt(day, clockFor(0))
		for i, plan := range plans {
			departs[r] = append(departs[r], depart)
			if i+1 == len(plans) {
				break
			}
			arrive := depart.Add(time.Duration(plan.hours * float64(time.Hour)))
			ay, am, ad := arrive.Date()
			if n := args.StaysNights[i]; n > 0 {
				depart = departAt(time.Date(ay, am, ad+n, 0, 0, 0, 0, loc), clockFor(i+1))
			} else {
				depart = departAt(time.Date(ay, am, ad, 0, 0, 0, 0, loc), clockFor(i+1))
				if !depart.After(arrive) {
					depart = departAt(time.Date(ay, am, ad+1, 0, 0, 0, 0, loc), clockFor(i+1))
				}
			}
		}
	}
	var points []passageRoutePoint
	seen := map[string]bool{}
	for _, plan := range plans {
		for _, pt := range plan.samples {
			if k := passagePointKey(pt); !seen[k] {
				seen[k] = true
				points = append(points, pt)
			}
		}
	}
	if err := as.prefetch(ctx, points); err != nil {
		return passageWindowPayload{}, err
	}
	fetched := now
	if !as.fetchedAt.IsZero() {
		fetched = as.fetchedAt
	}
	out.ForecastFetchedAt = fetched.In(loc).Format(time.RFC3339)

	for r, ds := range args.DepartureDates {
		row := passageWindowRow{DepartDate: ds, Cells: make([]passageWindowCell, 0, len(plans))}
		states := make([]string, 0, len(plans))
		for i, plan := range plans {
			cell, err := as.cell(ctx, i, plan, departs[r][i])
			if err != nil {
				return passageWindowPayload{}, err
			}
			row.Cells = append(row.Cells, cell)
			states = append(states, cell.State)
		}
		row.Verdict = passageRowVerdict(states)
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

// passageHoursFromBundles keys a point's weather and wave hours by the unix
// time of the hour. A wave hour the provider does not have is absent, never
// zero, and an hour with no weather point is absent too.
func passageHoursFromBundles(wb weatherForecastBundle, vb *waveForecastBundle) map[int64]passageHour {
	out := map[int64]passageHour{}
	for _, hp := range wb.Hourly {
		if hp.Time.IsZero() {
			continue
		}
		out[hp.Time.Truncate(time.Hour).Unix()] = passageHour{WindMS: hp.WindSpeedMS, GustMS: hp.WindGustMS, WindDirDeg: hp.WindDirectionDeg}
	}
	if vb != nil {
		for _, wp := range vb.Hourly {
			k := wp.Time.Truncate(time.Hour).Unix()
			if h, ok := out[k]; ok {
				if wp.WaveHeightM != nil {
					h.HasWave, h.WaveM = true, *wp.WaveHeightM
				}
				if wp.WaveDirectionDeg != nil {
					h.HasWaveDir, h.WaveDirDeg = true, *wp.WaveDirectionDeg
				}
				if wp.WavePeriodS != nil && *wp.WavePeriodS > 0 {
					h.HasWavePeriod, h.WavePeriodS = true, *wp.WavePeriodS
				}
				out[k] = h
			}
		}
	}
	return out
}

// ignoredSpeedNotes names every leg that gave both passage_hours and speed_kn.
// The hours are used; the model is told, so it does not retry the same call.
func ignoredSpeedNotes(a passageWindowArgs) []string {
	var notes []string
	for i, l := range a.Legs {
		if l.PassageHours != nil && l.SpeedKn != nil {
			notes = append(notes, fmt.Sprintf("legs[%d]: speed_kn %g was ignored because passage_hours %g was also given; the hours are used", i, *l.SpeedKn, *l.PassageHours))
		}
	}
	return notes
}

// passageModelResult is what the model reads: a compact summary of the grid
// (every row's verdict, and per cell the state, what limited it and the local
// times), not the card's payload. The card carries the full figures and goes
// out of band (assistantCardSink), so the chat result cap never cuts it.
type passageModelResult struct {
	Legs        []string          `json:"legs"`
	Rows        []passageModelRow `json:"rows"`
	Instruction string            `json:"instruction"`
	Notes       []string          `json:"notes,omitempty"`

	level, next int
}

type passageModelRow struct {
	DepartDate string             `json:"depart_date"`
	Verdict    string             `json:"verdict"`
	Cells      []passageModelCell `json:"cells,omitempty"`
}

type passageModelCell struct {
	Leg   int    `json:"leg"`
	State string `json:"state"`
	Why   string `json:"why,omitempty"`
	Times string `json:"times,omitempty"`
}

func newPassageModelResult(p passageWindowPayload) *passageModelResult {
	m := &passageModelResult{Instruction: passageWindowInstruction, Legs: make([]string, len(p.Legs)), Rows: make([]passageModelRow, len(p.Rows))}
	for i, l := range p.Legs {
		m.Legs[i] = l.Name
	}
	clock := func(s string) string {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return ""
		}
		return t.Format("15:04")
	}
	for i, r := range p.Rows {
		row := passageModelRow{DepartDate: r.DepartDate, Verdict: r.Verdict, Cells: make([]passageModelCell, len(r.Cells))}
		for j, c := range r.Cells {
			mc := passageModelCell{Leg: c.Leg, State: c.State, Why: c.Note}
			if c.Limiting != nil {
				mc.Why = c.Limiting.Text
			}
			if d, a := clock(c.Depart), clock(c.Arrive); d != "" && a != "" {
				mc.Times = d + "→" + a
			}
			row.Cells[j] = mc
		}
		m.Rows[i] = row
	}
	return m
}

// trim removes one level of detail from a row and reports whether anything
// was there to remove: go cells' reasons, then every reason, then the times,
// then the cells themselves. A row's verdict is never removed.
func (r *passageModelRow) trim(level int) bool {
	changed := false
	switch level {
	case 0, 1:
		for i := range r.Cells {
			if r.Cells[i].Why != "" && (level == 1 || r.Cells[i].State == "go") {
				r.Cells[i].Why, changed = "", true
			}
		}
	case 2:
		for i := range r.Cells {
			if r.Cells[i].Times != "" {
				r.Cells[i].Times, changed = "", true
			}
		}
	case 3:
		if r.Cells != nil {
			r.Cells, changed = nil, true
		}
	}
	return changed
}

// shrink is capToolResultJSON's trim: one row's detail per call, the last row
// first, a level at a time. It reports false only once only verdicts remain.
func (m *passageModelResult) shrink() bool {
	for m.level < 4 {
		for m.next < len(m.Rows) {
			row := &m.Rows[len(m.Rows)-1-m.next]
			m.next++
			if row.trim(m.level) {
				return true
			}
		}
		m.level, m.next = m.level+1, 0
	}
	return false
}

func (d assistantToolDeps) executeAssessPassageWindow(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	args, err := parsePassageWindowArgs(raw)
	if err != nil {
		return "", err
	}
	if d.passageLimits == nil {
		return "", fmt.Errorf("assess_passage_window: passage limits are not available")
	}
	if d.cards == nil {
		return "", fmt.Errorf("assess_passage_window: cards are not available")
	}
	limits, err := d.passageLimits()
	if err != nil {
		return "", fmt.Errorf("assess_passage_window: %w", err)
	}
	wx, wxID, err := d.weather()
	if err != nil {
		return "", fmt.Errorf("assess_passage_window: %w", err)
	}

	// A provider gap is logged once per cause per call, naming the provider
	// and the first point it hit.
	var logMu sync.Mutex
	logged := map[string]bool{}
	logOnce := func(cause, format string, v ...any) {
		logMu.Lock()
		defer logMu.Unlock()
		if !logged[cause] {
			logged[cause] = true
			log.Printf("assistant: assess_passage_window: "+format, v...)
		}
	}

	// No wave provider is a gap the card shows as "wave not checked", not an
	// error and never calm seas.
	var wv waveProvider
	var wvID string
	if d.waves != nil {
		p, id, werr := d.waves()
		if werr != nil {
			logOnce("waves:"+werr.Error(), "wave provider unavailable, waves not checked: %v", werr)
		} else {
			wv, wvID = p, id
		}
	}
	const days = assistantMaxForecastDays
	now := d.now()
	fc := forecastSource{Provider: wxID, WaveProvider: wvID, Fetch: func(ctx context.Context, lat, lon float64) (passageForecast, error) {
		if err := ctx.Err(); err != nil {
			return passageForecast{}, err
		}
		// Every point is fetched in the itinerary's zone, the one the assessor
		// buckets and labels every day in (assessItinerary), so a provider's
		// daily rollup can't land on a different boundary from the one read.
		if len(args.Legs) == 0 || len(args.Legs[0].Route) == 0 {
			return passageForecast{}, fmt.Errorf("assess_passage_window: the itinerary has no first point to take its time zone from")
		}
		first := args.Legs[0].Route[0]
		itineraryZone, err := zoneAt(first.Lat, first.Lon)
		if err != nil {
			return passageForecast{}, err
		}
		wb, err := wx.FetchForecast(lat, lon, days, itineraryZone.Name)
		if err != nil {
			return passageForecast{}, fmt.Errorf("weather provider %q: %w", wxID, err)
		}
		fetchedAt := now
		if wb.Cached && !wb.CachedAt.IsZero() {
			fetchedAt = wb.CachedAt
		}
		var vb *waveForecastBundle
		if wv != nil {
			if err := ctx.Err(); err != nil {
				return passageForecast{}, err
			}
			if b, werr := wv.FetchWaves(lat, lon, days); werr != nil {
				logOnce("wavefetch:"+wvID+":"+werr.Error(), "wave provider %q failed at %.2f, %.2f, waves not checked: %v", wvID, lat, lon, werr)
			} else {
				vb = &b
				if b.Cached && !b.CachedAt.IsZero() && b.CachedAt.Before(fetchedAt) {
					fetchedAt = b.CachedAt
				}
			}
		}
		return passageForecast{Hours: passageHoursFromBundles(wb, vb), FetchedAt: fetchedAt}, nil
	}}
	tides := func(ctx context.Context, lat, lon float64) ([]tideExtremePoint, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if d.tides == nil {
			return nil, fmt.Errorf("no tide provider")
		}
		st, err := d.resolveTideStation("assess_passage_window", lat, lon)
		if err != nil {
			_, id, _ := d.tides()
			logOnce("tides:"+id+":"+err.Error(), "tide provider %q failed at %.2f, %.2f, wind against tide not checked: %v", id, lat, lon, err)
			return nil, err
		}
		return st.chart.Extremes, nil
	}
	payload, err := assessItinerary(ctx, args, limits, now, fc, tides, sunTimes)
	if err != nil {
		return "", err
	}
	card, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("assess_passage_window card: %w", err)
	}
	model := newPassageModelResult(payload)
	model.Notes = ignoredSpeedNotes(args)
	text, err := capToolResultJSON(model, model.shrink)
	if err != nil {
		return "", err
	}
	d.cards.put(text, mateCard{Kind: passageWindowCardKind, Payload: card, routes: legRoutes(args), created: now})
	return text, nil
}

var mateToolAssessPassageWindow = mateTool{
	name: "assess_passage_window",
	function: openRouterFunctionDef{
		Description: "Judge an itinerary against the operator's own passage limits and show it as a grid card: one " +
			"row per candidate departure date, one cell per leg on the date it would be sailed, each go, marginal, " +
			"no_go or no_data with the limiting factor. Use it for \"which day should I leave\" and multi-leg trips " +
			"with stays (leave Thursday, two nights at Cape Upstart, then Bowen). The limits come from the " +
			"operator's settings; if they are not set the tool says so and you must tell the operator, never " +
			"assume numbers. Wind is judged by its angle to each segment of the route, so give the route's own " +
			"bends as via points: call route_distance (or read the route waypoints in estimate_passage) for each leg and use its " +
			"waypoints, never headlands you guess. Get place coordinates from find_places. For each leg give " +
			"passage_hours (from estimate_passage) or speed_kn; if both are given the hours are used. Give flood_set_deg only from the operator's " +
			"standing notes or after asking them; never guess it. Dates beyond the forecast show no_data. A card " +
			"is attached to your reply, so do not repeat the grid as a table. " +
			fmt.Sprintf("The operator sets only a no-go wind (required) and no-go wave (optional) per sector, plus an optional wind against tide reduction. "+
				"Marginal is worked out as %.0f%% of no-go and gusts are judged against the no-go wind plus %g kn, "+
				"so never ask the operator to set marginal or gust limits.", passageMarginalFraction*100, passageGustAboveWindKn),
		Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"legs": {
							"type": "array", "minItems": 1, "maxItems": 6,
							"description": "The legs in sailing order.",
							"items": {
								"type": "object",
								"properties": {
									"name": {"type": "string", "description": "Optional. Defaults to \"<start> → <end>\" from the point names."},
									"route": {
										"type": "array", "minItems": 2, "maxItems": 10,
										"description": "Start, any via points, end. Use find_places for the places and the waypoints route_distance returns for the via points between them.",
										"items": {
											"type": "object",
											"properties": {
												"lat": {"type": "number"},
												"lon": {"type": "number"},
												"name": {"type": "string", "description": "Place name, used in the limiting text."}
											},
											"required": ["lat", "lon"]
										}
									},
									"passage_hours": {"type": "number", "minimum": 0.5, "maximum": 36, "description": "Hours under way. Used when given, even if speed_kn is also given."},
									"speed_kn": {"type": "number", "minimum": 1, "maximum": 25, "description": "Planned speed in knots; hours are then the route distance over the speed. Ignored when passage_hours is also given."},
									"flood_set_deg": {"type": "number", "minimum": 0, "maximum": 360, "description": "Direction in degrees true the flood stream sets toward. Only from standing notes or the operator; omit if unknown."},
									"depart_time": {"type": "string", "description": "Optional HH:MM local, overriding the itinerary's departure time for this leg."}
								},
								"required": ["route"]
							}
						},
						"stays_nights": {
							"type": "array", "items": {"type": "integer", "minimum": 0, "maximum": 14},
							"description": "One entry per gap between legs (legs minus one): nights spent at the end of leg i before leg i+1. With 1 or more, the next leg leaves that many days after the arrival date. With 0, the next leg leaves the same local date as arrival if its departure time is later than the arrival time, otherwise the next day. Use [] for a single leg."
						},
						"departure_dates": {
							"type": "array", "minItems": 1, "maxItems": 7, "items": {"type": "string"},
							"description": "Local dates YYYY-MM-DD the first leg could leave."
						},
						"depart_time": {"type": "string", "description": "Local departure time HH:MM for every leg unless a leg gives its own."}
					},
					"required": ["legs", "stays_nights", "departure_dates", "depart_time"]
				}`),
	},
	execute: assistantToolDeps.executeAssessPassageWindow,
	describe: func(args json.RawMessage) string {
		return "Checking the forecast along the route against your passage limits…"
	},
}

// passageLimitsFromSettingsFile reads assistant.passage_limits fresh on every
// call, so a change on the Settings page applies to the next question. An
// unreadable settings file or block is an error, never empty limits.
func passageLimitsFromSettingsFile(settingsPath string) func() (passageLimits, error) {
	return func() (passageLimits, error) {
		settings, err := readSettings(settingsPath)
		if err != nil {
			return passageLimits{}, fmt.Errorf("settings are unreadable: %w", err)
		}
		am, _ := settings["assistant"].(map[string]any)
		return passageLimitsFromSettings(am["passage_limits"])
	}
}
