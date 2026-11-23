package main

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
)

// The fetch cap on the wind wave, and the enclosed-water flag on the swell (ADR 0300).
//
// Open-Meteo's marine model has no grid cell that holds a bay, a river mouth or a
// marina, so in sheltered water its wind wave is several times what the boat felt.
// The logbook corrects it where the shore is charted: the wind wave cannot be
// higher than the wind can build over the water upwind, which is the fetch. The
// spell's mean true wind and the direction it came from are kept with the spell.
// The hourly sea sweep reads the shoreline from the Offline coastline once for
// each position, casts the rays and stores what they found (sea_fetch_sweep.go);
// the page only reads those rows, and never the shoreline. A wind wave that
// cannot be judged keeps the model's figure and says why it was not checked, and
// never passes as checked.

const (
	seaCheckCapped    = "capped"
	seaCheckWithin    = "within"
	seaCheckOpen      = "open"
	seaCheckUnchecked = "unchecked"

	// fetchRayStepDeg and fetchRaySpreadDeg are the rays cast for a wind wave's
	// fetch: seven, every 5 degrees over 15 degrees either side of the wind.
	fetchRayStepDeg   = 5
	fetchRaySpreadDeg = 15
	// fetchCalmKts is the true wind below which a spell has no direction worth
	// using, and every bearing is looked at instead.
	fetchCalmKts = 1.0
	// enclosedSpreadDeg and enclosedRayStepDeg are the rays cast for the swell:
	// seven, every 10 degrees over 30 degrees either side of where it comes from.
	enclosedSpreadDeg  = 30
	enclosedRayStepDeg = 10

	// fetchMeanPeriodRatio is the peak period over the mean period of a
	// fetch-limited sea. Open-Meteo gives a mean period, so the cap on the period
	// is the limit's peak period over this.
	fetchMeanPeriodRatio = 1.2
	// fetchMinWindMS keeps the formula defined for a dead calm.
	fetchMinWindMS = 0.1

	swellEnclosedNote  = "swell not judged in enclosed water"
	swellUncheckedNote = "not checked for enclosed water: "
	reefTideCaveat     = "a reef upwind blocks like land, so the limit depends on the tide"

	// A projected segment carries which kind of line it came from in place of its
	// line index, since the fetch only needs to know a reef from land.
	fetchSegCoast = 0
	fetchSegReef  = 1
)

// fetchWaveLimit is the highest and longest-period wind wave a wind of twsKts can
// build over fetchM metres of deep water: the U.S. Army Corps of Engineers'
// Coastal Engineering Manual (EM 1110-2-1100, Part II-2, equations II-2-36 to
// II-2-38). The wind is taken as recorded, at the height of the anemometer; the
// manual's correction to 10 m is not applied, and the speed log is never
// corrected. Hm0 is the significant wave height and Tp the peak period.
func fetchWaveLimit(twsKts, fetchM float64) (hm0, tp float64) {
	g := gravityMPerS2
	u := math.Max(twsKts*knotsToMetersPerSecond, fetchMinWindMS)
	cd := 0.001 * (1.1 + 0.035*u)
	us2 := cd * u * u // u*²
	us := math.Sqrt(us2)
	x := g * fetchM / us2
	hm0 = math.Min(0.0413*(us2/g)*math.Sqrt(x), 211.5*us2/g)
	tp = math.Min(0.651*(us/g)*math.Cbrt(x), 239.8*us/g)
	return hm0, tp
}

// cutShows says a figure the cap lowered still reads lower on the page, which
// rounds a height to 0.1 m and a period to a whole second. A cut finer than that
// would read as a cut to the same figure, so it is neither named nor counted.
func heightCutShows(hindcast, limit float64) bool {
	return math.Round(hindcast*10) > math.Round(limit*10)
}
func periodCutShows(hindcast, limit float64) bool { return math.Round(hindcast) > math.Round(limit) }

// fetchCheck is what the fetch check made of a wind wave, with the figures it
// went by. Check is capped, within, open or unchecked. A capped wind wave
// carries the capped height and period in its own figures and the model's here.
type fetchCheck struct {
	Check string `json:"check"`
	// Calm says the spell's wind was too light to have a direction, so the fetch is
	// the longest over every bearing.
	Calm            bool     `json:"calm,omitempty"`
	WindKts         *float64 `json:"wind_kts,omitempty"`
	FetchNm         *float64 `json:"fetch_nm,omitempty"`
	CapHeightM      *float64 `json:"cap_height_m,omitempty"`
	CapPeriodS      *float64 `json:"cap_period_s,omitempty"`
	HindcastHeightM *float64 `json:"hindcast_height_m,omitempty"`
	HindcastPeriodS *float64 `json:"hindcast_period_s,omitempty"`
	// Reason is why the wave was not checked.
	Reason string `json:"reason,omitempty"`
	// Caveat is what the limit depends on, when the wave was checked.
	Caveat string `json:"caveat,omitempty"`
	// Ashore says the wave was not checked because the position is charted ashore.
	Ashore bool `json:"-"`
}

// note is the check in the words the page shows after the wind wave's figures.
func (c *fetchCheck) note() string {
	var s string
	switch c.Check {
	case seaCheckCapped:
		// Only what the page shows lowered is named: a period cut alone does not
		// imply the height was cut.
		height := heightCutShows(*c.HindcastHeightM, *c.CapHeightM)
		period := periodCutShows(*c.HindcastPeriodS, *c.CapPeriodS)
		hind := fmt.Sprintf("%.1f m", *c.HindcastHeightM)
		lead := "fetch-limited from "
		switch {
		case height && period:
			hind += fmt.Sprintf(" %d s", int(math.Round(*c.HindcastPeriodS)))
		case period:
			lead = "period fetch-limited from "
			hind = fmt.Sprintf("%d s", int(math.Round(*c.HindcastPeriodS)))
		}
		wind := fmt.Sprintf("in %d kn", int(math.Round(*c.WindKts)))
		if c.Calm {
			wind = "in a calm"
		}
		s = fmt.Sprintf("%s%s: %.1f nm fetch %s", lead, hind, *c.FetchNm, wind)
	case seaCheckWithin:
		s = fmt.Sprintf("within the %.1f m fetch limit", *c.CapHeightM)
	case seaCheckOpen:
		if c.Calm {
			return "open water nearby"
		}
		return "open water upwind"
	default:
		return "not checked against fetch: " + c.Reason
	}
	if c.Caveat != "" {
		s += "; " + c.Caveat
	}
	return s
}

// fetchGeometry is the shore around one spell's position: the segments a ray
// from it can reach. The sweep builds one for each spell it stores. A plan
// assessment builds one for a position whose rays the shore ray memo does not
// hold, and lets it go when the leg is judged (comfortSetup.geometryAt).
type fetchGeometry struct {
	segs []shelterSegment
	// onLand says the position is inside the charted coastline.
	onLand bool
}

// newFetchGeometry projects the lines around a position once. Only the segments
// within a ray's reach are kept to cast over: a ray never goes further, so the
// rest changes nothing a spell reads.
func newFetchGeometry(lat, lon float64, all []shorelineLine) *fetchGeometry {
	// Only what lies near the position is projected; the rest is projected below
	// only when nothing near says whether the position is ashore.
	lines := cullShoreline(lat, lon, all)
	segs, _ := projectShoreline(lat, lon, lines)
	reach := shelterMaxFetchNm * metersPerNauticalMile
	g := &fetchGeometry{}
	var nearCoast []wrSeg
	for _, s := range segs {
		kind := lines[s.line].Kind
		if !(math.Max(s.x1, s.x2) >= -reach && math.Min(s.x1, s.x2) <= reach &&
			math.Max(s.y1, s.y2) >= -reach && math.Min(s.y1, s.y2) <= reach) {
			continue
		}
		s.line = fetchSegCoast
		if kind == "reef" {
			s.line = fetchSegReef
		}
		g.segs = append(g.segs, s)
		if kind == "coastline" {
			nearCoast = append(nearCoast, wrSeg{wrPt{s.x1, s.y1}, wrPt{s.x2, s.y2}})
		}
	}
	// Water lies to the right of a coastline way, so a position with land on its
	// nearest way's left is ashore. Reefs are not land.
	dist, _, _, onLand, found := nearestShore(wrPt{}, nearCoast)
	if !found || dist >= reach-1 {
		// Nothing within reach: the nearest shore is further off, and a position
		// well inside the land still has land on that shore's left.
		allSegs, _ := projectShoreline(lat, lon, all)
		var coast []wrSeg
		for _, s := range allSegs {
			if all[s.line].Kind == "coastline" {
				coast = append(coast, wrSeg{wrPt{s.x1, s.y1}, wrPt{s.x2, s.y2}})
			}
		}
		_, _, _, onLand, found = nearestShore(wrPt{}, coast)
	}
	g.onLand = found && onLand
	return g
}

// cullShoreline keeps the segments whose box meets the box 15 nm (and a margin)
// around a position, before anything is projected. A way that runs in and out of
// the box is kept as the runs inside it, each segment whole, so a long segment
// with both ends outside but its length across the box stays.
func cullShoreline(lat, lon float64, lines []shorelineLine) []shorelineLine {
	// A degree of latitude is shelterEarthRadiusM * pi/180 metres; longitude
	// degrees are that much shorter by cos(lat).
	dLat := 1.05 * shelterMaxFetchNm * metersPerNauticalMile / (math.Pi / 180 * shelterEarthRadiusM)
	dLon := dLat / math.Max(math.Cos(lat*math.Pi/180), 1e-6)
	south, north := lat-dLat, lat+dLat
	meets := func(a, b shorelinePoint) bool {
		// Longitudes are differences from the position, wrapped, so the box holds
		// across the antimeridian.
		ax, bx := lonDelta(a.Lon, lon), lonDelta(b.Lon, lon)
		return math.Max(a.Lat, b.Lat) >= south && math.Min(a.Lat, b.Lat) <= north &&
			math.Max(ax, bx) >= -dLon && math.Min(ax, bx) <= dLon
	}
	var out []shorelineLine
	for _, line := range lines {
		var run []shorelinePoint
		flush := func() {
			if len(run) >= 2 {
				out = append(out, shorelineLine{ID: line.ID, Name: line.Name, Kind: line.Kind, Points: run})
			}
			run = nil
		}
		for j := 1; j < len(line.Points); j++ {
			a, b := line.Points[j-1], line.Points[j]
			if !meets(a, b) {
				flush()
				continue
			}
			if len(run) == 0 {
				run = append(run, a)
			}
			run = append(run, b)
		}
		flush()
	}
	return out
}

// raySpread is the bearings spread either side of centre.
func raySpread(centre float64, spreadDeg, stepDeg int) []float64 {
	var out []float64
	for d := -spreadDeg; d <= spreadDeg; d += stepDeg {
		out = append(out, centre+float64(d))
	}
	return out
}

// sweep casts a ray along each bearing: how far the shore is along each, in
// metres, and whether it is a reef. open is true when any ray finds nothing
// within shelterMaxFetchNm, and then dist and reef are not filled.
func (g *fetchGeometry) sweep(bearings []float64) (dist []float64, reef []bool, open bool) {
	dist, hit := castShelterRays(g.segs, bearings, shelterMaxFetchNm*metersPerNauticalMile)
	reef = make([]bool, len(hit))
	for i, h := range hit {
		if h < 0 {
			return nil, nil, true
		}
		reef[i] = h == fetchSegReef
	}
	return dist, reef, false
}

// windFetch is the fetch for a wind from fromDeg: the mean distance over seven
// rays. open is true when any ray finds no shore within reach, since a wave
// built over water that open is not limited by it and an open ray is never
// counted as 15 nm.
func (g *fetchGeometry) windFetch(fromDeg float64) (m float64, open, reef bool) {
	dist, reefs, open := g.sweep(raySpread(fromDeg, fetchRaySpreadDeg, fetchRayStepDeg))
	if open {
		return 0, true, false
	}
	var sum float64
	for i, d := range dist {
		sum += d
		reef = reef || reefs[i]
	}
	return sum / float64(len(dist)), false, reef
}

// calmFetch is the longest fetch over the 36 bearings, for a spell with no wind
// worth a direction.
func (g *fetchGeometry) calmFetch() (m float64, open, reef bool) {
	dist, reefs, open := g.sweep(shelterBearings())
	if open {
		return 0, true, false
	}
	for i, d := range dist {
		if d > m {
			m, reef = d, reefs[i]
		}
	}
	return m, false, reef
}

// enclosed says the shore closes every ray within 30 degrees of where the swell
// comes from.
func (g *fetchGeometry) enclosed(swellFromDeg float64) bool {
	_, _, open := g.sweep(raySpread(swellFromDeg, enclosedSpreadDeg, enclosedRayStepDeg))
	return !open
}

// What the shore says of a spell, kept by the sweep and read by the page.
const (
	fetchKindWind  = "wind"
	fetchKindCalm  = "calm"
	fetchKindSwell = "swell"

	// fetchNotWorkedOut is the reason a spell has while the sweep has not yet
	// stored what the shore says of it: a position or a coastline that is new.
	fetchNotWorkedOut = "not worked out yet"
	// fetchStoredUnreadable is the reason when the stored figures could not be read.
	fetchStoredUnreadable = "the stored fetch figures couldn't be read"
)

// rayResult is what the shore says of one spell: the position is ashore, or the
// rays found open water (open), or the mean (wind) or longest (calm) distance to
// the shore in metres and whether a reef is what stops it. For a swell, Open
// says it is not enclosed.
type rayResult struct {
	OnLand, Open, Reef bool
	DistM              float64
}

// rayFunc reads what the shore says for one kind of ray along a bearing, or says
// why it cannot (not worked out yet). The bearing is rounded to a tenth of a
// degree, which is the figure rays are cast along and stored under.
type rayFunc func(kind string, bearingDeg float64) (rayResult, string)

// raySource gives the rays for one spell, or says why there are none: the
// coastline is off, a square is missing, the position is inland.
type raySource func() (rayFunc, string)

// bearingDD is a bearing in tenths of a degree, 0 to 3599.
func bearingDD(deg float64) int { return int(math.Round(normalizeDegrees(deg)*10)) % 3600 }

// ray is what the shore says along one bearing (in tenths of a degree), cast
// along the rounded bearing so that what is stored is what was computed.
func (g *fetchGeometry) ray(kind string, dd int) rayResult {
	if g.onLand {
		return rayResult{OnLand: true}
	}
	deg := float64(dd) / 10
	var m float64
	var open, reef bool
	switch kind {
	case fetchKindCalm:
		m, open, reef = g.calmFetch()
	case fetchKindSwell:
		return rayResult{Open: !g.enclosed(deg)}
	default:
		m, open, reef = g.windFetch(deg)
	}
	if open {
		return rayResult{Open: true}
	}
	return rayResult{DistM: m, Reef: reef}
}

// judgeSea applies the fetch check to a spell's wind wave and the enclosed-water
// flag to its swell, with the rays src gives. The stored figures are never
// changed: a capped wind wave carries the model's own beside the capped one. A
// spell with no sea is left as it is.
func judgeSea(sp feltSpell, sea *spellSea, src raySource) {
	judgeSeaWind(spellWind(sp), sea, src)
}

// windInput is the wind a wind wave is checked against: the wind kept with a
// recorded spell, or the forecast's for an hour of a leg. Kept says the wind was
// known at all; a spell stored before the wind was kept has none to judge by.
type windInput struct {
	kept       bool
	kts        *float64
	fromDeg    *float64
	dirMissing string
	// span names what the wind was taken over, "spell" or "hour", for the reasons
	// that say so.
	span string
}

// spellWind is the wind kept with a recorded spell.
func spellWind(sp feltSpell) windInput {
	return windInput{kept: sp.WindKept, kts: sp.WindKts, fromDeg: sp.WindFromDeg, dirMissing: sp.WindDirMissing, span: "spell"}
}

// judgeSeaWind is judgeSea for the wind given.
func judgeSeaWind(wind windInput, sea *spellSea, src raySource) {
	if sea.Reason != "" || (sea.Swell == nil && sea.WindWave == nil) {
		return
	}
	rays := sync.OnceValues(src)
	if w := sea.WindWave; w != nil {
		w.Fetch = checkWindWave(wind, w, rays)
		w.Note = w.Fetch.note()
	}
	if s := sea.Swell; s != nil {
		// A swell that cannot be judged keeps the model's figure and says why, as
		// an unchecked wind wave does.
		get, why := rays()
		var res rayResult
		if get != nil {
			res, why = get(fetchKindSwell, s.FromDeg)
		}
		switch {
		case why != "":
			s.Unchecked, s.Note = true, swellUncheckedNote+why
		case res.OnLand:
			s.Unchecked, s.ashore, s.Note = true, true, swellUncheckedNote+reasonAshore
		case !res.Open:
			s.Enclosed, s.Note = true, swellEnclosedNote
		}
	}
}

const reasonAshore = "the position is charted ashore"

// coastlineReason words why the coastline could not be read, for the logbook.
func coastlineReason(err error) string {
	var ce *coastlineLookupError
	if !errors.As(err, &ce) {
		return "the Offline coastline couldn't be read: " + err.Error()
	}
	switch ce.Problem {
	case coastlineNotOnInstall:
		return "the Offline coastline isn't on this install"
	case coastlineOff:
		return "the Offline coastline is off; turn it on in Settings → Offline coastline"
	case coastlineSettingUnreadable:
		return "the Offline coastline setting couldn't be read"
	case coastlineInland:
		return "the position is inland, clear of the sea"
	case coastlineSquaresMissing:
		return "the Offline coastline is incomplete here: " + joinMissingPlain(ce.Missing)
	}
	return "the Offline coastline couldn't be read: " + err.Error()
}

// joinMissingPlain says which squares are missing and why, with no brackets: the
// page wraps the note in its own.
func joinMissingPlain(missing []missingSquare) string {
	parts := make([]string, len(missing))
	for i, m := range missing {
		switch {
		case strings.HasPrefix(m.State, "download failed"):
			parts[i] = "square " + m.Label + " " + strings.Replace(m.State, "download failed", "failed to download", 1)
		default:
			parts[i] = "square " + m.Label + " is " + m.State
		}
	}
	return strings.Join(parts, "; ")
}

// checkWindWave checks a spell's wind wave against its fetch and, when the shore
// caps it, replaces its figures with the cap. It asks for the rays only after the
// wind itself is known to be usable.
func checkWindWave(wind windInput, w *seaTrain, rays raySource) *fetchCheck {
	c := &fetchCheck{}
	unchecked := func(reason string) *fetchCheck {
		c.Check, c.Reason = seaCheckUnchecked, reason
		return c
	}
	switch {
	case !wind.kept:
		return unchecked("this " + wind.span + " was stored before wind was kept")
	case wind.kts == nil:
		return unchecked("no true wind was recorded for this " + wind.span)
	}
	tws := *wind.kts
	c.WindKts = &tws
	calm := tws < fetchCalmKts
	c.Calm = calm
	if !calm && wind.fromDeg == nil {
		if wind.dirMissing == windDirVaried {
			return unchecked("the wind direction varied too much during the " + wind.span)
		}
		return unchecked("no wind direction was recorded; it needs the heading and the wind angle")
	}
	get, why := rays()
	if get == nil {
		return unchecked(why)
	}
	// A calm has no direction, and its one row is kept under bearing 0.
	kind, bearing := fetchKindCalm, 0.0
	if !calm {
		kind, bearing = fetchKindWind, *wind.fromDeg
	}
	res, why := get(kind, bearing)
	switch {
	case why != "":
		return unchecked(why)
	case res.OnLand:
		c.Ashore = true
		return unchecked(reasonAshore)
	case res.Open:
		c.Check = seaCheckOpen
		return c
	}
	fetchM := res.DistM
	fetchNm := fetchM / metersPerNauticalMile
	hm0, tp := fetchWaveLimit(tws, fetchM)
	capPeriod := tp / fetchMeanPeriodRatio
	hindH, hindP := w.HeightM, w.PeriodS
	c.FetchNm, c.CapHeightM, c.CapPeriodS = &fetchNm, &hm0, &capPeriod
	if res.Reef {
		c.Caveat = reefTideCaveat
	}
	// Height and period are each held to their own limit, and a cut that does not
	// show at the page's rounding is no cut.
	if !heightCutShows(hindH, hm0) && !periodCutShows(hindP, capPeriod) {
		c.Check = seaCheckWithin
		return c
	}
	c.Check = seaCheckCapped
	c.HindcastHeightM, c.HindcastPeriodS = &hindH, &hindP
	w.HeightM = math.Min(hindH, hm0)
	w.PeriodS = math.Min(hindP, capPeriod)
	return c
}
