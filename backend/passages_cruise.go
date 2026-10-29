package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"sort"
	"strings"
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
	// SuggestionStatus is pending (not checked yet), found, none (checked, no
	// marina there: final) or failed (the check itself failed; it is retried).
	// SuggestionError names the class of failure when it failed.
	SuggestionStatus string  `json:"suggestion_status"`
	SuggestionError  string  `json:"suggestion_error,omitempty"`
	Label            *string `json:"label"`
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
	startZone, zerr := zoneAt(span[0].Lat, span[0].Lon)
	if zerr != nil {
		log.Printf("cruise stats: no time zone at the first track point, so nights and night SoC are left out: %v", zerr)
	} else {
		st.Nights = localMidnightsBetween(start, end, startZone.Loc)
		st.NightSoC = nightSoCFigures(span, start, end, startZone.Loc)
	}
	st.GeneratorHours = generatorHours(span)
	for _, p := range span {
		if p.SoCPct != nil && (st.LowestSoCPct == nil || *p.SoCPct < *st.LowestSoCPct) {
			v := *p.SoCPct
			st.LowestSoCPct = &v
		}
	}
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

// riseRow is a stored label or suggestion with the time range of the rise it
// was given to. Rises are found again on every read by a filter that can start
// them a little earlier or later when the span changes, so a row belongs to
// the current rise it overlaps most, never to an exact millisecond.
type riseRow struct {
	At, End time.Time
	Label   string
	Info    storedRiseInfo
}

func overlapMS(aStart, aEnd, bStart, bEnd time.Time) int64 {
	lo, hi := aStart, aEnd
	if bStart.After(lo) {
		lo = bStart
	}
	if bEnd.Before(hi) {
		hi = bEnd
	}
	if d := hi.Sub(lo).Milliseconds(); d > 0 {
		return d
	}
	if !hi.Before(lo) { // a single instant inside the other range
		return 1
	}
	return 0
}

// matchRiseRows pairs rises with stored rows, largest overlap first, each rise
// and each row used once. The result maps a rise index to a row index.
func matchRiseRows(rises []waterRise, rows []riseRow) map[int]int {
	type pair struct {
		ri, rj  int
		overlap int64
	}
	var pairs []pair
	for i, r := range rises {
		for j, row := range rows {
			if o := overlapMS(r.Start, r.End, row.At, row.End); o > 0 {
				pairs = append(pairs, pair{i, j, o})
			}
		}
	}
	sort.SliceStable(pairs, func(x, y int) bool { return pairs[x].overlap > pairs[y].overlap })
	out := map[int]int{}
	usedRow := map[int]bool{}
	for _, p := range pairs {
		if _, taken := out[p.ri]; taken || usedRow[p.rj] {
			continue
		}
		out[p.ri], usedRow[p.rj] = p.rj, true
	}
	return out
}

func riseEnd(atMs int64, end sql.NullInt64) time.Time {
	if end.Valid {
		return time.UnixMilli(end.Int64).UTC()
	}
	return time.UnixMilli(atMs).UTC()
}

func (s *passageStore) waterLabelRows(cruiseID string) ([]riseRow, error) {
	rows, err := s.db.Query(`SELECT at_ms, end_ms, label FROM water_rise_labels WHERE cruise_id = ? ORDER BY at_ms`, cruiseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []riseRow
	for rows.Next() {
		var ms int64
		var end sql.NullInt64
		var l string
		if err := rows.Scan(&ms, &end, &l); err != nil {
			return nil, err
		}
		out = append(out, riseRow{At: time.UnixMilli(ms).UTC(), End: riseEnd(ms, end), Label: l})
	}
	return out, rows.Err()
}

// SetWaterRiseLabel stores the operator's label for the water rise that
// contains at. An empty label clears it. A time inside no rise is refused.
func (s *passageStore) SetWaterRiseLabel(cruiseID string, at time.Time, label string) error {
	_, _, st, err := s.cruiseCore(cruiseID)
	if err != nil {
		return err
	}
	if label != "" && label != waterLabelWatermaker && label != waterLabelDockFill {
		return invalidf("%v", errLabelInvalid)
	}
	ri := -1
	for i, r := range st.WaterRises {
		if !at.Before(r.Start) && !at.After(r.End) {
			ri = i
			break
		}
	}
	if ri < 0 {
		return invalidf("no water rise at that time")
	}
	rise := st.WaterRises[ri]
	rows, err := s.waterLabelRows(cruiseID)
	if err != nil {
		return err
	}
	if j, ok := matchRiseRows(st.WaterRises, rows)[ri]; ok {
		if _, err := s.db.Exec(`DELETE FROM water_rise_labels WHERE cruise_id = ? AND at_ms = ?`, cruiseID, rows[j].At.UnixMilli()); err != nil {
			return err
		}
	}
	if label == "" {
		return nil
	}
	_, err = s.db.Exec(`INSERT OR REPLACE INTO water_rise_labels (cruise_id, at_ms, end_ms, label) VALUES (?,?,?,?)`,
		cruiseID, rise.Start.UnixMilli(), rise.End.UnixMilli(), label)
	return err
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
	s.cruiseCoreCalls.Add(1)
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

const (
	suggestionPending = "pending"
	suggestionFound   = "found"
	suggestionNone    = "none"
	suggestionFailed  = "failed"

	// marinaRetryAfter is how long a failed lookup waits before the job
	// tries it again.
	marinaRetryAfter = 6 * time.Hour
)

type storedRiseInfo struct {
	suggested *string
	lookedUp  time.Time
	status    string
	errClass  string
}

// classifyLookupError names the kind of failure without carrying its text into
// the API.
func classifyLookupError(err error) string {
	var ne net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()):
		return "timeout"
	case errors.As(err, &ne):
		return "network"
	case strings.Contains(err.Error(), "provider"):
		return "provider"
	}
	return "error"
}

func (s *passageStore) suggestionRows(cruiseID string) ([]riseRow, error) {
	rows, err := s.db.Query(`SELECT at_ms, end_ms, suggested, looked_up_ms, status, error_class FROM water_rise_suggestions WHERE cruise_id = ? ORDER BY at_ms`, cruiseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []riseRow
	for rows.Next() {
		var ms, looked int64
		var end sql.NullInt64
		var sug sql.NullString
		var status, class string
		if err := rows.Scan(&ms, &end, &sug, &looked, &status, &class); err != nil {
			return nil, err
		}
		info := storedRiseInfo{lookedUp: time.UnixMilli(looked).UTC(), status: status, errClass: class}
		if sug.Valid {
			v := sug.String
			info.suggested = &v
		}
		if info.status == "" { // a row from before statuses existed
			info.status = suggestionNone
			if info.suggested != nil {
				info.status = suggestionFound
			}
		}
		out = append(out, riseRow{At: time.UnixMilli(ms).UTC(), End: riseEnd(ms, end), Info: info})
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
	sort.Slice(st.WaterRises, func(i, j int) bool { return st.WaterRises[i].Start.Before(st.WaterRises[j].Start) })
	labelRows, err := s.waterLabelRows(cruiseID)
	if err != nil {
		return cr, nil, st, err
	}
	sugRows, err := s.suggestionRows(cruiseID)
	if err != nil {
		return cr, nil, st, err
	}
	labelAt := matchRiseRows(st.WaterRises, labelRows)
	sugAt := matchRiseRows(st.WaterRises, sugRows)
	for i := range st.WaterRises {
		r := &st.WaterRises[i]
		if j, ok := labelAt[i]; ok {
			l := labelRows[j].Label
			r.Label = &l
		}
		r.SuggestionStatus = suggestionPending
		if j, ok := sugAt[i]; ok {
			in := sugRows[j].Info
			t := in.lookedUp
			r.LookedUpAt, r.SuggestedLabel = &t, in.suggested
			r.SuggestionStatus, r.SuggestionError = in.status, in.errClass
		}
	}
	return cr, passages, st, nil
}

// LookUpNewWaterRises checks, once per rise, whether the rise happened beside a
// marina and stores the answer with the time of the check. No marina is a final
// answer. A failed check is stored as failed, with the class of failure, and is
// retried once marinaRetryAfter has passed.
func (s *passageStore) LookUpNewWaterRises() error { return s.LookUpNewWaterRisesAt(time.Now()) }

func (s *passageStore) LookUpNewWaterRisesAt(now time.Time) error {
	cruises, err := s.ListCruises()
	if err != nil {
		return err
	}
	due, err := s.cruisesWithRetryDue(now)
	if err != nil {
		return err
	}
	for _, cr := range cruises {
		fp, err := s.cruiseFingerprint(cr.ID)
		if err != nil {
			return err
		}
		s.marinaMu.Lock()
		unchanged := s.marinaSeen[cr.ID] == fp
		s.marinaMu.Unlock()
		if unchanged && !due[cr.ID] {
			continue
		}
		_, _, st, err := s.cruiseCore(cr.ID)
		if err != nil {
			return err
		}
		sort.Slice(st.WaterRises, func(i, j int) bool { return st.WaterRises[i].Start.Before(st.WaterRises[j].Start) })
		sugRows, err := s.suggestionRows(cr.ID)
		if err != nil {
			return err
		}
		labelRows, err := s.waterLabelRows(cr.ID)
		if err != nil {
			return err
		}
		sugAt := matchRiseRows(st.WaterRises, sugRows)
		labelAt := matchRiseRows(st.WaterRises, labelRows)
		for i, r := range st.WaterRises {
			// keep the stored ranges following the rises they belong to
			if j, ok := labelAt[i]; ok && (!labelRows[j].At.Equal(r.Start) || !labelRows[j].End.Equal(r.End)) {
				if _, err := s.db.Exec(`UPDATE water_rise_labels SET at_ms = ?, end_ms = ? WHERE cruise_id = ? AND at_ms = ?`,
					r.Start.UnixMilli(), r.End.UnixMilli(), cr.ID, labelRows[j].At.UnixMilli()); err != nil {
					return err
				}
			}
			j, have := sugAt[i]
			if have && !(sugRows[j].Info.status == suggestionFailed && now.Sub(sugRows[j].Info.lookedUp) >= marinaRetryAfter) {
				if !sugRows[j].At.Equal(r.Start) || !sugRows[j].End.Equal(r.End) {
					if _, err := s.db.Exec(`UPDATE water_rise_suggestions SET at_ms = ?, end_ms = ? WHERE cruise_id = ? AND at_ms = ?`,
						r.Start.UnixMilli(), r.End.UnixMilli(), cr.ID, sugRows[j].At.UnixMilli()); err != nil {
						return err
					}
				}
				continue
			}
			var suggested any
			status, class := suggestionNone, ""
			near, lerr := marinaNear(r.Lat, r.Lon)
			switch {
			case lerr != nil:
				status, class = suggestionFailed, classifyLookupError(lerr)
				log.Printf("passages: marina lookup for water rise at %s failed (%s), will retry: %v", r.Start.Format(time.RFC3339), class, lerr)
			case near:
				status, suggested = suggestionFound, waterLabelDockFill
			}
			if have {
				if _, err := s.db.Exec(`DELETE FROM water_rise_suggestions WHERE cruise_id = ? AND at_ms = ?`, cr.ID, sugRows[j].At.UnixMilli()); err != nil {
					return err
				}
			}
			if _, err := s.db.Exec(`INSERT OR REPLACE INTO water_rise_suggestions (cruise_id, at_ms, end_ms, suggested, looked_up_ms, status, error_class) VALUES (?,?,?,?,?,?,?)`,
				cr.ID, r.Start.UnixMilli(), r.End.UnixMilli(), suggested, now.UnixMilli(), status, class); err != nil {
				return err
			}
		}
		s.marinaMu.Lock()
		if s.marinaSeen == nil {
			s.marinaSeen = map[string]string{}
		}
		s.marinaSeen[cr.ID] = fp
		s.marinaMu.Unlock()
	}
	return nil
}

// cruiseFingerprint is a cheap summary of what decides a cruise's water rises:
// which visible passages it holds and their ranges. A closed passage's points
// do not change, so the same fingerprint means the same rises.
func (s *passageStore) cruiseFingerprint(cruiseID string) (string, error) {
	var n int
	var starts, ends int64
	var ids sql.NullString
	if err := s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(start_ms),0), COALESCE(SUM(end_ms),0), group_concat(id) FROM passages WHERE cruise_id = ? AND hidden = 0`, cruiseID).Scan(&n, &starts, &ends, &ids); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d/%d/%d/%s", n, starts, ends, ids.String), nil
}

// cruisesWithRetryDue are the cruises holding a failed lookup old enough to try again.
func (s *passageStore) cruisesWithRetryDue(now time.Time) (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT DISTINCT cruise_id FROM water_rise_suggestions WHERE status = ? AND looked_up_ms <= ?`,
		suggestionFailed, now.Add(-marinaRetryAfter).UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
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
