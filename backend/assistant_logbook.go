package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// The passage tools (ADR 0219). Mate reads the recorded Passages logbook, the
// store behind /api/passages and the Passages page, and not the telemetry:
// names, times, distance, fuel, rating and notes are what the operator sees
// and has edited. list_passages (ADR 0166) used to detect runs in InfluxDB's
// speed over ground, which stopped at 60 days and knew no place names.
//
// Nothing here infers geography. A place is whatever the record says, a blank
// stays blank, and a stay is only the gap between two passages.

const (
	logbookDefaultLimit = 20
	logbookMaxLimit     = 100

	// logbookMinStay is the shortest gap between two passages counted as a
	// stay. A shorter one is the same day's hops, not a stop.
	logbookMinStay = 6 * time.Hour

	logbookTopLegs  = 3
	logbookTopStays = 10
)

// logbookRange bounds the passages by departure. A bare date is a day on the
// boat's clock, the zone at the passage's own start position (ADR 0228), so
// "August" is the boat's August. An RFC3339
// bound is an instant.
type logbookRange struct {
	from, to       time.Time // instants; zero means unbounded; to is exclusive
	fromDay, toDay string    // YYYY-MM-DD, set instead of the instant for a bare date
}

// has reports whether a departure is in the range. A bare date is a day in the
// passage's own zone. A passage with no zone cannot be placed on one, so it is
// counted in when its instant is near enough to the dates for some zone from
// UTC-12 to UTC+14 to put it inside them (its row says its zone is not known),
// and out when it is further away than that.
func (r logbookRange) has(t time.Time, zone *placeZone) bool {
	if r.fromDay != "" || r.toDay != "" {
		if zone == nil {
			if r.fromDay != "" {
				lo, _ := time.Parse("2006-01-02", r.fromDay)
				if t.Before(lo.Add(-14 * time.Hour)) {
					return false
				}
			}
			if r.toDay != "" {
				hi, _ := time.Parse("2006-01-02", r.toDay)
				if !t.Before(hi.Add(36 * time.Hour)) {
					return false
				}
			}
		} else {
			day := t.In(zone.Loc).Format("2006-01-02")
			if r.fromDay != "" && day < r.fromDay {
				return false
			}
			if r.toDay != "" && day > r.toDay {
				return false
			}
		}
	}
	return (r.from.IsZero() || !t.Before(r.from)) && (r.to.IsZero() || t.Before(r.to))
}

// parseLogbookBound reads a YYYY-MM-DD date or an RFC3339 time. A date comes
// back as its day string. An RFC3339 upper bound is inclusive to the second.
func parseLogbookBound(name, v string, upper bool) (t time.Time, day string, err error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, "", nil
	}
	if d, e := time.Parse("2006-01-02", v); e == nil {
		return time.Time{}, d.Format("2006-01-02"), nil
	}
	if p, e := time.Parse(time.RFC3339, v); e == nil {
		if upper {
			p = p.Add(time.Second)
		}
		return p.UTC(), "", nil
	}
	return time.Time{}, "", fmt.Errorf("%s %q is not a date: use YYYY-MM-DD or RFC3339", name, v)
}

func parseLogbookRange(from, to string) (logbookRange, error) {
	var r logbookRange
	var err error
	if r.from, r.fromDay, err = parseLogbookBound("from", from, false); err != nil {
		return r, err
	}
	if r.to, r.toDay, err = parseLogbookBound("to", to, true); err != nil {
		return r, err
	}
	lo, hi := r.from, r.to
	if r.fromDay != "" {
		lo, _ = time.Parse("2006-01-02", r.fromDay)
	}
	if r.toDay != "" {
		hi, _ = time.Parse("2006-01-02", r.toDay)
		hi = hi.AddDate(0, 0, 1)
	}
	if !lo.IsZero() && !hi.IsZero() && !lo.Before(hi) {
		return r, errors.New("from must be before to")
	}
	return r, nil
}

// localTime is t on the clock of the zone it was recorded in, such as
// "2026-10-27 10:16 AEST (UTC+10)". The label is worked out at t, because
// daylight saving makes it depend on the date. With no zone it is empty: a
// passage with no recorded position has no local time to give.
func localTime(t time.Time, zone *placeZone) string {
	if zone == nil {
		return ""
	}
	return t.In(zone.Loc).Format("2006-01-02 15:04") + " " + zone.Label(t)
}

// passageZone is the zone a recorded passage keeps: the one at its first
// recorded point. A passage that has none, or whose zone name is unreadable,
// gets no zone and the reason in words. That is that passage's problem and does
// not stop the others.
func passageZone(timezone *string, timezoneError string) (*placeZone, string) {
	if timezone == nil {
		if timezoneError == "" {
			timezoneError = "no position was recorded for it"
		}
		return nil, timezoneError
	}
	z, err := zoneAtName(*timezone)
	if err != nil {
		return nil, "its zone name could not be read: " + err.Error()
	}
	return &z, ""
}

// zoneUnknownNote is a row's statement that its time zone is missing.
func zoneUnknownNote(reason string) string {
	if reason == "" {
		return ""
	}
	return "The time zone is not known (" + reason + "), so there is no local time for it."
}

// openRunZone is the zone at the first point of the run under way, or the
// reason it has none.
func openRunZone(pts []recordedTrackPoint, start time.Time) (*placeZone, string) {
	for _, p := range pts {
		if !p.At.Before(start) {
			zone, err := zoneAt(p.Lat, p.Lon)
			if err != nil {
				return nil, "its start position has no time zone: " + err.Error()
			}
			return &zone, ""
		}
	}
	return nil, "no position was recorded for its start, so its time zone is not known"
}

// passageRatingLabels are the labels the Passages page shows for a rating.
var passageRatingLabels = map[string]string{
	"comfortable":        "Comfortable",
	"lively":             "Lively",
	"shouldnt_have_gone": "Shouldn't have gone",
}

func ratingLabel(r string) string {
	if l, ok := passageRatingLabels[r]; ok {
		return l
	}
	return r
}

type logbookPassage struct {
	passageRecord
	stats passageStats
	zone  *placeZone
	// zoneReason says why zone is nil, for a row that could be in the range.
	zoneReason string
}

// logbookOpen is a passage still under way, from the track log.
type logbookOpen struct {
	start time.Time
	nm    float64
	zone  *placeZone
	// zoneReason says why zone is nil.
	zoneReason string
}

// logbookView is the visible passages whose departure falls in the range,
// oldest first, with the cruise names, the hidden passages' spans, the whole
// logbook's span, and the passage under way if there is one.
type logbookView struct {
	passages    []logbookPassage
	cruiseNames map[string]string
	hidden      [][2]time.Time
	total       int
	first, last time.Time // whole logbook, not just the range
	open        *logbookOpen
}

func readLogbook(ctx context.Context, d assistantToolDeps, tool string, r logbookRange) (logbookView, error) {
	var v logbookView
	if d.passageLog == nil {
		return v, fmt.Errorf("%s: the passage log is not running", tool)
	}
	store := d.passageLog()
	if store == nil {
		return v, fmt.Errorf("%s: the passage log is not running", tool)
	}
	if err := ctx.Err(); err != nil {
		return v, err
	}
	all, err := store.ListPassageSummaries() // newest first
	if err != nil {
		return v, fmt.Errorf("%s: %w", tool, err)
	}
	if err := ctx.Err(); err != nil {
		return v, err
	}
	cruises, err := store.ListCruises()
	if err != nil {
		return v, fmt.Errorf("%s: %w", tool, err)
	}
	v.cruiseNames = map[string]string{}
	for _, c := range cruises {
		v.cruiseNames[c.ID] = c.Name
	}
	if v.hidden, err = store.hiddenPassageSpans(); err != nil {
		return v, fmt.Errorf("%s: %w", tool, err)
	}
	v.total = len(all)
	if len(all) > 0 {
		v.first = all[len(all)-1].Start
	}
	for i := len(all) - 1; i >= 0; i-- {
		p := all[i]
		if p.End.After(v.last) {
			v.last = p.End
		}
		zone, reason := passageZone(p.Timezone, p.TimezoneError)
		if !r.has(p.Start, zone) {
			continue
		}
		v.passages = append(v.passages, logbookPassage{passageRecord: p.passageRecord, stats: p.Stats, zone: zone, zoneReason: reason})
	}
	sort.SliceStable(v.passages, func(i, j int) bool { return v.passages[i].Start.Before(v.passages[j].Start) })
	if v.open, err = openPassage(store); err != nil {
		return v, fmt.Errorf("%s: %w", tool, err)
	}
	if v.open != nil {
		if !r.has(v.open.start, v.open.zone) {
			v.open = nil
		}
	}
	return v, nil
}

// hiddenPassageSpans is the start and end of every hidden passage.
func (s *passageStore) hiddenPassageSpans() ([][2]time.Time, error) {
	rows, err := s.db.Query(`SELECT start_ms, end_ms FROM passages WHERE hidden = 1 ORDER BY start_ms`)
	if err != nil {
		return nil, fmt.Errorf("list hidden passages: %w", err)
	}
	defer rows.Close()
	var out [][2]time.Time
	for rows.Next() {
		var a, b int64
		if err := rows.Scan(&a, &b); err != nil {
			return nil, err
		}
		out = append(out, [2]time.Time{time.UnixMilli(a).UTC(), time.UnixMilli(b).UTC()})
	}
	return out, rows.Err()
}

// openPassage runs the passage detector over the track points since it last
// stopped, as the recorder does, and returns the run still under way, or nil.
// It reads the track log in SQLite, not the telemetry.
func openPassage(store *passageStore) (*logbookOpen, error) {
	resume, ok, err := store.DetectorResume()
	if err != nil {
		return nil, err
	}
	if !ok {
		first, any, err := store.FirstTrackPointTime()
		if err != nil || !any {
			return nil, err
		}
		resume = first
	}
	pts, err := store.TrackPointsSince(resume)
	if err != nil || len(pts) == 0 {
		return nil, err
	}
	o := detectLoggedPassages(pts).Open
	if o == nil || o.End.Sub(o.Departed) < passageMinDuration || o.DistanceNM < passageMinDistanceNM {
		return nil, nil
	}
	out := &logbookOpen{start: o.Start, nm: o.DistanceNM}
	out.zone, out.zoneReason = openRunZone(pts, o.Start)
	return out, nil
}

// addUnderWay puts a passage in progress into a result, apart from the
// logbook's figures, and says it is not finished.
func (v logbookView) addUnderWay(res map[string]any) {
	if v.open == nil {
		return
	}
	underWay := map[string]any{
		"start": rfc3339(v.open.start), "start_local": localTime(v.open.start, v.open.zone),
		"distance_nm_so_far": round1(v.open.nm),
	}
	if note := zoneUnknownNote(v.open.zoneReason); note != "" {
		underWay["time_zone_note"] = note
	}
	res["under_way_now"] = underWay
	n, _ := res["note"].(string)
	res["note"] = n + " The boat is under way now (under_way_now): that passage joins the logbook when it ends, and it is not in found, the count or the totals."
}

func (v logbookView) cruiseName(p passageRecord) string {
	if p.CruiseID == nil {
		return ""
	}
	return v.cruiseNames[*p.CruiseID]
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// emptyResult is the plain statement for a logbook with nothing to read. It
// is given as it is: the tools do not go looking in the telemetry instead.
func (v logbookView) emptyResult() map[string]any {
	res := map[string]any{"found": 0, "passages": []any{}}
	if v.total == 0 {
		res["status"] = "empty_logbook"
		res["note"] = "The Passages logbook has no passages yet, so there is nothing to report. Say so. Do not look for passages in the telemetry: passages come from the logbook, not from the telemetry."
		return res
	}
	res["status"] = "none_in_range"
	res["logbook_first_departure"] = rfc3339(v.first)
	res["logbook_last_arrival"] = rfc3339(v.last)
	res["note"] = "The logbook holds passages, but none departed in the range asked for. Say what range the logbook does cover (logbook_first_departure to logbook_last_arrival)."
	return res
}

type assistantPassagesArgs struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Limit *int   `json:"limit"`
}

type assistantPassageJSON struct {
	Start         string   `json:"start"`
	End           string   `json:"end"`
	StartLocal    string   `json:"start_local"`
	EndLocal      string   `json:"end_local"`
	From          string   `json:"from"`
	To            string   `json:"to"`
	Title         string   `json:"title,omitempty"`
	Cruise        string   `json:"cruise,omitempty"`
	Rating        string   `json:"rating,omitempty"`
	Notes         string   `json:"notes,omitempty"`
	DurationHours float64  `json:"duration_hours"`
	UnderwayHours float64  `json:"underway_hours"`
	DistanceNm    float64  `json:"distance_nm"`
	MeanSOGKts    *float64 `json:"mean_sog_kts"`
	MaxSOGKts     *float64 `json:"max_sog_kts"`
	FuelBurnedL   *float64 `json:"fuel_burned_l"`
	MeanTWSKts    *float64 `json:"mean_true_wind_kts"`
	MaxGustKts    *float64 `json:"max_gust_kts"`
	// TimeZoneNote says why there is no local time, when the passage has no zone.
	TimeZoneNote string `json:"time_zone_note,omitempty"`
}

func (v logbookView) passageJSON(p logbookPassage) assistantPassageJSON {
	return assistantPassageJSON{
		Start: rfc3339(p.Start), End: rfc3339(p.End),
		StartLocal: localTime(p.Start, p.zone), EndLocal: localTime(p.End, p.zone),
		From: p.FromName, To: p.ToName, Title: p.Title,
		Cruise: v.cruiseName(p.passageRecord), Rating: ratingLabel(p.Rating), Notes: p.Notes,
		DurationHours: round1(p.End.Sub(p.Start).Hours()),
		UnderwayHours: round1(p.stats.UnderwaySeconds / 3600),
		DistanceNm:    round1(p.stats.DistanceNM),
		MeanSOGKts:    roundPtr1(p.stats.MeanSOGKts), MaxSOGKts: roundPtr1(p.stats.MaxSOGKts),
		FuelBurnedL: roundPtr1(p.stats.FuelBurnedL),
		MeanTWSKts:  roundPtr1(p.stats.TWSMeanKts), MaxGustKts: roundPtr1(p.stats.MaxGustKts),
		TimeZoneNote: zoneUnknownNote(p.zoneReason),
	}
}

func roundPtr1(v *float64) *float64 {
	if v == nil {
		return nil
	}
	r := round1(*v)
	return &r
}

func (d assistantToolDeps) executeListPassages(ctx context.Context, raw json.RawMessage) (string, error) {
	var args assistantPassagesArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parse list_passages arguments: %w", err)
		}
	}
	r, err := parseLogbookRange(args.From, args.To)
	if err != nil {
		return "", fmt.Errorf("list_passages: %w", err)
	}
	limit := logbookDefaultLimit
	if args.Limit != nil && *args.Limit > 0 {
		limit = *args.Limit
	}
	if limit > logbookMaxLimit {
		limit = logbookMaxLimit
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	v, err := readLogbook(ctx, d, "list_passages", r)
	if err != nil {
		return "", err
	}
	var res map[string]any
	if len(v.passages) == 0 {
		res = v.emptyResult()
	} else {
		found := len(v.passages)
		newest := v.passages
		for i, j := 0, len(newest)-1; i < j; i, j = i+1, j-1 {
			newest[i], newest[j] = newest[j], newest[i]
		}
		if len(newest) > limit {
			newest = newest[:limit]
		}
		out := make([]assistantPassageJSON, 0, len(newest))
		for _, p := range newest {
			out = append(out, v.passageJSON(p))
		}
		res = map[string]any{
			"status": "ok", "found": found, "returned": len(out), "passages": out,
			"logbook_first_departure": rfc3339(v.first),
			"logbook_last_arrival":    rfc3339(v.last),
			"note": "From the Passages logbook, most recent first. Times are UTC. A null figure was not recorded; do not estimate it. " +
				"from and to are the place names on the record; a blank is a place that has no name.",
		}
	}
	v.addUnderWay(res)
	b, err := json.Marshal(res)
	if err != nil {
		return "", fmt.Errorf("list_passages: %w", err)
	}
	return string(b), nil
}

type cruiseStay struct {
	Place         string  `json:"place"`
	Arrived       string  `json:"arrived"`
	Departed      string  `json:"departed"`
	Days          float64 `json:"days"`
	NextDeparture string  `json:"next_departure_place,omitempty"`
	// HiddenBetween says a hidden passage lies inside this stay.
	HiddenBetween bool `json:"hidden_passage_between,omitempty"`
}

// logbookStays is the gaps of at least logbookMinStay between one passage's
// end and the next one's start, longest first. The place is where the earlier
// passage arrived. When the next one departed from a differently named place,
// both are given and neither is preferred. A hidden passage inside the gap was
// a move of the boat the operator chose to leave off the page, so the stay is
// flagged and not quietly merged or split.
func logbookStays(ps []logbookPassage, hidden [][2]time.Time) (all []cruiseStay) {
	for i := 0; i+1 < len(ps); i++ {
		a, b := ps[i], ps[i+1]
		gap := b.Start.Sub(a.End)
		if gap < logbookMinStay {
			continue
		}
		s := cruiseStay{
			Place: a.ToName, Arrived: rfc3339(a.End), Departed: rfc3339(b.Start),
			Days: round1(gap.Hours() / 24),
		}
		if a.ToName != "" && b.FromName != "" && a.ToName != b.FromName {
			s.NextDeparture = b.FromName
		}
		for _, h := range hidden {
			if h[0].Before(b.Start) && h[1].After(a.End) {
				s.HiddenBetween = true
				break
			}
		}
		all = append(all, s)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Days > all[j].Days })
	return all
}

// logbookRoute is the places in order: where the first passage left, then
// where each arrived. Blank names are skipped and repeats collapse.
func logbookRoute(ps []logbookPassage) []string {
	route := []string{}
	add := func(n string) {
		if n != "" && (len(route) == 0 || route[len(route)-1] != n) {
			route = append(route, n)
		}
	}
	for i, p := range ps {
		if i == 0 {
			add(p.FromName)
		}
		add(p.ToName)
	}
	return route
}

func (d assistantToolDeps) executeGetCruiseSummary(ctx context.Context, raw json.RawMessage) (string, error) {
	var args assistantPassagesArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parse get_cruise_summary arguments: %w", err)
		}
	}
	r, err := parseLogbookRange(args.From, args.To)
	if err != nil {
		return "", fmt.Errorf("get_cruise_summary: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	v, err := readLogbook(ctx, d, "get_cruise_summary", r)
	if err != nil {
		return "", err
	}
	var res map[string]any
	if len(v.passages) == 0 {
		res = v.emptyResult()
		res["passage_count"] = 0
	} else {
		res = v.summary()
	}
	v.addUnderWay(res)
	b, err := json.Marshal(res)
	if err != nil {
		return "", fmt.Errorf("get_cruise_summary: %w", err)
	}
	return string(b), nil
}

func (v logbookView) summary() map[string]any {
	ps := v.passages // oldest first
	var nm, underwaySec, fuel float64
	withFuel := 0
	unnamed := false
	cruiseCount := map[string]int{}
	var cruiseOrder []string
	lastIdx := 0 // the passage that finished last
	for i, p := range ps {
		nm += p.stats.DistanceNM
		underwaySec += p.stats.UnderwaySeconds
		if p.stats.FuelBurnedL != nil {
			fuel += *p.stats.FuelBurnedL
			withFuel++
		}
		if p.FromName == "" || p.ToName == "" {
			unnamed = true
		}
		if p.End.After(ps[lastIdx].End) {
			lastIdx = i
		}
		if n := v.cruiseName(p.passageRecord); n != "" {
			if cruiseCount[n] == 0 {
				cruiseOrder = append(cruiseOrder, n)
			}
			cruiseCount[n]++
		}
	}
	cruises := []map[string]any{}
	for _, n := range cruiseOrder {
		cruises = append(cruises, map[string]any{"name": n, "passages": cruiseCount[n]})
	}

	legs := append([]logbookPassage(nil), ps...)
	sort.SliceStable(legs, func(i, j int) bool { return legs[i].stats.DistanceNM > legs[j].stats.DistanceNM })
	if len(legs) > logbookTopLegs {
		legs = legs[:logbookTopLegs]
	}
	longest := make([]assistantPassageJSON, 0, len(legs))
	for _, p := range legs {
		longest = append(longest, v.passageJSON(p))
	}

	stays := logbookStays(ps, v.hidden)
	hiddenInStay := false
	for _, st := range stays {
		hiddenInStay = hiddenInStay || st.HiddenBetween
	}
	total := len(stays)
	var stayDays float64
	for _, s := range stays {
		stayDays += s.Days
	}
	if len(stays) > logbookTopStays {
		stays = stays[:logbookTopStays]
	}

	note := "From the Passages logbook. Times are UTC. Places are the names on the records, in order in route. " +
		"Stays are the gaps of six hours or more between one passage's end and the next one's start: the logbook does not record whether the boat was at anchor, on a mooring or at a dock. " +
		"underway_hours is time under way, not engine hours. Fuel is added up only over the passages that recorded it. Do not infer a direction of travel or regional geography beyond these records."
	if unnamed {
		note += " Some passages have no place name: the route leaves those places out, and a stay at one has a blank place."
	}
	if hiddenInStay {
		note += " A stay marked hidden_passage_between has a hidden passage inside it, so the boat moved during it."
	}
	var fuelTotal *float64 // null when no passage recorded fuel, never 0
	if withFuel > 0 {
		f := round1(fuel)
		fuelTotal = &f
	}
	return map[string]any{
		"status": "ok", "passage_count": len(ps),
		"first_place": ps[0].FromName, "first_departure": rfc3339(ps[0].Start),
		"last_place": ps[lastIdx].ToName, "last_arrival": rfc3339(ps[lastIdx].End),
		"total_distance_nm": round1(nm), "underway_hours": round1(underwaySec / 3600),
		"fuel": map[string]any{
			"burned_l": fuelTotal, "passages_with_fuel": withFuel, "passages_without_fuel": len(ps) - withFuel,
		},
		"route": logbookRoute(ps), "cruises": cruises,
		"longest_passages": longest,
		"stays_total":      total, "stay_days_total": round1(stayDays), "longest_stays": stays,
		"logbook_first_departure": rfc3339(v.first), "logbook_last_arrival": rfc3339(v.last),
		"note": note,
	}
}

const logbookRangeParams = `"from": {"type": "string", "description": "Earliest departure: YYYY-MM-DD (that day on the boat's clock) or RFC3339. Omit for the start of the logbook."},
						"to": {"type": "string", "description": "Latest departure: YYYY-MM-DD (that whole day on the boat's clock) or RFC3339. Omit for the end of the logbook."}`

var mateToolListPassages = mateTool{
	name: "list_passages",
	function: openRouterFunctionDef{
		Description: "List passages from the Passages logbook, the record the operator sees on the Passages page: " +
			"start and end (UTC, and on the boat's clock as start_local and end_local), the place it left and arrived at, cruise, rating, notes, distance in NM, time " +
			"under way, speeds, wind and fuel burned in litres (null where not recorded), most recent first. " +
			"The logbook goes back as far as it was kept, with no ceiling on how far back to ask. Use it for " +
			"\"the last passage\", \"the passages in August\" and \"when were we last underway\", then call " +
			"get_path_history over a passage's start and end for the readings during it. A passage still under " +
			"way is not in the logbook until it ends; it is reported apart as under_way_now. If the logbook has " +
			"no passages it says so; it does not fall back to the telemetry.",
		Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						` + logbookRangeParams + `,
						"limit": {"type": "integer", "description": "How many passages to return, most recent first. Default 20, at most 100."}
					}
				}`),
	},
	execute: assistantToolDeps.executeListPassages,
	describe: func(args json.RawMessage) string {
		return "Reading the Passages logbook…"
	},
}

var mateToolGetCruiseSummary = mateTool{
	name: "get_cruise_summary",
	function: openRouterFunctionDef{
		Description: "Summarise the Passages logbook over a date range, or all of it: passage count, total " +
			"distance in NM, time under way, fuel burned in litres over the passages that recorded it, the " +
			"first place left and last place reached, the route as the recorded place names in order, the " +
			"cruises, the longest passages, and the longest stays worked out from the gaps between passages " +
			"(place, arrival, departure, days). The figures are added up here; quote them, do not recompute. " +
			"Use it for \"summarise the season\", \"the trip\" or \"where have we been\", with list_passages " +
			"for any one passage's detail. Places are the names on the records and it infers no geography.",
		Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						` + logbookRangeParams + `
					}
				}`),
	},
	execute: assistantToolDeps.executeGetCruiseSummary,
	describe: func(args json.RawMessage) string {
		return "Adding up the Passages logbook…"
	},
}
