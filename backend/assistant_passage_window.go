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

// jsonList is a list argument. Some models send an array as a JSON string
// holding the array, so both are accepted. Either way it decodes strictly: an
// unknown field inside an element is an error, as it is for the arguments
// around it. A string that does not hold an array is an error.
type jsonList[T any] []T

func (l *jsonList[T]) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return err
		}
		trimmed = []byte(s)
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	var items []T
	if err := dec.Decode(&items); err != nil {
		return fmt.Errorf("must be an array: %w", err)
	}
	*l = items
	return nil
}

type passageWindowArgs struct {
	Legs           jsonList[passageLegArgs] `json:"legs"`
	StaysNights    jsonList[int]            `json:"stays_nights"`
	DepartureDates jsonList[string]         `json:"departure_dates"`
	DepartTime     string                   `json:"depart_time"`
}

type passageWindowLimiting struct {
	Factor string  `json:"factor"`
	Sector *string `json:"sector"`
	Text   string  `json:"text"`
	// Short is the same figure without the leg's name, the direction the wind
	// comes from or the marginal threshold: "gusts 32 kn ahead". The passage plan
	// table reads it; the card does not carry it.
	Short string `json:"-"`
}

// passageNoData is why a cell has no verdict.
type passageNoData int

const (
	passageNoDataNone passageNoData = iota
	// passageNoDataBeyondForecast: every missing hour lies past the last hour the
	// forecast has, so the leg is further ahead than the forecast reaches.
	passageNoDataBeyondForecast
	// passageNoDataGap: the forecast has a hole inside the leg's hours.
	passageNoDataGap
)

type passageWindowCell struct {
	Leg        int                    `json:"leg"`
	Date       string                 `json:"date"`
	Depart     string                 `json:"depart"`
	Arrive     string                 `json:"arrive"`
	State      string                 `json:"state"`
	Limiting   *passageWindowLimiting `json:"limiting"`
	Note       string                 `json:"note,omitempty"`
	NotChecked []string               `json:"not_checked"`

	// wind is what the model reads beside the limiting line (ADR 0265). It is
	// not part of the card.
	wind string
	// noData says why the state is no_data. ratio is how far the worst figure
	// stood past its limit (1 is at it), which orders no-go cells by how bad they
	// are. Neither is part of the card.
	noData passageNoData
	ratio  float64
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
	// passageWindowMaxRoutePoints is how many points of a route the wind check
	// is given. It judges the wind against each segment's bearing and reads one
	// forecast per segment, so planPassageLeg keeps no more than this.
	passageWindowMaxRoutePoints = 10
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
		if n := len(l.Route); n < 2 || n > passageWindowMaxRoutePoints {
			return fail("legs[%d].route must hold 2 to %d points (start, any vias, end), got %d", i, passageWindowMaxRoutePoints, n)
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
	return len(l.noGoSectors()) > 0
}

// noGoSectors names the sectors with a wind no-go, in ahead, abeam, astern
// order.
func (l passageLimits) noGoSectors() []string {
	var out []string
	for _, sec := range []struct {
		name string
		l    passageSectorLimits
	}{{"ahead", l.Ahead}, {"abeam", l.Abeam}, {"astern", l.Astern}} {
		if sec.l.WindKnNoGo != nil {
			out = append(out, sec.name)
		}
	}
	return out
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

// capRoutePoints keeps at most max points of a route: both ends, and the bends
// that matter. It is Douglas-Peucker (douglasPeucker) at the smallest tolerance
// that leaves no more than max points, found by bisection.
func capRoutePoints(route []passageRoutePoint, max int) []passageRoutePoint {
	if len(route) <= max || max < 2 {
		return route
	}
	const metresPerDegree = 111195.0
	var latSum float64
	for _, p := range route {
		latSum += p.Lat
	}
	cosLat := math.Cos(latSum / float64(len(route)) * math.Pi / 180)
	xy := make([][2]float64, len(route))
	pathM := 0.0
	for i, p := range route {
		xy[i] = [2]float64{p.Lon * metresPerDegree * cosLat, p.Lat * metresPerDegree}
		if i > 0 {
			pathM += math.Hypot(xy[i][0]-xy[i-1][0], xy[i][1]-xy[i-1][1])
		}
	}
	keepAt := func(tol float64) (keep []bool, n int) {
		keep = make([]bool, len(route))
		keep[0], keep[len(route)-1] = true, true
		douglasPeucker(xy, 0, len(route)-1, tol, keep)
		for _, k := range keep {
			if k {
				n++
			}
		}
		return keep, n
	}
	// No point lies further off a chord than the whole path is long, so at that
	// tolerance only the ends are kept.
	lo, hi := 0.0, pathM
	for i := 0; i < 40; i++ {
		mid := (lo + hi) / 2
		if _, n := keepAt(mid); n <= max {
			hi = mid
		} else {
			lo = mid
		}
	}
	keep, _ := keepAt(hi)
	out := make([]passageRoutePoint, 0, max)
	for i, k := range keep {
		if k {
			out = append(out, route[i])
		}
	}
	return out
}

// hoursDuration is a number of hours as a Duration.
func hoursDuration(hours float64) time.Duration {
	return time.Duration(hours * float64(time.Hour))
}

// planPassageLeg lays out a leg: its segments, the points the forecast is read
// at, and its hours. A route over passageWindowMaxRoutePoints is thinned to that
// many points first, so every caller gives the wind check the same bound.
func planPassageLeg(i int, l passageLegArgs) passageLegPlan {
	l.Route = capRoutePoints(l.Route, passageWindowMaxRoutePoints)
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
	return routeWalk{segs: p.segs, totalM: p.totalM}.at(frac)
}

// routeWalk dead-reckons along a polyline of segments. It is the one place that
// turns a fraction of the way into a position, shared by the passage window and
// the fair-tide departure.
type routeWalk struct {
	segs   []passageSegment
	totalM float64
}

// at is the position and segment a fraction of the way along the route.
func (w routeWalk) at(frac float64) (lat, lon float64, seg passageSegment) {
	d := math.Max(0, math.Min(1, frac)) * w.totalM
	idx := len(w.segs) - 1
	for i, s := range w.segs {
		if d < s.startM+s.lengthM {
			idx = i
			break
		}
	}
	s := w.segs[idx]
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
	// short is the line without the leg's name, the wind's direction or the
	// marginal threshold (passageWindowLimiting.Short).
	short string
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
	// skipWindAgainstTide leaves wind against tide out of every cell, and out of
	// not_checked: a passage plan has no flood direction to judge it by.
	skipWindAgainstTide bool

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

// newPassageAssessor is an assessor with empty caches. The itinerary and the
// passage plan table both judge legs through one; cell is given the zone its
// days are read in.
func newPassageAssessor(limits passageLimits, fc forecastSource, tides tideSource, sun func(date time.Time, lat, lon float64) (rise, set time.Time, ok bool)) *passageAssessor {
	return &passageAssessor{limits: limits, fc: fc, tides: tides, sun: sun,
		cache: map[string]map[int64]passageHour{}, tideCache: map[string]passageTideResult{}, tideGaps: map[string]bool{}}
}

// prefetchLegs fetches the forecast at every distinct sample point of the legs.
func (as *passageAssessor) prefetchLegs(ctx context.Context, plans []passageLegPlan) error {
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
	return as.prefetch(ctx, points)
}

// legEndSun is the sunrise and sunset at the end of a leg on the day t falls in,
// in loc: the day the daylight rule judges an arrival on. The assessor's cell and
// the passage plan's departure window both read it here.
func legEndSun(sun func(date time.Time, lat, lon float64) (rise, set time.Time, ok bool), plan passageLegPlan, t time.Time, loc *time.Location) (rise, set time.Time, ok bool) {
	return sun(t.In(loc), plan.dailyEnd.Lat, plan.dailyEnd.Lon)
}

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
				out[i].err = fmt.Errorf("forecast at %.2f, %.2f: %w", pt.Lat, pt.Lon, err)
				return
			}
			out[i].f = f
		}(i, pt)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
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
		return nil, fmt.Errorf("no forecast was fetched at %.2f, %.2f", pt.Lat, pt.Lon)
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
			fmt.Sprintf("Waves %s%s · %s", waveFigures(waveM, h), marginalNote(s, *lim), label),
			"waves " + waveFigures(waveM, h)}
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

// lastHour is the unix time of the latest hour a point's forecast has, 0 for none.
func lastHour(hours map[int64]passageHour) int64 {
	var last int64
	for t := range hours {
		if t > last {
			last = t
		}
	}
	return last
}

// cell judges one leg leaving at depart, reading its days in loc. Its errors
// carry no tool name; the caller adds its own.
func (as *passageAssessor) cell(ctx context.Context, idx int, plan passageLegPlan, depart time.Time, loc *time.Location) (passageWindowCell, error) {
	arrive := depart.Add(hoursDuration(plan.hours))
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
	missing, gap := false, false
	anyGust := false
	tally := newPassageWindTally(plan.hours, len(plan.segs))

	// Wind against tide: only for a leg with a known flood set and a band, and
	// never when the assessor was told to leave it out.
	var cycles []tidalHalfCycle
	tideOn := false
	if plan.args.FloodSetDeg != nil && as.limits.WindAgainstTideOffsetKn != nil && !as.skipWindAgainstTide {
		ext, err := as.tideExtremes(ctx, plan.args.Route[0])
		if cerr := ctx.Err(); cerr != nil {
			return cell, cerr
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
	if !tideOn && !as.skipWindAgainstTide {
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
			// An hour past the last the forecast has is a leg further ahead than
			// it reaches; one before it is a hole in the data.
			missing, gap = true, gap || t.Unix() <= lastHour(hours)
			tally.gap(t, depart, arrive)
			continue
		}
		// Judged at the precision the card prints, so what the operator
		// reads is what was compared with the limit.
		rawWindKn := h.WindMS * metersPerSecondToKnots
		rawGustKn := h.GustMS * metersPerSecondToKnots
		windKn, gustKn, waveM := math.Round(rawWindKn), math.Round(rawGustKn), roundTo1(h.WaveM)
		sector := passageSector(angleBetween(h.WindDirDeg, seg.bearing))
		tally.add(t, depart, arrive, h.WindDirDeg, windKn, sector, seg.label)
		windFrom := degreesToDirection(h.WindDirDeg)
		lim := as.limits.sector(sector)
		if lim.WindKnNoGo == nil {
			return cell, fmt.Errorf("Passage limits have no wind no-go for the %s sector, and %s runs in it. Set it in Settings → Mate → Passage limits", sector, plan.name)
		}
		windNoGo := *lim.WindKnNoGo
		sev, ratio := judge(windKn, rawWindKn, windNoGo)
		consider(passageCandidate{sev, ratio, "wind", sector,
			fmt.Sprintf("Wind %d kn from %s, %s%s · %s", int(windKn), windFrom, sector, marginalNote(sev, windNoGo), seg.label),
			fmt.Sprintf("wind %d kn %s", int(windKn), sector)})

		// Providers leave a gust they do not have at 0. An hour at 0 beside
		// hours with a figure is a calm hour (0 cannot exceed a positive
		// limit); only a leg with no gust figure at all is listed as not
		// checked, below.
		if h.GustMS > 0 {
			anyGust = true
			gustNoGo := windNoGo + passageGustAboveWindKn
			sev, ratio = judge(gustKn, rawGustKn, gustNoGo)
			consider(passageCandidate{sev, ratio, "gust", sector,
				fmt.Sprintf("Gusts %d kn from %s, %s%s · %s", int(gustKn), windFrom, sector, marginalNote(sev, gustNoGo), seg.label),
				fmt.Sprintf("gusts %d kn %s", int(gustKn), sector)})
		}

		switch {
		case !h.HasWave:
			notChecked["wave"] = true
		case h.HasWaveDir:
			waveSector := passageSector(angleBetween(h.WaveDirDeg, seg.bearing))
			if waveNoGo := as.limits.sector(waveSector).WaveMNoGo; waveNoGo != nil {
				sev, ratio := judge(waveM, h.WaveM, *waveNoGo)
				consider(passageCandidate{sev, ratio, "wave", waveSector,
					fmt.Sprintf("Waves %s %s%s · %s", waveFigures(waveM, h), waveSector, marginalNote(sev, *waveNoGo), seg.label),
					fmt.Sprintf("waves %s %s", waveFigures(waveM, h), waveSector)})
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
						return cell, fmt.Errorf("the wind against tide reduction (%g kn) is not below the %s no-go wind (%g kn). Fix it in Settings → Mate → Passage limits", *as.limits.WindAgainstTideOffsetKn, sector, windNoGo)
					}
					sev, ratio := judge(windKn, rawWindKn, tideNoGo)
					consider(passageCandidate{sev, ratio, "wind_against_tide", "",
						fmt.Sprintf("Wind %d kn against the %s%s · %s", int(windKn), phase, marginalNote(sev, tideNoGo), plan.name),
						fmt.Sprintf("wind %d kn against the %s", int(windKn), phase)})
				}
			}
		}
	}

	if as.limits.DaylightArrival {
		rise, set, ok := legEndSun(as.sun, plan, arrive, loc)
		switch {
		case !ok:
			notChecked["daylight"] = true
		case arrive.Before(rise):
			consider(passageCandidate{2, 1, "daylight", "",
				fmt.Sprintf("Arrives %s, before sunrise %s · %s", boatClock(arrive, loc), boatClock(rise, loc), plan.name),
				fmt.Sprintf("arrives %s, before sunrise %s", boatClock(arrive, loc), boatClock(rise, loc))})
		case arrive.After(set):
			consider(passageCandidate{2, 1, "daylight", "",
				fmt.Sprintf("Arrives %s, after sunset %s · %s", boatClock(arrive, loc), boatClock(set, loc), plan.name),
				fmt.Sprintf("arrives %s, after sunset %s", boatClock(arrive, loc), boatClock(set, loc))})
		}
	}

	cell.wind = tally.summary(plan.name)
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
	if haveWorst {
		cell.ratio = worst.ratio
	}
	if cell.State == "no_data" {
		cell.Note = passageWindowNoForecastNote
		cell.noData = passageNoDataBeyondForecast
		if gap {
			cell.noData = passageNoDataGap
		}
	} else if haveWorst {
		l := &passageWindowLimiting{Factor: worst.factor, Text: worst.text, Short: worst.short}
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
	as := newPassageAssessor(limits, fc, tides, sun)

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
			arrive := depart.Add(hoursDuration(plan.hours))
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
	if err := as.prefetchLegs(ctx, plans); err != nil {
		return passageWindowPayload{}, fmt.Errorf("assess_passage_window: %w", err)
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
			cell, err := as.cell(ctx, i, plan, departs[r][i], loc)
			if err != nil {
				return passageWindowPayload{}, fmt.Errorf("assess_passage_window: %w", err)
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
// zero, and an hour with no weather point or no wind data is absent too, so a
// leg into it is no_data rather than judged as a calm.
func passageHoursFromBundles(wb weatherForecastBundle, vb *waveForecastBundle) map[int64]passageHour {
	out := map[int64]passageHour{}
	for _, hp := range wb.Hourly {
		if hp.Time.IsZero() || !hp.hasWindDirection() {
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
	Legs []string          `json:"legs"`
	Rows []passageModelRow `json:"rows"`
	// Instruction is present only when Helmcentral shows the grid as a card.
	Instruction string   `json:"instruction,omitempty"`
	Notes       []string `json:"notes,omitempty"`

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
	// Wind says how the wind lies along the whole leg, which Why (one hour's
	// figure) does not.
	Wind string `json:"wind,omitempty"`
}

func newPassageModelResult(p passageWindowPayload, withCard bool) *passageModelResult {
	m := &passageModelResult{Legs: make([]string, len(p.Legs)), Rows: make([]passageModelRow, len(p.Rows))}
	if withCard {
		m.Instruction = passageWindowInstruction
	}
	for i, l := range p.Legs {
		m.Legs[i] = l.Name
	}
	clock := func(s string) string {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return ""
		}
		return boatClock(t, t.Location())
	}
	for i, r := range p.Rows {
		row := passageModelRow{DepartDate: r.DepartDate, Verdict: r.Verdict, Cells: make([]passageModelCell, len(r.Cells))}
		for j, c := range r.Cells {
			mc := passageModelCell{Leg: c.Leg, State: c.State, Why: c.Note}
			if c.Limiting != nil {
				mc.Why = c.Limiting.Text
			}
			mc.Wind = c.wind
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
// was there to remove: go cells' reasons, then go cells' wind summaries, then
// the summaries of the other cells, then every reason, then the times, then the
// cells themselves. A reason that decided a no-go or marginal cell, and the
// summary beside it, outlast everything on a go cell. A row's verdict is never
// removed.
func (r *passageModelRow) trim(level int) bool {
	changed := false
	switch level {
	case 0, 3:
		for i := range r.Cells {
			if r.Cells[i].Why != "" && (level == 3 || r.Cells[i].State == "go") {
				r.Cells[i].Why, changed = "", true
			}
		}
	case 1: // go cells' summaries
		for i := range r.Cells {
			if r.Cells[i].Wind != "" && r.Cells[i].State == "go" {
				r.Cells[i].Wind, changed = "", true
			}
		}
	case 2: // the other cells' summaries
		for i := range r.Cells {
			if r.Cells[i].Wind != "" && r.Cells[i].State != "go" {
				r.Cells[i].Wind, changed = "", true
			}
		}
	case 4:
		for i := range r.Cells {
			if r.Cells[i].Times != "" {
				r.Cells[i].Times, changed = "", true
			}
		}
	case 5:
		if r.Cells != nil {
			r.Cells, changed = nil, true
		}
	}
	return changed
}

// shrink is capToolResultJSON's trim: one row's detail per call, the last row
// first, a level at a time. It reports false only once only verdicts remain.
func (m *passageModelResult) shrink() bool {
	for m.level < 6 {
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

// passageSources are the forecast and tide readers a passage assessment judges
// with, wired from the configured providers.
type passageSources struct {
	fc    forecastSource
	tides tideSource
	// fallbackModel is the wave model some point fell back to, "" when none
	// did. Read it after the forecast has been fetched.
	fallbackModel func() string
}

// passageSources wires the configured weather, wave and tide providers for an
// assessment named tool, which leads its log lines; its errors carry no tool
// name. zone names the
// time zone every point's forecast is fetched in. A weather provider that
// cannot be resolved is an error; no wave provider is a gap the result shows
// as waves not checked, never calm seas.
func (d assistantToolDeps) passageSources(tool string, zone func() (string, error), now time.Time) (passageSources, error) {
	wx, wxID, err := d.weather()
	if err != nil {
		return passageSources{}, err
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
			log.Printf("assistant: "+tool+": "+format, v...)
		}
	}

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
	// Fetch runs for several points at once.
	var fallbackMu sync.Mutex
	var fallbackModel string
	fc := forecastSource{Provider: wxID, WaveProvider: wvID, Fetch: func(ctx context.Context, lat, lon float64) (passageForecast, error) {
		if err := ctx.Err(); err != nil {
			return passageForecast{}, err
		}
		zoneName, err := zone()
		if err != nil {
			return passageForecast{}, err
		}
		// The wave read is slow and does not depend on the weather one, so they run
		// together; the weather's error, if any, still decides the result.
		type waveRead struct {
			b    waveForecastBundle
			werr error
		}
		var waves chan waveRead
		if wv != nil {
			waves = make(chan waveRead, 1)
			go func() {
				b, werr := wv.FetchWaves(lat, lon, days)
				waves <- waveRead{b, werr}
			}()
		}
		wb, err := wx.FetchForecast(lat, lon, days, zoneName)
		var wr waveRead
		if waves != nil {
			wr = <-waves
		}
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
			if b, werr := wr.b, wr.werr; werr != nil {
				logOnce("wavefetch:"+wvID+":"+werr.Error(), "wave provider %q failed at %.2f, %.2f, waves not checked: %v", wvID, lat, lon, werr)
			} else {
				vb = &b
				if b.FallbackModel != "" {
					fallbackMu.Lock()
					fallbackModel = b.FallbackModel
					fallbackMu.Unlock()
				}
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
		st, err := d.resolveTideStation(lat, lon)
		if err != nil {
			_, id, _ := d.tides()
			logOnce("tides:"+id+":"+err.Error(), "tide provider %q failed at %.2f, %.2f, wind against tide not checked: %v", id, lat, lon, err)
			return nil, err
		}
		return st.chart.Extremes, nil
	}
	return passageSources{fc: fc, tides: tides, fallbackModel: func() string {
		fallbackMu.Lock()
		defer fallbackMu.Unlock()
		return fallbackModel
	}}, nil
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
	limits, err := d.passageLimits()
	if err != nil {
		return "", fmt.Errorf("assess_passage_window: %w", err)
	}
	now := d.now()
	src, err := d.passageSources("assess_passage_window", func() (string, error) {
		// Every point is fetched in the itinerary's zone, the one the assessor
		// buckets and labels every day in (assessItinerary), so a provider's
		// daily rollup can't land on a different boundary from the one read.
		if len(args.Legs) == 0 || len(args.Legs[0].Route) == 0 {
			return "", fmt.Errorf("the itinerary has no first point to take its time zone from")
		}
		first := args.Legs[0].Route[0]
		itineraryZone, err := zoneAt(first.Lat, first.Lon)
		if err != nil {
			return "", err
		}
		return itineraryZone.Name, nil
	}, now)
	if err != nil {
		return "", fmt.Errorf("assess_passage_window: %w", err)
	}
	fc, tides := src.fc, src.tides
	payload, err := assessItinerary(ctx, args, limits, now, fc, tides, sunTimes)
	if err != nil {
		return "", err
	}
	// A card is built only where a run takes it (Mate's chat). An outside
	// assistant has no card sink, so it gets the grid figures and no card.
	var card json.RawMessage
	if d.cards != nil {
		if card, err = json.Marshal(payload); err != nil {
			return "", fmt.Errorf("assess_passage_window card: %w", err)
		}
	}
	model := newPassageModelResult(payload, d.cards != nil)
	model.Notes = ignoredSpeedNotes(args)
	if fallbackModel := src.fallbackModel(); fallbackModel != "" {
		model.Notes = append(model.Notes, fmt.Sprintf("Wave figures at some points come from the fallback model %s, because the usual wave model has no data there. Say so when you give wave figures.", fallbackModel))
	}
	text, err := capToolResultJSON(model, model.shrink)
	if err != nil {
		return "", err
	}
	if d.cards != nil {
		d.cards.put(text, mateCard{Kind: passageWindowCardKind, Payload: card, routes: legRoutes(args), created: now})
	}
	return text, nil
}

var mateToolAssessPassageWindow = mateTool{
	name:      "assess_passage_window",
	effect:    effectReads,
	audiences: audienceMCPOnly,
	drawsCard: true,
	function: openRouterFunctionDef{
		Description: "Judge an itinerary against the operator's own passage limits as a grid: one " +
			"row per candidate departure date, one cell per leg on the date it would be sailed, each go, marginal, " +
			"no_go or no_data with the limiting factor. Use it for \"which day should I leave\" and multi-leg trips " +
			"with stays (leave Thursday, two nights at Cape Upstart, then Bowen). The limits come from the " +
			"operator's settings; if they are not set the tool says so and you must tell the operator, never " +
			"assume numbers. Wind is judged by its angle to each segment of the route, so give the route's own " +
			"bends as via points: call route_distance (or read the route waypoints in estimate_passage) for each leg and use its " +
			"waypoints, never headlands you guess. Get place coordinates from find_places. For each leg give " +
			"passage_hours (from estimate_passage) or speed_kn; if both are given the hours are used. Give flood_set_deg only from the operator's " +
			"standing notes or after asking them; never guess it. Dates beyond the forecast show no_data. " +
			"The operator sets only a no-go wind (required) and no-go wave (optional) per sector, plus an optional wind against tide reduction. " +
			passageLimitsSentence + " " + rulePassageWindow,
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
	execute: plainTool(assistantToolDeps.executeAssessPassageWindow),
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
		return passageLimitsFromSettingsMap(settings)
	}
}

// passageLimitsFromSettingsMap finds assistant.passage_limits in a decoded
// settings file and decodes it.
func passageLimitsFromSettingsMap(settings map[string]any) (passageLimits, error) {
	am, _ := settings["assistant"].(map[string]any)
	return passageLimitsFromSettings(am["passage_limits"])
}
