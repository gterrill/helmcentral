package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Passage plans as a registered record type (ADR 0289, on the registry of ADR
// 0158). Mate can read a plan and propose a new one or a replacement for an
// existing one; the operator taps Apply. A plan is never deleted by a changeset.
//
// An operation goes through the rule the Forecast page's writes use
// (normalisePlan, checkPlanZones, createPlanTx and updatePlanTx), so the dates
// that follow from the first arrival and the nights are worked out in one place.
// An update replaces the plan whole: the stops given are the stops the plan has,
// so a stop left out is removed. Its card says only what differs.
//
// Where a stop's position came from (a saved place, or the place-names search)
// is not part of the plan. It rides on the proposal for the card to show and is
// not stored.

const recordTypePlan = "plan"

// The places a stop's position may come from, as the card names them.
const (
	planSourceSavedPlace   = "saved place"
	planSourcePlaceNames   = "place names"
	planSourceYourPosition = "your position"
)

func init() {
	defaultRecordRegistry.register(planRecordType())
}

func planRecordType() *recordType {
	return &recordType{
		Name:  recordTypePlan,
		Label: "plan",
		Summary: "A passage plan on the Forecast page: an ordered list of stops, each with the nights the boat stays, so the dates follow from the first arrival. " +
			"You can create a plan or replace one whole; deleting a plan is the operator's own tap. Give every stop a lat and lon from find_places, never a guess.",
		Actions: []string{changeCreate, changeUpdate},
		Fields: []recordField{
			{Name: "name", Kind: kindString, Writable: true, Description: "the plan's name; \"To <last stop>\" when the operator gave none"},
			{Name: "stops", Kind: kindObjectList, Writable: true, Description: "the whole list of stops in order, each {name, lat, lon, nights, vias, depart_time, arrive_by, source}. " +
				"Only the first stop has an arrive (YYYY-MM-DD): every later arrival is worked out from the stop before it and its nights, so give none. " +
				"lat and lon are required and come from find_places. nights is the whole nights at the stop, 0 for a stop passed through the same day. " +
				"vias is the places the leg leaving the stop passes, each {name, lat, lon}, left out on the last stop. depart_time is \"HH:MM\" at the stop, " +
				"arrive_by is \"YYYY-MM-DDTHH:MM\" at the stop and never on the first. source is \"saved place\" or \"place names\" where find_places found the position, or \"your position\" for one the operator gave. " +
				"On an update send every stop the plan keeps with all its fields: a field left out is cleared."},
			{Name: "first_date", Kind: kindDate, Description: "the day the plan starts"},
			{Name: "last_date", Kind: kindDate, Description: "the day the plan ends"},
		},
		Get:       getPlanRecord,
		List:      listPlanRecords,
		Create:    createPlanRecord,
		Update:    updatePlanRecord,
		Describe:  describePlan,
		Display:   showPlan,
		Href:      func(r recordSnapshot) string { return "/forecast?plan=" + r.ID },
		HrefLabel: "Open plan",
	}
}

// planVersion is the plan as last saved, for base_version on a proposal; ""
// for a plan that has not been saved. The save time is whole seconds, so the
// count of writes rides with it: an edit in the same second as a read still
// changes the version.
func planVersion(p plan) string {
	if p.UpdatedAt == nil {
		return ""
	}
	return fmt.Sprintf("%s#%d", unixVersion(*p.UpdatedAt), p.Revision)
}

// planStopFields is a stop as the snapshot shows it and as Mate sends it.
func planStopFields(s planStop) map[string]any {
	vias := make([]map[string]any, len(s.Vias))
	for i, v := range s.Vias {
		vias[i] = map[string]any{"name": v.Name, "lat": v.Lat, "lon": v.Lon}
	}
	return map[string]any{
		"name": s.Name, "lat": s.Lat, "lon": s.Lon, "arrive": s.Arrive, "nights": s.Nights, "depart": s.Depart,
		"vias": vias, "depart_time": s.DepartTime, "arrive_by": s.ArriveBy,
	}
}

func planSnapshot(p plan) recordSnapshot {
	stops := make([]map[string]any, len(p.Stops))
	for i, s := range p.Stops {
		stops[i] = planStopFields(s)
	}
	first, last := planDates(p)
	return recordSnapshot{
		ID:      strconv.FormatInt(p.ID, 10),
		Label:   p.Name,
		Version: planVersion(p),
		Fields:  map[string]any{"name": p.Name, "stops": stops, "first_date": first, "last_date": last},
	}
}

func getPlanRecord(q sqlQueryer, id string) (recordSnapshot, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(id), 10, 64)
	if err != nil || n <= 0 {
		return recordSnapshot{}, errRecordNotFound
	}
	p, ok, err := loadPlan(q, n)
	if err != nil {
		return recordSnapshot{}, err
	}
	if !ok {
		return recordSnapshot{}, errRecordNotFound
	}
	return planSnapshot(p), nil
}

func listPlanRecords(s *documentStore, _ map[string]string) ([]recordSnapshot, error) {
	plans, err := s.ListPlans()
	if err != nil {
		return nil, err
	}
	out := make([]recordSnapshot, len(plans))
	for i, p := range plans {
		out[i] = planSnapshot(p)
	}
	return out, nil
}

// planStopInput is a stop as Mate gives it. Lat and lon are pointers so that a
// stop with no position is refused rather than placed at 0, 0. Depart is what a
// read shows and is ignored on a write.
type planStopInput struct {
	Name       string         `json:"name"`
	Lat        *float64       `json:"lat"`
	Lon        *float64       `json:"lon"`
	Arrive     string         `json:"arrive"`
	Nights     int            `json:"nights"`
	Depart     string         `json:"depart"`
	Vias       []planViaInput `json:"vias"`
	DepartTime string         `json:"depart_time"`
	ArriveBy   string         `json:"arrive_by"`
	Source     string         `json:"source"`

	// What get_plans shows beside a stop and nothing a write reads: the finished
	// labels and the stop's time zone. Mate is told to send a kept stop whole, so
	// they are accepted and dropped.
	ArriveLabel     string `json:"arrive_label"`
	DepartLabel     string `json:"depart_label"`
	DepartTimeLabel string `json:"depart_time_label"`
	ArriveByLabel   string `json:"arrive_by_label"`
	Timezone        string `json:"timezone"`
}

type planViaInput struct {
	Name string   `json:"name"`
	Lat  *float64 `json:"lat"`
	Lon  *float64 `json:"lon"`
}

// parsePlanStops reads the stops of a create or an update, strictly: a field
// that is not a stop's is a misspelling Mate believes it sent, so it fails and
// names the field. A stop's source is checked here and is not kept: the card
// reads it from the operation's fields (planSources).
func parsePlanStops(raw any) ([]planStop, error) {
	items, ok := raw.([]map[string]any)
	if !ok {
		return nil, fieldErr("stops", "must be a list of stops")
	}
	stops := make([]planStop, len(items))
	for i, item := range items {
		n := i + 1
		body, err := json.Marshal(item)
		if err != nil {
			return nil, fieldErr("stops", "stop %d: %v", n, err)
		}
		var in planStopInput
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			return nil, fieldErr("stops", "stop %d: %v", n, err)
		}
		if in.Lat == nil || in.Lon == nil {
			return nil, fieldErr("lat", "stop %d (%s): give its lat and lon from find_places; a position is never guessed", n, in.Name)
		}
		switch in.Source {
		case "", planSourceSavedPlace, planSourcePlaceNames, planSourceYourPosition:
		default:
			return nil, fieldErr("source", "stop %d: must be %q, %q or %q, got %q", n, planSourceSavedPlace, planSourcePlaceNames, planSourceYourPosition, in.Source)
		}
		stop := planStop{Name: in.Name, Lat: *in.Lat, Lon: *in.Lon, Arrive: in.Arrive, Nights: in.Nights, DepartTime: in.DepartTime, ArriveBy: in.ArriveBy}
		for j, v := range in.Vias {
			if v.Lat == nil || v.Lon == nil {
				return nil, fieldErr("vias", "stop %d, via %d (%s): give its lat and lon from find_places; a via place is left out when it matches no place", n, j+1, v.Name)
			}
			stop.Vias = append(stop.Vias, planVia{Name: v.Name, Lat: *v.Lat, Lon: *v.Lon})
		}
		stops[i] = stop
	}
	return stops, nil
}

// planSources is the source of each stop's position as the operation gave it,
// "" for a stop that gave none, in the order of the stops. It is empty when the
// operation sent no stops.
func planSources(fields map[string]any) []string {
	var sources []string
	if items, ok := fields["stops"].([]map[string]any); ok {
		for _, item := range items {
			sources = append(sources, stringOf(item["source"]))
		}
	}
	return sources
}

// checkedPlan runs a plan through the Forecast page's rule: normalised, with a
// time zone at every stop.
func checkedPlan(p plan) (plan, error) {
	norm, verr := normalisePlan(p)
	if verr == nil {
		verr = checkPlanZones(norm)
	}
	if verr != nil {
		return plan{}, fieldErr(verr.Field, "%s", verr.Message)
	}
	return norm, nil
}

func createPlanRecord(env changeEnv, fields fieldSet) (createdRecord, error) {
	raw, ok := fields["stops"]
	if !ok {
		return createdRecord{}, fieldErr("stops", "is required: the stops in order")
	}
	stops, err := parsePlanStops(raw)
	if err != nil {
		return createdRecord{}, err
	}
	name := strings.TrimSpace(stringOf(fields["name"]))
	if name == "" && len(stops) > 0 {
		// The agreed default; a stop with no name is refused by the plan rule.
		if last := strings.TrimSpace(stops[len(stops)-1].Name); last != "" {
			name = "To " + last
		}
	}
	norm, err := checkedPlan(plan{Name: name, Stops: stops})
	if err != nil {
		return createdRecord{}, err
	}
	saved, err := createPlanTx(env.tx, env.now, norm)
	if err != nil {
		return createdRecord{}, err
	}
	return createdRecord{ID: strconv.FormatInt(saved.ID, 10)}, nil
}

func updatePlanRecord(env changeEnv, before recordSnapshot, given fieldSet) error {
	id, err := strconv.ParseInt(before.ID, 10, 64)
	if err != nil {
		return err
	}
	cur, ok, err := loadPlan(env.tx, id)
	if err != nil {
		return err
	}
	if !ok {
		return errRecordNotFound
	}
	next := cur
	next.Stops = copyStops(cur.Stops)
	if v, ok := given["name"]; ok {
		next.Name = stringOf(v)
	}
	if v, ok := given["stops"]; ok {
		if next.Stops, err = parsePlanStops(v); err != nil {
			return err
		}
	}
	norm, err := checkedPlan(next)
	if err != nil {
		return err
	}
	if sameFieldValue(planSnapshot(cur).Fields, planSnapshot(norm).Fields) {
		return fieldErr("stops", "the change leaves %q exactly as it is; propose only what differs", cur.Name)
	}
	if _, found, err := updatePlanTx(env.tx, env.now, id, norm); err != nil {
		return err
	} else if !found {
		return errRecordNotFound
	}
	return nil
}

// ── the card ────────────────────────────────────────────────────────────

// planStopsOfFields reads a snapshot's stops (the list planStopFields wrote)
// back into the store's shape. A snapshot that does not hold that list is an
// error.
func planStopsOfFields(v any) ([]planStop, error) {
	items, ok := v.([]map[string]any)
	if !ok {
		return nil, fmt.Errorf("the plan's stops are %T, not a list of stops", v)
	}
	stops := make([]planStop, len(items))
	for i, item := range items {
		stop, err := planStopOfFields(item)
		if err != nil {
			return nil, fmt.Errorf("stop %d: %w", i+1, err)
		}
		stops[i] = stop
	}
	return stops, nil
}

// planFieldReader reads typed values out of one stop's snapshot fields and
// keeps the first field that is missing or of the wrong type.
type planFieldReader struct {
	fields map[string]any
	err    error
}

func readPlanField[T any](r *planFieldReader, name string) T {
	v, ok := r.fields[name].(T)
	if !ok && r.err == nil {
		r.err = fmt.Errorf("%s is %T, not the type a stop holds", name, r.fields[name])
	}
	return v
}

func planStopOfFields(fields map[string]any) (planStop, error) {
	r := &planFieldReader{fields: fields}
	stop := planStop{
		Name: readPlanField[string](r, "name"), Lat: readPlanField[float64](r, "lat"), Lon: readPlanField[float64](r, "lon"),
		Arrive: readPlanField[string](r, "arrive"), Nights: readPlanField[int](r, "nights"), Depart: readPlanField[string](r, "depart"),
		DepartTime: readPlanField[string](r, "depart_time"), ArriveBy: readPlanField[string](r, "arrive_by"),
	}
	for _, via := range readPlanField[[]map[string]any](r, "vias") {
		v := &planFieldReader{fields: via}
		stop.Vias = append(stop.Vias, planVia{Name: readPlanField[string](v, "name"), Lat: readPlanField[float64](v, "lat"), Lon: readPlanField[float64](v, "lon")})
		if v.err != nil && r.err == nil {
			r.err = fmt.Errorf("via: %w", v.err)
		}
	}
	return stop, r.err
}

// planDayText is a day as the card says it, "Wed 18 Nov"; a day that does not
// parse is said as it is.
func planDayText(day string) string {
	if label, err := planDayLabel(day); err == nil {
		return label
	}
	return day
}

// planTimeText is an "HH:MM" departure time as the operator reads a time,
// "6.30 AM".
func planTimeText(hhmm string) string {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return hhmm
	}
	return boatClock(t, time.UTC)
}

// planArriveByText is an "YYYY-MM-DDTHH:MM" arrive-by as "Mon 16 Nov 4.00 PM".
func planArriveByText(v string) string {
	t, err := time.Parse("2006-01-02T15:04", v)
	if err != nil {
		return v
	}
	return boatDayClock(t, time.UTC)
}

// describePlan writes the operator's line. showPlan has already read both
// snapshots' stops by the time this runs, so a failure here is reported in the
// line rather than hidden.
func describePlan(d describeInput) string {
	if d.Op.Action == changeCreate {
		stops, err := planStopsOfFields(d.After.Fields["stops"])
		if err != nil {
			return fmt.Sprintf("Add plan %q: its stops could not be read: %v", d.After.Label, err)
		}
		first, last := stringOf(d.After.Fields["first_date"]), stringOf(d.After.Fields["last_date"])
		noun := "stops"
		if len(stops) == 1 {
			noun = "stop"
		}
		return fmt.Sprintf("Add plan %q: %d %s, %s to %s", d.After.Label, len(stops), noun, planDayText(first), planDayText(last))
	}
	sentences, err := planDiff(d.Before, d.After)
	if err != nil {
		return fmt.Sprintf("Change plan %q: its stops could not be read: %v", d.Before.Label, err)
	}
	return fmt.Sprintf("Change plan %q: %s", d.Before.Label, strings.Join(sentences, " "))
}

// planDiff says what an update changes, in the order a reader looks: the name,
// the stops that came or went, what changed at each stop that stayed, and last
// how the dates of the stops after the change moved. It says nothing of a stop
// that is as it was. The sentences are worked out from the two snapshots alone,
// so the same change always reads the same.
func planDiff(before, after *recordSnapshot) ([]string, error) {
	bs, err := planStopsOfFields(before.Fields["stops"])
	if err != nil {
		return nil, err
	}
	as, err := planStopsOfFields(after.Fields["stops"])
	if err != nil {
		return nil, err
	}
	var out []string
	if b, a := stringOf(before.Fields["name"]), stringOf(after.Fields["name"]); b != a {
		out = append(out, fmt.Sprintf("Renamed to %q.", a))
	}
	matchOf, taken := matchPlanStops(bs, as)
	out = append(out, planStopsCameAndWent(bs, as, matchOf, taken)...)
	if planStopsReordered(matchOf) {
		out = append(out, "The stops are in a new order.")
	}
	var shifts []planArrivalShift
	for i, a := range as {
		if matchOf[i] < 0 {
			continue
		}
		b := bs[matchOf[i]]
		out = append(out, planStopChanges(i == 0, b, a)...)
		if i == 0 {
			continue
		}
		bd, berr := time.Parse(planDateLayout, b.Arrive)
		ad, aerr := time.Parse(planDateLayout, a.Arrive)
		if berr == nil && aerr == nil && !ad.Equal(bd) {
			shifts = append(shifts, planArrivalShift{a.Name, a.Arrive, planDaysBetween(bd, ad)})
		}
	}
	if len(shifts) > 0 {
		out = append(out, planShiftSentence(shifts))
	}
	return out, nil
}

// matchPlanStops pairs the stops by name, the first unmatched of that name each
// time, so a place visited twice keeps its two visits apart. matchOf[i] is the
// before-stop the after-stop i is, or -1; taken marks the before-stops matched.
func matchPlanStops(bs, as []planStop) (matchOf []int, taken []bool) {
	matchOf = make([]int, len(as))
	taken = make([]bool, len(bs))
	for i, a := range as {
		matchOf[i] = -1
		for j, b := range bs {
			if !taken[j] && b.Name == a.Name {
				matchOf[i], taken[j] = j, true
				break
			}
		}
	}
	return matchOf, taken
}

// planStopsCameAndWent says the stops that were removed, then the ones added and
// where.
func planStopsCameAndWent(bs, as []planStop, matchOf []int, taken []bool) []string {
	var out []string
	for j, b := range bs {
		if !taken[j] {
			out = append(out, fmt.Sprintf("%s removed.", b.Name))
		}
	}
	for i, a := range as {
		if matchOf[i] >= 0 {
			continue
		}
		where := "as the first stop"
		if i > 0 {
			where = "after " + as[i-1].Name
		}
		out = append(out, fmt.Sprintf("%s added %s (arrives %s, %s).", a.Name, where, planDayText(a.Arrive), plural(a.Nights, "night", "nights")))
	}
	return out
}

// planStopsReordered says whether the stops that stayed are in a different
// order.
func planStopsReordered(matchOf []int) bool {
	last := -1
	for _, m := range matchOf {
		if m < 0 {
			continue
		}
		if m < last {
			return true
		}
		last = m
	}
	return false
}

// planArrivalShift is a stop after the first whose arrival day moved.
type planArrivalShift struct {
	name, arrive string
	days         int
}

// planShiftSentence says the stops whose arrival moved: one sentence with the
// shift when it is the same for every stop (uniform), else one that only lists
// the new dates.
func planShiftSentence(shifts []planArrivalShift) string {
	parts := make([]string, len(shifts))
	uniform := true
	for i, s := range shifts {
		parts[i] = fmt.Sprintf("%s arrives %s", s.name, planDayText(s.arrive))
		uniform = uniform && s.days == shifts[0].days
	}
	if !uniform {
		return fmt.Sprintf("Later stops change date: %s.", strings.Join(parts, ", "))
	}
	days := shifts[0].days
	how := "a day"
	if days > 1 || days < -1 {
		how = fmt.Sprintf("%d days", max(days, -days))
	}
	if days < 0 {
		how += " earlier"
	}
	return fmt.Sprintf("Later stops move %s: %s.", how, strings.Join(parts, ", "))
}

// planStopChanges is what differs at one stop that is in both plans.
func planStopChanges(first bool, b, a planStop) []string {
	var out []string
	say := func(format string, args ...any) {
		out = append(out, a.Name+": "+fmt.Sprintf(format, args...)+".")
	}
	if b.Nights != a.Nights {
		say("%s → %s", plural(b.Nights, "night", "nights"), plural(a.Nights, "night", "nights"))
	}
	if first && b.Arrive != a.Arrive {
		say("arrives %s → %s", planDayText(b.Arrive), planDayText(a.Arrive))
	}
	if b.Lat != a.Lat || b.Lon != a.Lon {
		say("position changed")
	}
	out = append(out, planTextChange(a.Name, "departure time", b.DepartTime, a.DepartTime, planTimeText)...)
	out = append(out, planTextChange(a.Name, "arrive-by", b.ArriveBy, a.ArriveBy, planArriveByText)...)
	out = append(out, planViaChanges(a.Name, b.Vias, a.Vias)...)
	return out
}

// planTextChange says a setting that was set, changed or cleared.
func planTextChange(stop, what, before, after string, text func(string) string) []string {
	switch {
	case before == after:
		return nil
	case before == "":
		return []string{fmt.Sprintf("%s: %s set to %s.", stop, what, text(after))}
	case after == "":
		return []string{fmt.Sprintf("%s: %s removed.", stop, what)}
	}
	return []string{fmt.Sprintf("%s: %s %s → %s.", stop, what, text(before), text(after))}
}

func planViaChanges(stop string, before, after []planVia) []string {
	var out []string
	has := func(list []planVia, v planVia) (planVia, bool) {
		for _, w := range list {
			if w.Name == v.Name {
				return w, true
			}
		}
		return planVia{}, false
	}
	for _, v := range after {
		w, ok := has(before, v)
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("%s: via %s added.", stop, v.Name))
		case w.Lat != v.Lat || w.Lon != v.Lon:
			out = append(out, fmt.Sprintf("%s: via %s moved.", stop, v.Name))
		}
	}
	for _, v := range before {
		if _, ok := has(after, v); !ok {
			out = append(out, fmt.Sprintf("%s: via %s removed.", stop, v.Name))
		}
	}
	if len(out) == 0 && len(before) == len(after) {
		for i := range before {
			if before[i].Name != after[i].Name {
				out = append(out, fmt.Sprintf("%s: vias are in a new order.", stop))
				break
			}
		}
	}
	return out
}

// showPlan is the card's rows. A new plan shows one row per stop: the day, the
// nights, the place, where its position came from and the position. An update
// shows the sentence in its description alone.
// It reads the stops of both snapshots first, so that describePlan, which runs
// after it, finds them readable.
func showPlan(op changeOp, before, after *recordSnapshot) (map[string]any, map[string]any, error) {
	var stops []planStop
	for _, snap := range []*recordSnapshot{before, after} {
		if snap == nil {
			continue
		}
		var err error
		if stops, err = planStopsOfFields(snap.Fields["stops"]); err != nil {
			return nil, nil, err
		}
	}
	if op.Action != changeCreate || after == nil {
		return nil, nil, nil
	}
	sources := planSources(op.Fields)
	rows := make([]map[string]any, len(stops))
	for i, s := range stops {
		row := map[string]any{
			"arrive": s.Arrive, "arrive_label": planDayText(s.Arrive), "nights": s.Nights,
			"name": s.Name, "lat": s.Lat, "lon": s.Lon,
		}
		if i < len(sources) && sources[i] != "" {
			row["source"] = sources[i]
		}
		rows[i] = row
	}
	return nil, map[string]any{"stops": rows}, nil
}
