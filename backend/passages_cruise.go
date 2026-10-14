package main

import (
	"database/sql"
	"errors"
	"log"
	"sort"
	"time"
)

// Cruise-level figures (plan step 4). A cruise's span runs from its first
// departure to its last arrival and includes the nights at anchor between, so
// generator hours, water and battery are read across the whole span from the
// track points. Anything the readings do not support is nil.

const (
	waterDeadbandFraction = 0.01
	dawnSoCMaxAge         = 15 * time.Minute

	waterLabelWatermaker = "Watermaker"
	waterLabelDockFill   = "Dock fill"
	// marinaRadiusMeters is how close a marina feature must be to the place
	// water went into the tank for the rise to be suggested as a dock fill.
	marinaRadiusMeters = 400
)

type nightSoC struct {
	// Night is the local date the night began on.
	Night      string    `json:"night"`
	Sunset     time.Time `json:"sunset"`
	Sunrise    time.Time `json:"sunrise"`
	MinSoCPct  *float64  `json:"min_soc_pct"`
	DawnSoCPct *float64  `json:"dawn_soc_pct"`
}

type waterRise struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Litres float64   `json:"litres"`
	Lat    float64   `json:"lat"`
	Lon    float64   `json:"lon"`
	// SuggestedLabel is only ever Dock fill, and only when a marina was found
	// beside the rise. Anywhere else it is nil: open water does not prove a
	// watermaker. Label is what the operator confirmed.
	SuggestedLabel *string `json:"suggested_label"`
	// LookedUpAt is when the marina check ran; nil means it has not yet.
	LookedUpAt *time.Time `json:"looked_up_at"`
	Label      *string    `json:"label"`
}

type cruiseStats struct {
	PassageCount    int        `json:"passage_count"`
	Departure       *time.Time `json:"departure"`
	Arrival         *time.Time `json:"arrival"`
	SpanSeconds     float64    `json:"span_seconds"`
	UnderwaySeconds float64    `json:"underway_seconds"`
	Nights          int        `json:"nights"`
	DistanceNM      float64    `json:"distance_nm"`
	FuelBurnedL     *float64   `json:"fuel_burned_l"`
	// Wind is reported twice: points inside the passages, and points in the
	// span outside them (the nights and days at anchor).
	UnderwayTWSMeanKts *float64    `json:"underway_tws_mean_kts"`
	UnderwayMaxGustKts *float64    `json:"underway_max_gust_kts"`
	AnchorTWSMeanKts   *float64    `json:"anchor_tws_mean_kts"`
	AnchorMaxGustKts   *float64    `json:"anchor_max_gust_kts"`
	GeneratorHours     *float64    `json:"generator_hours"`
	LowestSoCPct       *float64    `json:"lowest_soc_pct"`
	NightSoC           []nightSoC  `json:"night_soc"`
	WaterUsedL         *float64    `json:"water_used_l"`
	WaterTakenOnL      *float64    `json:"water_taken_on_l"`
	WaterRises         []waterRise `json:"water_rises"`
}

// computeCruiseStats takes the cruise's passages with each passage's points,
// and the points of the whole span.
func computeCruiseStats(passages []passageRecord, perPassage [][]recordedTrackPoint, span []recordedTrackPoint) cruiseStats {
	st := cruiseStats{PassageCount: len(passages), NightSoC: []nightSoC{}, WaterRises: []waterRise{}}
	if len(passages) == 0 {
		return st
	}
	start, end := passages[0].Start, passages[0].End
	for _, p := range passages[1:] {
		if p.Start.Before(start) {
			start = p.Start
		}
		if p.End.After(end) {
			end = p.End
		}
	}
	st.Departure, st.Arrival = &start, &end
	st.SpanSeconds = end.Sub(start).Seconds()

	var (
		fuel         float64
		fuelComplete = true
	)
	for _, pts := range perPassage {
		ps := computePassageStats(pts)
		st.DistanceNM += ps.DistanceNM
		st.UnderwaySeconds += ps.UnderwaySeconds
		if ps.FuelBurnedL == nil {
			fuelComplete = false
		} else {
			fuel += *ps.FuelBurnedL
		}
	}
	if fuelComplete {
		st.FuelBurnedL = &fuel
	}
	var underway, anchored []recordedTrackPoint
	for _, pts := range perPassage {
		underway = append(underway, pts...)
	}
	for _, p := range span {
		inside := false
		for _, ps := range passages {
			if !p.At.Before(ps.Start) && !p.At.After(ps.End) {
				inside = true
				break
			}
		}
		if !inside {
			anchored = append(anchored, p)
		}
	}
	st.UnderwayTWSMeanKts, st.UnderwayMaxGustKts = windFigures(underway)
	st.AnchorTWSMeanKts, st.AnchorMaxGustKts = windFigures(anchored)

	if len(span) == 0 {
		return st
	}
	loc := vesselLocalLocation(span[0].Lon)
	st.Nights = localMidnightsBetween(start, end, loc)
	st.GeneratorHours = generatorHours(span)
	for _, p := range span {
		if p.SoCPct != nil && (st.LowestSoCPct == nil || *p.SoCPct < *st.LowestSoCPct) {
			v := *p.SoCPct
			st.LowestSoCPct = &v
		}
	}
	st.NightSoC = nightSoCFigures(span, start, end, loc)
	st.WaterRises, st.WaterUsedL, st.WaterTakenOnL = waterFigures(span)
	return st
}

// windFigures is the mean true wind speed over every sample and the highest
// gust, each nil when no point carries the reading.
func windFigures(pts []recordedTrackPoint) (mean, maxGust *float64) {
	var sum float64
	n := 0
	for _, p := range pts {
		if p.TWSKts != nil {
			sum += *p.TWSKts
			n++
		}
		if p.GustKts != nil && (maxGust == nil || *p.GustKts > *maxGust) {
			v := *p.GustKts
			maxGust = &v
		}
	}
	if n > 0 {
		v := sum / float64(n)
		mean = &v
	}
	return
}

func localMidnightsBetween(start, end time.Time, loc *time.Location) int {
	s := start.In(loc)
	next := time.Date(s.Year(), s.Month(), s.Day()+1, 0, 0, 0, 0, loc)
	n := 0
	for next.Before(end) {
		n++
		next = next.AddDate(0, 0, 1)
	}
	return n
}

// generatorHours sums the intervals the generator was running, as the
// recorder forces a point at every start and stop. A hole in the record counts
// for nothing. Nil when the log never carried a generator state.
func generatorHours(pts []recordedTrackPoint) *float64 {
	seen := false
	var secs float64
	for i, p := range pts {
		if p.GenState != nil {
			seen = true
		}
		if i == 0 || !legBetween(pts[i-1], p) {
			continue
		}
		if prev := pts[i-1]; prev.GenState != nil && *prev.GenState == "running" {
			secs += p.At.Sub(prev.At).Seconds()
		}
	}
	if !seen {
		return nil
	}
	h := secs / 3600
	return &h
}

func nightSoCFigures(pts []recordedTrackPoint, start, end time.Time, loc *time.Location) []nightSoC {
	out := []nightSoC{}
	if len(pts) == 0 {
		return out
	}
	lat, lon := pts[0].Lat, pts[0].Lon
	first := start.In(loc)
	day := time.Date(first.Year(), first.Month(), first.Day()-1, 12, 0, 0, 0, loc)
	for ; day.Before(end); day = day.AddDate(0, 0, 1) {
		_, sunset, ok1 := sunTimes(day, lat, lon)
		sunrise, _, ok2 := sunTimes(day.AddDate(0, 0, 1), lat, lon)
		if !ok1 || !ok2 {
			continue
		}
		if sunset.Before(start) || sunrise.After(end) {
			continue
		}
		n := nightSoC{Night: day.Format("2006-01-02"), Sunset: sunset, Sunrise: sunrise}
		for _, p := range pts {
			if p.SoCPct == nil || p.At.Before(sunset) || p.At.After(sunrise) {
				continue
			}
			if n.MinSoCPct == nil || *p.SoCPct < *n.MinSoCPct {
				v := *p.SoCPct
				n.MinSoCPct = &v
			}
		}
		var dawn *recordedTrackPoint
		for i := range pts {
			if pts[i].SoCPct != nil && !pts[i].At.After(sunrise) {
				dawn = &pts[i]
			}
		}
		if dawn != nil && sunrise.Sub(dawn.At) <= dawnSoCMaxAge {
			v := *dawn.SoCPct
			n.DawnSoCPct = &v
		}
		out = append(out, n)
	}
	return out
}

// waterFigures finds the rises and falls of the fresh water level. Moves
// smaller than 1% of the fullest the tanks were in the span are slosh and
// gauge noise and are ignored (a zig-zag filter on that threshold).
func waterFigures(pts []recordedTrackPoint) (rises []waterRise, used, taken *float64) {
	rises = []waterRise{}
	var lv []recordedTrackPoint
	maxL := 0.0
	for _, p := range pts {
		if p.WaterL != nil {
			lv = append(lv, p)
			if *p.WaterL > maxL {
				maxL = *p.WaterL
			}
		}
	}
	if len(lv) < 2 || maxL <= 0 {
		return
	}
	theta := waterDeadbandFraction * maxL

	type pivot struct {
		idx  int
		high bool
	}
	var pivots []pivot
	lo, hi := 0, 0
	trend := 0 // +1 rising, -1 falling
	ext := 0
	for i := 1; i < len(lv); i++ {
		v := *lv[i].WaterL
		switch trend {
		case 0:
			if v < *lv[lo].WaterL {
				lo = i
			}
			if v > *lv[hi].WaterL {
				hi = i
			}
			switch {
			case v-*lv[lo].WaterL >= theta && lo < i:
				pivots = append(pivots, pivot{lo, false})
				trend, ext = 1, i
			case *lv[hi].WaterL-v >= theta && hi < i:
				pivots = append(pivots, pivot{hi, true})
				trend, ext = -1, i
			}
		case 1:
			if v > *lv[ext].WaterL {
				ext = i
			} else if *lv[ext].WaterL-v >= theta {
				pivots = append(pivots, pivot{ext, true})
				trend, ext = -1, i
			}
		case -1:
			if v < *lv[ext].WaterL {
				ext = i
			} else if v-*lv[ext].WaterL >= theta {
				pivots = append(pivots, pivot{ext, false})
				trend, ext = 1, i
			}
		}
	}
	if trend != 0 {
		pivots = append(pivots, pivot{ext, trend == 1})
	}
	if len(pivots) < 2 {
		z := 0.0
		return rises, &z, &z
	}
	var u, tk float64
	for i := 1; i < len(pivots); i++ {
		a, b := lv[pivots[i-1].idx], lv[pivots[i].idx]
		delta := *b.WaterL - *a.WaterL
		if delta < 0 {
			u += -delta
			continue
		}
		tk += delta
		rises = append(rises, waterRise{Start: a.At, End: b.At, Litres: delta, Lat: a.Lat, Lon: a.Lon})
	}
	return rises, &u, &tk
}

// ── labels and the marina suggestion ──────────────────────────────────────

var errLabelInvalid = errors.New("label must be Watermaker or Dock fill")

// SetWaterRiseLabel stores the operator's label for the rise starting at at.
// An empty label clears it.
func (s *passageStore) SetWaterRiseLabel(cruiseID string, at time.Time, label string) error {
	if _, err := s.GetCruise(cruiseID); err != nil {
		return err
	}
	if label == "" {
		_, err := s.db.Exec(`DELETE FROM water_rise_labels WHERE cruise_id = ? AND at_ms = ?`, cruiseID, at.UnixMilli())
		return err
	}
	if label != waterLabelWatermaker && label != waterLabelDockFill {
		return invalidf("%v", errLabelInvalid)
	}
	_, err := s.db.Exec(`INSERT INTO water_rise_labels (cruise_id, at_ms, label) VALUES (?,?,?)
		ON CONFLICT(cruise_id, at_ms) DO UPDATE SET label = excluded.label`, cruiseID, at.UnixMilli(), label)
	return err
}

func (s *passageStore) waterRiseLabels(cruiseID string) (map[int64]string, error) {
	rows, err := s.db.Query(`SELECT at_ms, label FROM water_rise_labels WHERE cruise_id = ?`, cruiseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var ms int64
		var l string
		if err := rows.Scan(&ms, &l); err != nil {
			return nil, err
		}
		out[ms] = l
	}
	return out, rows.Err()
}

// marinaNear reports whether the place-data plugin knows a marina within
// radius of the point. It is a variable so tests can supply an answer.
var marinaNear = func(lat, lon float64) (bool, error) {
	provider, _, err := resolvePOIProvider(getEnv("SETTINGS_FILE", "../settings.yaml"))
	if err != nil {
		return false, err
	}
	res, err := provider.FetchPOI(lat, lon, marinaRadiusMeters, []string{"marina"}, 5)
	if err != nil {
		return false, err
	}
	for _, f := range res.Features {
		if f.Category == "marina" && haversineMeters(lat, lon, f.Lat, f.Lon) <= marinaRadiusMeters {
			return true, nil
		}
	}
	return false, nil
}

// cruiseCore loads a cruise and works out its figures from the track points.
// Water labels and suggestions are not applied here.
func (s *passageStore) cruiseCore(cruiseID string) (cruiseRecord, []passageRecord, cruiseStats, error) {
	cr, err := s.GetCruise(cruiseID)
	if err != nil {
		return cr, nil, cruiseStats{}, err
	}
	passages, err := s.CruisePassages(cruiseID)
	if err != nil {
		return cr, nil, cruiseStats{}, err
	}
	var per [][]recordedTrackPoint
	var span []recordedTrackPoint
	if len(passages) > 0 {
		from, to := passages[0].Start, passages[0].End
		for _, p := range passages {
			pts, err := s.TrackPointsBetween(p.Start, p.End)
			if err != nil {
				return cr, nil, cruiseStats{}, err
			}
			per = append(per, pts)
			if p.Start.Before(from) {
				from = p.Start
			}
			if p.End.After(to) {
				to = p.End
			}
		}
		if span, err = s.TrackPointsBetween(from, to); err != nil {
			return cr, nil, cruiseStats{}, err
		}
	}
	return cr, passages, computeCruiseStats(passages, per, span), nil
}

type storedRiseInfo struct {
	suggested *string
	lookedUp  time.Time
}

func (s *passageStore) storedRiseInfo(cruiseID string) (map[int64]storedRiseInfo, error) {
	rows, err := s.db.Query(`SELECT at_ms, suggested, looked_up_ms FROM water_rise_suggestions WHERE cruise_id = ?`, cruiseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]storedRiseInfo{}
	for rows.Next() {
		var ms, looked int64
		var sug sql.NullString
		if err := rows.Scan(&ms, &sug, &looked); err != nil {
			return nil, err
		}
		info := storedRiseInfo{lookedUp: time.UnixMilli(looked).UTC()}
		if sug.Valid {
			v := sug.String
			info.suggested = &v
		}
		out[ms] = info
	}
	return out, rows.Err()
}

// CruiseStats is the cruise's figures with the operator's water labels and the
// stored marina suggestions applied. It only reads: nothing here goes out to a
// plugin.
func (s *passageStore) CruiseStats(cruiseID string) (cruiseRecord, []passageRecord, cruiseStats, error) {
	cr, passages, st, err := s.cruiseCore(cruiseID)
	if err != nil {
		return cr, nil, st, err
	}
	labels, err := s.waterRiseLabels(cruiseID)
	if err != nil {
		return cr, nil, st, err
	}
	info, err := s.storedRiseInfo(cruiseID)
	if err != nil {
		return cr, nil, st, err
	}
	for i := range st.WaterRises {
		r := &st.WaterRises[i]
		if l, ok := labels[r.Start.UnixMilli()]; ok {
			l := l
			r.Label = &l
		}
		if in, ok := info[r.Start.UnixMilli()]; ok {
			t := in.lookedUp
			r.LookedUpAt, r.SuggestedLabel = &t, in.suggested
		}
	}
	sort.Slice(st.WaterRises, func(i, j int) bool { return st.WaterRises[i].Start.Before(st.WaterRises[j].Start) })
	return cr, passages, st, nil
}

// LookUpNewWaterRises checks, once per rise, whether the rise happened beside a
// marina and stores the answer with the time of the check. A rise already
// checked is skipped. A failed check is logged and stored as no suggestion, so
// it is not repeated on every pass.
func (s *passageStore) LookUpNewWaterRises() error {
	cruises, err := s.ListCruises()
	if err != nil {
		return err
	}
	for _, cr := range cruises {
		_, _, st, err := s.cruiseCore(cr.ID)
		if err != nil {
			return err
		}
		done, err := s.storedRiseInfo(cr.ID)
		if err != nil {
			return err
		}
		for _, r := range st.WaterRises {
			if _, ok := done[r.Start.UnixMilli()]; ok {
				continue
			}
			var suggested any
			near, lerr := marinaNear(r.Lat, r.Lon)
			switch {
			case lerr != nil:
				log.Printf("passages: marina lookup for water rise at %s failed, stored as no suggestion: %v", r.Start.Format(time.RFC3339), lerr)
			case near:
				suggested = waterLabelDockFill
			}
			if _, err := s.db.Exec(`INSERT OR REPLACE INTO water_rise_suggestions (cruise_id, at_ms, suggested, looked_up_ms) VALUES (?,?,?,?)`,
				cr.ID, r.Start.UnixMilli(), suggested, time.Now().UnixMilli()); err != nil {
				return err
			}
		}
	}
	return nil
}

// startMarinaLookups runs the lookups in the background: once at start, then on
// a wake (a passage filed into a cruise) and on a timer.
func startMarinaLookups(store *passageStore) {
	signal := make(chan struct{}, 1)
	marinaLookupWake = func() {
		select {
		case signal <- struct{}{}:
		default:
		}
	}
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			if err := store.LookUpNewWaterRises(); err != nil {
				log.Printf("passages: water rise lookups: %v", err)
			}
			select {
			case <-signal:
			case <-ticker.C:
			}
		}
	}()
}

// marinaLookupWake nudges the lookup job; nil until it is running.
var marinaLookupWake func()
