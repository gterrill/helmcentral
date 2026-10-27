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

type logbookRange struct {
	from, to time.Time // zero means unbounded; to is exclusive
}

func (r logbookRange) has(t time.Time) bool {
	return (r.from.IsZero() || !t.Before(r.from)) && (r.to.IsZero() || t.Before(r.to))
}

// parseLogbookBound reads a YYYY-MM-DD date (UTC) or an RFC3339 time. A bare
// date as an upper bound means the whole of that day.
func parseLogbookBound(name, v string, upper bool) (time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, nil
	}
	if d, err := time.Parse("2006-01-02", v); err == nil {
		if upper {
			d = d.AddDate(0, 0, 1)
		}
		return d, nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		if upper {
			t = t.Add(time.Second) // an RFC3339 upper bound is inclusive
		}
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("%s %q is not a date: use YYYY-MM-DD or RFC3339", name, v)
}

func parseLogbookRange(from, to string) (logbookRange, error) {
	var r logbookRange
	var err error
	if r.from, err = parseLogbookBound("from", from, false); err != nil {
		return r, err
	}
	if r.to, err = parseLogbookBound("to", to, true); err != nil {
		return r, err
	}
	if !r.from.IsZero() && !r.to.IsZero() && !r.from.Before(r.to) {
		return r, errors.New("from must be before to")
	}
	return r, nil
}

type logbookPassage struct {
	passageRecord
	stats passageStats
}

// logbookPassages is the visible passages whose departure falls in the range,
// oldest first, with the cruise names, plus the whole logbook's span.
type logbookView struct {
	passages    []logbookPassage
	cruiseNames map[string]string
	total       int
	first, last time.Time // whole logbook, not just the range
}

func readLogbook(d assistantToolDeps, tool string, r logbookRange) (logbookView, error) {
	var v logbookView
	if d.passageLog == nil {
		return v, fmt.Errorf("%s: the passage log is not running", tool)
	}
	store := d.passageLog()
	if store == nil {
		return v, fmt.Errorf("%s: the passage log is not running", tool)
	}
	all, err := store.ListPassages() // newest first
	if err != nil {
		return v, fmt.Errorf("%s: %w", tool, err)
	}
	v.total = len(all)
	cruises, err := store.ListCruises()
	if err != nil {
		return v, fmt.Errorf("%s: %w", tool, err)
	}
	v.cruiseNames = map[string]string{}
	for _, c := range cruises {
		v.cruiseNames[c.ID] = c.Name
	}
	for i := len(all) - 1; i >= 0; i-- {
		p := all[i]
		if v.first.IsZero() || p.Start.Before(v.first) {
			v.first = p.Start
		}
		if p.End.After(v.last) {
			v.last = p.End
		}
		if !r.has(p.Start) {
			continue
		}
		figs, err := store.PassageFiguresFor(p)
		if err != nil {
			return v, fmt.Errorf("%s: %w", tool, err)
		}
		v.passages = append(v.passages, logbookPassage{passageRecord: p, stats: figs.Stats})
	}
	sort.SliceStable(v.passages, func(i, j int) bool { return v.passages[i].Start.Before(v.passages[j].Start) })
	return v, nil
}

func (v logbookView) cruiseName(p passageRecord) string {
	if p.CruiseID == nil {
		return ""
	}
	return v.cruiseNames[*p.CruiseID]
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// logbookEmpty is the plain statement for a logbook with nothing to read. It
// is given as it is: the tools do not go looking in the telemetry instead.
func (v logbookView) emptyResult(r logbookRange) map[string]any {
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
}

func (v logbookView) passageJSON(p logbookPassage) assistantPassageJSON {
	return assistantPassageJSON{
		Start: rfc3339(p.Start), End: rfc3339(p.End),
		From: p.FromName, To: p.ToName, Title: p.Title,
		Cruise: v.cruiseName(p.passageRecord), Rating: p.Rating, Notes: p.Notes,
		DurationHours: round1(p.End.Sub(p.Start).Hours()),
		UnderwayHours: round1(p.stats.UnderwaySeconds / 3600),
		DistanceNm:    round1(p.stats.DistanceNM),
		MeanSOGKts:    roundPtr1(p.stats.MeanSOGKts), MaxSOGKts: roundPtr1(p.stats.MaxSOGKts),
		FuelBurnedL: roundPtr1(p.stats.FuelBurnedL),
		MeanTWSKts:  roundPtr1(p.stats.TWSMeanKts), MaxGustKts: roundPtr1(p.stats.MaxGustKts),
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
	v, err := readLogbook(d, "list_passages", r)
	if err != nil {
		return "", err
	}
	var res map[string]any
	if len(v.passages) == 0 {
		res = v.emptyResult(r)
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
}

// logbookStays is the gaps of at least logbookMinStay between one passage's
// end and the next one's start, longest first. The place is where the earlier
// passage arrived. When the next one departed from a differently named place
// both are given and neither is preferred.
func logbookStays(ps []logbookPassage) (all []cruiseStay) {
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
	v, err := readLogbook(d, "get_cruise_summary", r)
	if err != nil {
		return "", err
	}
	var res map[string]any
	if len(v.passages) == 0 {
		res = v.emptyResult(r)
		res["passage_count"] = 0
	} else {
		res = v.summary()
	}
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
	end := ps[0].End
	for _, p := range ps {
		nm += p.stats.DistanceNM
		underwaySec += p.stats.UnderwaySeconds
		if p.stats.FuelBurnedL != nil {
			fuel += *p.stats.FuelBurnedL
			withFuel++
		}
		if p.FromName == "" || p.ToName == "" {
			unnamed = true
		}
		if p.End.After(end) {
			end = p.End
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

	stays := logbookStays(ps)
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
		note += " Some passages have no place name, so route and stays leave those places out."
	}
	return map[string]any{
		"status": "ok", "passage_count": len(ps),
		"first_place": ps[0].FromName, "first_departure": rfc3339(ps[0].Start),
		"last_place": ps[len(ps)-1].ToName, "last_arrival": rfc3339(end),
		"total_distance_nm": round1(nm), "underway_hours": round1(underwaySec / 3600),
		"fuel": map[string]any{
			"burned_l": round1(fuel), "passages_with_fuel": withFuel, "passages_without_fuel": len(ps) - withFuel,
		},
		"route": logbookRoute(ps), "cruises": cruises,
		"longest_passages": longest,
		"stays_total":      total, "stay_days_total": round1(stayDays), "longest_stays": stays,
		"logbook_first_departure": rfc3339(v.first), "logbook_last_arrival": rfc3339(v.last),
		"note": note,
	}
}

const logbookRangeParams = `"from": {"type": "string", "description": "Earliest departure, YYYY-MM-DD (UTC) or RFC3339. Omit for the start of the logbook."},
						"to": {"type": "string", "description": "Latest departure, YYYY-MM-DD (UTC, that whole day) or RFC3339. Omit for the end of the logbook."}`

var mateToolListPassages = mateTool{
	name: "list_passages",
	function: openRouterFunctionDef{
		Description: "List passages from the Passages logbook, the record the operator sees on the Passages page: " +
			"start and end (UTC), the place it left and arrived at, cruise, rating, notes, distance in NM, time " +
			"under way, speeds, wind and fuel burned in litres (null where not recorded), most recent first. " +
			"The logbook goes back as far as it was kept, with no ceiling on how far back to ask. Use it for " +
			"\"the last passage\", \"the passages in August\" and \"when were we last underway\", then call " +
			"get_path_history over a passage's start and end for the readings during it. If the logbook has " +
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
