package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/labstack/echo/v4"
)

// A passage plan is the operator's intended run: an ordered list of stops,
// each with the day the boat arrives and the nights it stays. Legs are
// derived between consecutive stops. A leg may name via places, because the
// water router is point to point, and an optional departure time that only
// the operator sets. Plans replace the single planned stay (ADR 0285).
//
// Dates are calendar days at the place (YYYY-MM-DD). Only the first stop's
// arrival is authoritative: every later arrival is the previous stop's
// arrival plus its nights, recomputed on every write. That is the one rule
// that shifts later dates when a date or a night count changes.
//
// No omitempty on the fields the Forecast page binds: its TypeScript
// interface declares every one.

const (
	planNameMax        = 120
	planMaxStops       = 30
	planMaxVias        = 10
	planMaxNights      = 365
	planDateLayout     = "2006-01-02"
	planStatusPast     = "past"
	planStatusUpcoming = "upcoming"
	planStatusActive   = "active"
)

// planNow is the clock plans are judged by; tests replace it.
var planNow = time.Now

// stayZoneAt finds the zone at a stop's position, from the cache. A variable
// so a test can make the lookup fail.
var stayZoneAt = zoneAtCached

var planTimePattern = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

type planVia struct {
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

type planStop struct {
	Name   string  `json:"name"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	Arrive string  `json:"arrive"`
	Nights int     `json:"nights"`
	// Vias are the places the leg leaving this stop passes, in order. Empty on
	// the last stop.
	Vias []planVia `json:"vias"`
	// DepartTime is the operator's departure time for the leg leaving this
	// stop, "HH:MM" at this stop, or "" when none is set. Empty on the last stop.
	DepartTime string `json:"depart_time"`
	// Depart and Timezone are worked out on every read and ignored on a write.
	Depart   string `json:"depart"`
	Timezone string `json:"timezone"`
}

type plan struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Stops     []planStop `json:"stops"`
	Status    string     `json:"status"`
	FirstDate string     `json:"first_date"`
	LastDate  string     `json:"last_date"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
}

// planError names the one field a write failed on, in the {field, message}
// shape the other forms use.
type planError struct {
	Field   string
	Message string
}

func (e *planError) Error() string { return e.Field + ": " + e.Message }

func createPlansSchema(db *sql.DB) error {
	stmts := []string{
		// The single planned stay is dropped, not migrated: one operator,
		// re-entered as a plan.
		`DROP TABLE IF EXISTS planned_stay`,
		`CREATE TABLE IF NOT EXISTS plans (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			name       TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS plan_stops (
			plan_id     INTEGER NOT NULL,
			position    INTEGER NOT NULL,
			name        TEXT NOT NULL,
			lat         REAL NOT NULL,
			lon         REAL NOT NULL,
			arrive      TEXT NOT NULL,
			nights      INTEGER NOT NULL,
			vias        TEXT NOT NULL DEFAULT '[]',
			depart_time TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (plan_id, position)
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("create plans schema: %w", err)
		}
	}
	return nil
}

// ── dates ───────────────────────────────────────────────────────────────

func parsePlanDate(field, v string) (time.Time, *planError) {
	if !installDatePattern.MatchString(v) {
		return time.Time{}, &planError{Field: field, Message: field + " must be a date, YYYY-MM-DD"}
	}
	d, err := time.Parse(planDateLayout, v)
	if err != nil {
		return time.Time{}, &planError{Field: field, Message: field + " is not a real date"}
	}
	return d, nil
}

// planDaysBetween is the whole days from one calendar day to another. Both
// are parsed dates, midnight UTC, so the difference is exact.
func planDaysBetween(from, to time.Time) int {
	return int(to.Sub(from) / (24 * time.Hour))
}

// rechainStops makes every arrival after the first the previous arrival plus
// its nights, and sets each stop's departure. It fails when the first arrival
// is not a date.
func rechainStops(stops []planStop) error {
	if len(stops) == 0 {
		return nil
	}
	day, err := time.Parse(planDateLayout, stops[0].Arrive)
	if err != nil {
		return fmt.Errorf("the first arrival %q is not a date: %w", stops[0].Arrive, err)
	}
	for i := range stops {
		stops[i].Arrive = day.Format(planDateLayout)
		day = day.AddDate(0, 0, stops[i].Nights)
		stops[i].Depart = day.Format(planDateLayout)
	}
	return nil
}

func copyStops(in []planStop) []planStop {
	out := make([]planStop, len(in))
	for i, s := range in {
		out[i] = s
		out[i].Vias = append([]planVia{}, s.Vias...)
	}
	return out
}

// ── validation ──────────────────────────────────────────────────────────

// perr is a planError with a formatted message.
func perr(field, format string, args ...any) *planError {
	return &planError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// checkLatLon names the coordinate that is out of range, "lat" or "lon", and
// says why. axis is "" when both are fine.
func checkLatLon(lat, lon float64) (axis, why string) {
	if math.IsNaN(lat) || lat < -90 || lat > 90 {
		return "lat", "lat must be within -90..90"
	}
	if math.IsNaN(lon) || lon < -180 || lon > 180 {
		return "lon", "lon must be within -180..180"
	}
	return "", ""
}

// checkNights refuses a night count outside 0..planMaxNights. prefix leads the
// message ("stop 2: ") or is empty.
func checkNights(field, prefix string, nights int) *planError {
	if nights < 0 || nights > planMaxNights {
		return perr(field, "%snights must be 0..%d", prefix, planMaxNights)
	}
	return nil
}

func normaliseVia(v *planVia, stopNo, viaNo int) *planError {
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" {
		return perr("vias", "stop %d: name via place %d", stopNo, viaNo)
	}
	if utf8.RuneCountInString(v.Name) > planNameMax {
		return perr("vias", "stop %d: via place %d name is longer than %d characters", stopNo, viaNo, planNameMax)
	}
	if axis, why := checkLatLon(v.Lat, v.Lon); axis != "" {
		return perr("vias","stop %d via %d: %s", stopNo, viaNo, why)
	}
	return nil
}

// normaliseStop trims and checks stop i of a plan whose last stop is last.
func normaliseStop(s *planStop, i, last int) *planError {
	n := i + 1
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		return perr("stops.name", "name stop %d", n)
	}
	if utf8.RuneCountInString(s.Name) > planNameMax {
		return perr("stops.name", "stop %d: name is longer than %d characters", n, planNameMax)
	}
	if axis, why := checkLatLon(s.Lat, s.Lon); axis != "" {
		return perr("stops."+axis, "stop %d: %s", n, why)
	}
	if e := checkNights("stops.nights", fmt.Sprintf("stop %d: ", n), s.Nights); e != nil {
		return e
	}
	if i == 0 {
		if _, e := parsePlanDate("stops.arrive", s.Arrive); e != nil {
			return e
		}
	}
	if len(s.Vias) > planMaxVias {
		return perr("vias", "stop %d: at most %d via places", n, planMaxVias)
	}
	for j := range s.Vias {
		if e := normaliseVia(&s.Vias[j], n, j+1); e != nil {
			return e
		}
	}
	if s.DepartTime != "" && !planTimePattern.MatchString(s.DepartTime) {
		return perr("depart_time", "stop %d: departure time must be HH:MM", n)
	}
	if i == last {
		if len(s.Vias) > 0 {
			return perr("vias", "the last stop has no leg leaving it, so no via places")
		}
		if s.DepartTime != "" {
			return perr("depart_time", "the last stop has no leg leaving it, so no departure time")
		}
	}
	if s.Vias == nil {
		s.Vias = []planVia{}
	}
	return nil
}

// normalisePlan is the one rule the Forecast page's writes and a later Mate
// changeset share: names are trimmed, every field is checked, and each
// arrival after the first is recomputed from the stop before it.
func normalisePlan(p plan) (plan, *planError) {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return plan{}, perr("name", "name the plan")
	}
	if utf8.RuneCountInString(p.Name) > planNameMax {
		return plan{}, perr("name", "name is longer than %d characters", planNameMax)
	}
	if len(p.Stops) == 0 {
		return plan{}, perr("stops", "add at least one stop")
	}
	if len(p.Stops) > planMaxStops {
		return plan{}, perr("stops", "a plan has at most %d stops", planMaxStops)
	}
	p.Stops = copyStops(p.Stops)
	last := len(p.Stops) - 1
	for i := range p.Stops {
		if e := normaliseStop(&p.Stops[i], i, last); e != nil {
			return plan{}, e
		}
	}
	if err := rechainStops(p.Stops); err != nil {
		return plan{}, perr("stops.arrive", "%v", err)
	}
	return p, nil
}

// applyStopEdit changes one stop's arrival date or nights and shifts every
// later date with it. Moving the first arrival moves the whole plan; moving a
// later arrival is the nights of the stop before it. The input is not
// modified.
func applyStopEdit(stops []planStop, idx int, arrive *string, nights *int) ([]planStop, *planError) {
	if idx < 0 || idx >= len(stops) {
		return nil, perr("index", "there is no such stop")
	}
	out := copyStops(stops)
	if nights != nil {
		if e := checkNights("nights", "", *nights); e != nil {
			return nil, e
		}
	}
	if arrive != nil {
		want, e := parsePlanDate("arrive", *arrive)
		if e != nil {
			return nil, e
		}
		if idx == 0 {
			out[0].Arrive = *arrive
		} else {
			before, err := time.Parse(planDateLayout, out[idx-1].Arrive)
			if err != nil {
				return nil, perr("arrive", "the stop before has no date: %v", err)
			}
			n := planDaysBetween(before, want)
			if n < 0 {
				return nil, perr("arrive", "that is before the boat reaches the stop before")
			}
			if n > planMaxNights {
				return nil, perr("arrive", "that is more than %d nights after the stop before", planMaxNights)
			}
			out[idx-1].Nights = n
		}
	}
	if nights != nil {
		out[idx].Nights = *nights
	}
	if err := rechainStops(out); err != nil {
		return nil, perr("arrive", "%v", err)
	}
	return out, nil
}

// ── time: status and the current plan ───────────────────────────────────

// dayAt is the calendar day at a place.
func dayAt(zone placeZone, now time.Time) string {
	return now.In(zone.Loc).Format(planDateLayout)
}

// planDates are the first arrival and the last departure. The stops' Depart
// is set by rechainStops, which every read and write of a plan runs.
func planDates(p plan) (first, last string) {
	if len(p.Stops) == 0 {
		return "", ""
	}
	return p.Stops[0].Arrive, p.Stops[len(p.Stops)-1].Depart
}

// planStatusOn is "upcoming", "active" (the dates span today) or "past". The
// plan's first date is a day at its first stop and its last date a day at its
// last stop, so each is compared with today in that stop's own zone.
func planStatusOn(p plan, todayAtFirst, todayAtLast string) string {
	first, last := planDates(p)
	switch {
	case todayAtFirst < first:
		return planStatusUpcoming
	case todayAtLast > last:
		return planStatusPast
	}
	return planStatusActive
}

// selectCurrentPlan picks the plan the table opens on: the one whose dates
// span today, else the next upcoming, else, when every plan is past, the one
// that ended most recently. status says which of the three it is; idx is -1
// and status "" when there are no plans. The plans come from decoratePlan, so
// each already carries its status.
func selectCurrentPlan(plans []plan) (idx int, status string) {
	best := map[string]int{}
	for i, p := range plans {
		st := p.Status
		cur, seen := best[st]
		if !seen || planComesFirst(st, p, plans[cur]) {
			best[st] = i
		}
	}
	for _, st := range []string{planStatusActive, planStatusUpcoming, planStatusPast} {
		if i, ok := best[st]; ok {
			return i, st
		}
	}
	return -1, ""
}

// planComesFirst says whether a beats b among plans of one status: the
// earlier start for a plan under way or coming, the later end for one past.
func planComesFirst(status string, a, b plan) bool {
	aFirst, aLast := planDates(a)
	bFirst, bLast := planDates(b)
	if status == planStatusPast {
		return aLast > bLast
	}
	return aFirst < bFirst
}

// planZones finds every stop's zone, once. On a failure, stop is the index of
// the stop that has none.
func planZones(p plan) (zones []placeZone, stop int, err error) {
	zones = make([]placeZone, len(p.Stops))
	for i, s := range p.Stops {
		z, err := stayZoneAt(s.Lat, s.Lon)
		if err != nil {
			return nil, i, err
		}
		zones[i] = z
	}
	return zones, -1, nil
}

// decoratePlan adds what is worked out on every read: status, first and last
// dates, each stop's departure date and its zone. A position with no zone is
// an error, not UTC.
func decoratePlan(p plan, now time.Time) (plan, error) {
	if len(p.Stops) == 0 {
		return p, errors.New("plan has no stops")
	}
	p.Stops = copyStops(p.Stops)
	if err := rechainStops(p.Stops); err != nil {
		return p, err
	}
	zones, bad, err := planZones(p)
	if err != nil {
		return p, fmt.Errorf("stop %d (%s): %w", bad+1, p.Stops[bad].Name, err)
	}
	for i, z := range zones {
		p.Stops[i].Timezone = z.Name
	}
	p.Status = planStatusOn(p, dayAt(zones[0], now), dayAt(zones[len(zones)-1], now))
	p.FirstDate, p.LastDate = planDates(p)
	return p, nil
}

// checkPlanZones refuses a stop whose position has no time zone before
// anything is saved.
func checkPlanZones(p plan) *planError {
	if _, bad, err := planZones(p); err != nil {
		return perr("stops.lat", "stop %d: the place's time zone is not known: %v", bad+1, err)
	}
	return nil
}

// ── store ───────────────────────────────────────────────────────────────

func loadPlan(q sqlQueryer, id int64) (plan, bool, error) {
	p := plan{ID: id}
	var created, updated int64
	err := q.QueryRow(`SELECT name, created_at, updated_at FROM plans WHERE id = ?`, id).Scan(&p.Name, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return plan{}, false, nil
	}
	if err != nil {
		return plan{}, false, fmt.Errorf("get plan: %w", err)
	}
	c, u := time.Unix(created, 0).UTC(), time.Unix(updated, 0).UTC()
	p.CreatedAt, p.UpdatedAt = &c, &u
	rows, err := q.Query(`SELECT name, lat, lon, arrive, nights, vias, depart_time FROM plan_stops WHERE plan_id = ? ORDER BY position`, id)
	if err != nil {
		return plan{}, false, fmt.Errorf("get plan stops: %w", err)
	}
	defer rows.Close()
	p.Stops = []planStop{}
	for rows.Next() {
		var s planStop
		var vias string
		if err := rows.Scan(&s.Name, &s.Lat, &s.Lon, &s.Arrive, &s.Nights, &vias, &s.DepartTime); err != nil {
			return plan{}, false, fmt.Errorf("scan plan stop: %w", err)
		}
		if err := decodeStopVias(id, &s, vias); err != nil {
			return plan{}, false, err
		}
		p.Stops = append(p.Stops, s)
	}
	if err := rows.Err(); err != nil {
		return plan{}, false, fmt.Errorf("read plan stops: %w", err)
	}
	if err := rechainStops(p.Stops); err != nil {
		return plan{}, false, fmt.Errorf("plan %d: %w", id, err)
	}
	return p, true, nil
}

// decodeStopVias reads a stop's stored via places.
func decodeStopVias(id int64, s *planStop, vias string) error {
	if err := json.Unmarshal([]byte(vias), &s.Vias); err != nil {
		return fmt.Errorf("plan %d stop %q: stored via places are unreadable: %w", id, s.Name, err)
	}
	if s.Vias == nil {
		s.Vias = []planVia{}
	}
	return nil
}

func writePlanStops(tx *sql.Tx, id int64, stops []planStop) error {
	if _, err := tx.Exec(`DELETE FROM plan_stops WHERE plan_id = ?`, id); err != nil {
		return fmt.Errorf("replace plan stops: %w", err)
	}
	for i, s := range stops {
		vias, err := json.Marshal(s.Vias)
		if err != nil {
			return fmt.Errorf("encode via places: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO plan_stops (plan_id, position, name, lat, lon, arrive, nights, vias, depart_time)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, i, s.Name, s.Lat, s.Lon, s.Arrive, s.Nights, string(vias), s.DepartTime); err != nil {
			return fmt.Errorf("save plan stop: %w", err)
		}
	}
	return nil
}

// CreatePlan saves a new plan. A bad field comes back as a *planError.
func (s *documentStore) CreatePlan(in plan) (plan, error) {
	in, verr := normalisePlan(in)
	if verr == nil {
		verr = checkPlanZones(in)
	}
	if verr != nil {
		return plan{}, verr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var saved plan
	err := s.runTxLocked("create plan", func(tx *sql.Tx, now time.Time) error {
		res, err := tx.Exec(`INSERT INTO plans (name, created_at, updated_at) VALUES (?, ?, ?)`, in.Name, now.Unix(), now.Unix())
		if err != nil {
			return fmt.Errorf("create plan: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("create plan: %w", err)
		}
		if err := writePlanStops(tx, id, in.Stops); err != nil {
			return err
		}
		saved, _, err = loadPlan(tx, id)
		return err
	})
	if err != nil {
		return plan{}, err
	}
	return saved, nil
}

func (s *documentStore) GetPlan(id int64) (plan, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return loadPlan(s.db, id)
}

// ListPlans returns every plan, oldest first.
func (s *documentStore) ListPlans() ([]plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// One query: each plan's stops follow it in position order. A plan with no
	// stops has one row with a NULL position.
	rows, err := s.db.Query(`SELECT p.id, p.name, p.created_at, p.updated_at, s.position,
			COALESCE(s.name, ''), COALESCE(s.lat, 0), COALESCE(s.lon, 0), COALESCE(s.arrive, ''),
			COALESCE(s.nights, 0), COALESCE(s.vias, '[]'), COALESCE(s.depart_time, '')
		FROM plans p LEFT JOIN plan_stops s ON s.plan_id = p.id
		ORDER BY p.id, s.position`)
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	defer rows.Close()
	out := []plan{}
	for rows.Next() {
		var id, created, updated int64
		var name string
		var position sql.NullInt64
		var st planStop
		var vias string
		if err := rows.Scan(&id, &name, &created, &updated, &position,
			&st.Name, &st.Lat, &st.Lon, &st.Arrive, &st.Nights, &vias, &st.DepartTime); err != nil {
			return nil, fmt.Errorf("list plans: %w", err)
		}
		if len(out) == 0 || out[len(out)-1].ID != id {
			c, u := time.Unix(created, 0).UTC(), time.Unix(updated, 0).UTC()
			out = append(out, plan{ID: id, Name: name, Stops: []planStop{}, CreatedAt: &c, UpdatedAt: &u})
		}
		if !position.Valid {
			continue
		}
		if err := decodeStopVias(id, &st, vias); err != nil {
			return nil, err
		}
		cur := &out[len(out)-1]
		cur.Stops = append(cur.Stops, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	for i := range out {
		if err := rechainStops(out[i].Stops); err != nil {
			return nil, fmt.Errorf("plan %d: %w", out[i].ID, err)
		}
	}
	return out, nil
}

// UpdatePlan replaces a plan whole. ok is false when there is no such plan.
func (s *documentStore) UpdatePlan(id int64, in plan) (plan, bool, error) {
	in, verr := normalisePlan(in)
	if verr == nil {
		verr = checkPlanZones(in)
	}
	if verr != nil {
		return plan{}, false, verr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var saved plan
	found := true
	err := s.runTxLocked("update plan", func(tx *sql.Tx, now time.Time) error {
		res, err := tx.Exec(`UPDATE plans SET name = ?, updated_at = ? WHERE id = ?`, in.Name, now.Unix(), id)
		if err != nil {
			return fmt.Errorf("update plan: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			found = false
			return nil
		}
		if err := writePlanStops(tx, id, in.Stops); err != nil {
			return err
		}
		saved, _, err = loadPlan(tx, id)
		return err
	})
	if err != nil || !found {
		return plan{}, false, err
	}
	return saved, true, nil
}

// PatchPlanStop changes one stop's arrival or nights and shifts every later
// date. ok is false when there is no such plan.
func (s *documentStore) PatchPlanStop(id int64, idx int, arrive *string, nights *int) (plan, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var saved plan
	found := true
	err := s.runTxLocked("edit plan stop", func(tx *sql.Tx, now time.Time) error {
		cur, ok, err := loadPlan(tx, id)
		if err != nil {
			return err
		}
		if !ok {
			found = false
			return nil
		}
		stops, verr := applyStopEdit(cur.Stops, idx, arrive, nights)
		if verr != nil {
			return verr
		}
		// The same normalise and validate as a create or a replace, and what
		// it returns is what is written.
		cur.Stops = stops
		norm, verr := normalisePlan(cur)
		if verr == nil {
			verr = checkPlanZones(norm)
		}
		if verr != nil {
			return verr
		}
		if _, err := tx.Exec(`UPDATE plans SET updated_at = ? WHERE id = ?`, now.Unix(), id); err != nil {
			return fmt.Errorf("edit plan stop: %w", err)
		}
		if err := writePlanStops(tx, id, norm.Stops); err != nil {
			return err
		}
		saved, _, err = loadPlan(tx, id)
		return err
	})
	if err != nil || !found {
		return plan{}, false, err
	}
	return saved, true, nil
}

// DeletePlan removes a plan and its stops; ok is false when it was not there.
func (s *documentStore) DeletePlan(id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	err := s.runTxLocked("delete plan", func(tx *sql.Tx, _ time.Time) error {
		if _, err := tx.Exec(`DELETE FROM plan_stops WHERE plan_id = ?`, id); err != nil {
			return fmt.Errorf("delete plan: %w", err)
		}
		res, err := tx.Exec(`DELETE FROM plans WHERE id = ?`, id)
		if err != nil {
			return fmt.Errorf("delete plan: %w", err)
		}
		n, _ := res.RowsAffected()
		found = n > 0
		return nil
	})
	return found, err
}

// ── legs ────────────────────────────────────────────────────────────────

// planLeg is the routed run from one stop to the next through its via
// places. A segment the router cannot route fails the leg with its error and
// no figure; a straight line is never put in its place.
type planLeg struct {
	From       int          `json:"from"`
	To         int          `json:"to"`
	DistanceNm float64      `json:"distance_nm"`
	CourseDeg  int          `json:"course_deg"`
	Route      [][2]float64 `json:"route"`
	Note       string       `json:"note,omitempty"`
	// Error is the full sentence, "no passage distance for A to B: <reason>".
	// Reason is the router's own words alone.
	Error  string `json:"error,omitempty"`
	Reason string `json:"reason,omitempty"`
	// ErrorKind and ErrorStop are structured forms of Error for the page's
	// own wording: "on_land" with the stop whose end is off the water, or
	// "closed_off" with the two places (ErrorStop to ErrorTo) the charted
	// shoreline cuts apart.
	ErrorKind string `json:"error_kind,omitempty"`
	ErrorStop string `json:"error_stop,omitempty"`
	ErrorTo   string `json:"error_to,omitempty"`
	// Moved lists stops charted behind the shoreline and how far, as Note
	// says in the router's words.
	Moved []planLegMoved `json:"moved,omitempty"`
}

type planLegMoved struct {
	Stop string  `json:"stop"`
	Nm   float64 `json:"nm"`
}

// planLegsDeadline bounds the whole legs request. A segment still routing
// when it passes fails its leg, and every leg after it, with that reason.
var planLegsDeadline = 60 * time.Second

// planRoutes remembers the plan legs' routed segments.
var planRoutes = newWaterRouteCache()

// planCoastline is the on-board shoreline, wired as the route_distance tool
// wires it (offlineCoastlineReaderFor). A variable so a test can stand in.
var planCoastline = func() *offlineCoastlineReader {
	return offlineCoastlineReaderFor(getEnv("SETTINGS_FILE", "../settings.yaml"))()
}

// planRouteSegment stands in for the segment router in a test. It is nil in
// production, where the coastline reader is checked once per request.
var planRouteSegment func(ctx context.Context, aLat, aLon, bLat, bLon float64) (waterRoute, error)

// planSegmentRouter is the router one legs request uses. The install, the
// switch and the squares are checked once, here, not per segment. When any of
// them fails, every segment fails with that reason, so each leg says so.
func planSegmentRouter() segmentRouter {
	if planRouteSegment != nil {
		stand := planRouteSegment
		return func(ctx context.Context, a, b [2]float64) (waterRoute, error) {
			return stand(ctx, a[0], a[1], b[0], b[1])
		}
	}
	route, err := planCoastline().memoisedRouter(planRoutes)
	if err != nil {
		return func(context.Context, [2]float64, [2]float64) (waterRoute, error) { return waterRoute{}, err }
	}
	return route
}

// planLegWorkers is how many legs are routed at once.
const planLegWorkers = 3

// planLegs routes every leg of the plan, a few at a time. A leg that fails
// carries its error.
func planLegs(ctx context.Context, p plan) []planLeg {
	ctx, cancel := context.WithTimeout(ctx, planLegsDeadline)
	defer cancel()
	legs := make([]planLeg, max(len(p.Stops)-1, 0))
	route := planSegmentRouter()
	var wg sync.WaitGroup
	sem := make(chan struct{}, planLegWorkers)
	for i := range legs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			legs[i] = planLegFor(ctx, route, p, i)
		}()
	}
	wg.Wait()
	return legs
}

func planLegFor(ctx context.Context, route segmentRouter, p plan, i int) planLeg {
	leg := planLeg{From: i, To: i + 1, Route: [][2]float64{}}
	from, to := p.Stops[i], p.Stops[i+1]
	points := [][2]float64{{from.Lat, from.Lon}}
	for _, v := range from.Vias {
		points = append(points, [2]float64{v.Lat, v.Lon})
	}
	points = append(points, [2]float64{to.Lat, to.Lon})

	// A segment that finishes after the request's time is up has not routed.
	timed := func(ctx context.Context, a, b [2]float64) (waterRoute, error) {
		r, err := route(ctx, a, b)
		if err == nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			err = fmt.Errorf("the plan's legs took longer than %v to route", planLegsDeadline)
		}
		return r, err
	}
	passage, err := routePassageThrough(ctx, timed, points)
	if err != nil {
		var seg *routedPassageError
		errors.As(err, &seg)
		leg.Reason = err.Error()
		leg.Error = fmt.Sprintf("no passage distance for %s to %s: %s", segmentName(p, i, seg.Segment), segmentName(p, i, seg.Segment+1), leg.Reason)
		var onLand *waterRouteOnLandError
		if errors.Is(err, errNoWaterRoute) {
			leg.ErrorKind = "closed_off"
			leg.ErrorStop = segmentName(p, i, seg.Segment)
			leg.ErrorTo = segmentName(p, i, seg.Segment+1)
		} else if errors.As(err, &onLand) {
			leg.ErrorKind = "on_land"
			if onLand.AtStart {
				leg.ErrorStop = segmentName(p, i, seg.Segment)
			} else {
				leg.ErrorStop = segmentName(p, i, seg.Segment+1)
			}
		}
		return leg
	}
	for _, pt := range passage.Points {
		leg.Route = append(leg.Route, [2]float64{roundTo5(pt.Lat), roundTo5(pt.Lon)})
	}
	// A via ends one segment and starts the next, so it can be reported twice.
	seen := map[int]bool{}
	for _, m := range passage.Moved {
		if seen[m.Point] {
			continue
		}
		seen[m.Point] = true
		leg.Moved = append(leg.Moved, planLegMoved{Stop: segmentName(p, i, m.Point), Nm: m.Nm})
	}
	leg.DistanceNm = roundTo1(passage.DistanceNm)
	leg.Note = strings.Join(passage.Notes, " ")
	leg.CourseDeg = passage.CourseDeg
	return leg
}

// segmentName names point k of leg i: the stop, a via, or the next stop.
func segmentName(p plan, i, k int) string {
	if k == 0 {
		return p.Stops[i].Name
	}
	if k-1 < len(p.Stops[i].Vias) {
		return p.Stops[i].Vias[k-1].Name
	}
	return p.Stops[i+1].Name
}

// ── handlers ────────────────────────────────────────────────────────────

func planZoneError(c echo.Context, err error) error {
	log.Printf("plans: %v", err)
	return c.JSON(http.StatusInternalServerError, map[string]string{"error": "a plan's time zone could not be found: " + err.Error()})
}

func planNotFound(c echo.Context) error {
	return c.JSON(http.StatusNotFound, map[string]string{"error": "plan not found"})
}

func planIDParam(c echo.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	return id, err == nil && id > 0
}

// planWriteError answers a failed write: a bad field is a 400 {field, message}.
func planWriteError(c echo.Context, err error) error {
	var verr *planError
	if errors.As(err, &verr) {
		return c.JSON(http.StatusBadRequest, map[string]string{"field": verr.Field, "message": verr.Message})
	}
	return writeDocumentError(c, err)
}

// listPlansHandler is GET /api/plans: every plan with its status, and the id
// of the one the table opens on.
func listPlansHandler(c echo.Context) error {
	plans, err := globalDocumentStore.ListPlans()
	if err != nil {
		return writeDocumentError(c, err)
	}
	now := planNow()
	out := make([]plan, 0, len(plans))
	for _, p := range plans {
		d, err := decoratePlan(p, now)
		if err != nil {
			return planZoneError(c, err)
		}
		out = append(out, d)
	}
	var current *int64
	idx, status := selectCurrentPlan(out)
	if status != "" && status != planStatusPast {
		id := out[idx].ID
		current = &id
	}
	return c.JSON(http.StatusOK, map[string]any{"plans": out, "current_id": current})
}

func respondPlan(c echo.Context, p plan) error {
	d, err := decoratePlan(p, planNow())
	if err != nil {
		return planZoneError(c, err)
	}
	return c.JSON(http.StatusOK, d)
}

func getPlanHandler(c echo.Context) error {
	id, ok := planIDParam(c)
	if !ok {
		return planNotFound(c)
	}
	p, found, err := globalDocumentStore.GetPlan(id)
	if err != nil {
		return writeDocumentError(c, err)
	}
	if !found {
		return planNotFound(c)
	}
	return respondPlan(c, p)
}

func createPlanHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	var req plan
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	saved, err := globalDocumentStore.CreatePlan(req)
	if err != nil {
		return planWriteError(c, err)
	}
	return respondPlan(c, saved)
}

func updatePlanHandler(c echo.Context) error {
	id, ok := planIDParam(c)
	if !ok {
		return planNotFound(c)
	}
	limitNoteRequestBody(c)
	var req plan
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	saved, found, err := globalDocumentStore.UpdatePlan(id, req)
	if err != nil {
		return planWriteError(c, err)
	}
	if !found {
		return planNotFound(c)
	}
	planAssessMemoDrop(id)
	return respondPlan(c, saved)
}

// patchPlanStopHandler is PATCH /api/plans/:id/stops/:index with an optional
// arrive and nights; every later date shifts with it.
func patchPlanStopHandler(c echo.Context) error {
	id, ok := planIDParam(c)
	if !ok {
		return planNotFound(c)
	}
	idx, err := strconv.Atoi(c.Param("index"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"field": "index", "message": "index must be a stop number"})
	}
	limitNoteRequestBody(c)
	var req struct {
		Arrive *string `json:"arrive"`
		Nights *int    `json:"nights"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if req.Arrive == nil && req.Nights == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"field": "nights", "message": "give arrive or nights"})
	}
	saved, found, err := globalDocumentStore.PatchPlanStop(id, idx, req.Arrive, req.Nights)
	if err != nil {
		return planWriteError(c, err)
	}
	if !found {
		return planNotFound(c)
	}
	planAssessMemoDrop(id)
	return respondPlan(c, saved)
}

func deletePlanHandler(c echo.Context) error {
	id, ok := planIDParam(c)
	if !ok {
		return planNotFound(c)
	}
	found, err := globalDocumentStore.DeletePlan(id)
	if err != nil {
		return writeDocumentError(c, err)
	}
	if !found {
		return planNotFound(c)
	}
	planAssessMemoDrop(id)
	return c.JSON(http.StatusOK, map[string]bool{"deleted": true})
}

// planLegsHandler is GET /api/plans/:id/legs: each leg's routed distance,
// course and path, or the reason it could not be routed.
func planLegsHandler(c echo.Context) error {
	id, ok := planIDParam(c)
	if !ok {
		return planNotFound(c)
	}
	p, found, err := globalDocumentStore.GetPlan(id)
	if err != nil {
		return writeDocumentError(c, err)
	}
	if !found {
		return planNotFound(c)
	}
	return c.JSON(http.StatusOK, map[string]any{"legs": planLegs(c.Request().Context(), p)})
}

// plansRoutes returns the /api routes for passage plans. Each row carries its
// own auth tier (docs/adr/0177).
func plansRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/plans", tierRead, listPlansHandler},
		{http.MethodPost, "/api/plans", tierWrite, createPlanHandler},
		{http.MethodGet, "/api/plans/:id", tierRead, getPlanHandler},
		{http.MethodPut, "/api/plans/:id", tierWrite, updatePlanHandler},
		{http.MethodDelete, "/api/plans/:id", tierWrite, deletePlanHandler},
		{http.MethodPatch, "/api/plans/:id/stops/:index", tierWrite, patchPlanStopHandler},
		{http.MethodGet, "/api/plans/:id/legs", tierRead, planLegsHandler},
		{http.MethodGet, "/api/plans/:id/assessment", tierRead, planAssessmentHandler},
	}
}
