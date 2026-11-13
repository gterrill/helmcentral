package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
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
	planNameMax     = 120
	planMaxStops    = 30
	planMaxVias     = 10
	planMaxNights   = 365
	planDateLayout  = "2006-01-02"
	planStatusPast  = "past"
	planStatusSoon  = "upcoming"
	planStatusToday = "active"
)

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

func addPlanDays(date string, days int) string {
	d, err := time.Parse(planDateLayout, date)
	if err != nil {
		return date
	}
	return d.AddDate(0, 0, days).Format(planDateLayout)
}

func planDaysBetween(from, to time.Time) int {
	return int(math.Round(to.Sub(from).Hours() / 24))
}

// rechainStops makes every arrival after the first the previous arrival plus
// its nights.
func rechainStops(stops []planStop) {
	for i := range stops {
		if i > 0 {
			stops[i].Arrive = addPlanDays(stops[i-1].Arrive, stops[i-1].Nights)
		}
		stops[i].Depart = addPlanDays(stops[i].Arrive, stops[i].Nights)
	}
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

func checkLatLon(field string, lat, lon float64) *planError {
	if math.IsNaN(lat) || lat < -90 || lat > 90 {
		return &planError{Field: field, Message: "lat must be within -90..90"}
	}
	if math.IsNaN(lon) || lon < -180 || lon > 180 {
		return &planError{Field: field, Message: "lon must be within -180..180"}
	}
	return nil
}

// normalisePlan is the one rule the Forecast page's writes and a later Mate
// changeset share: names are trimmed, every field is checked, and each
// arrival after the first is recomputed from the stop before it.
func normalisePlan(p plan) (plan, *planError) {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return plan{}, &planError{Field: "name", Message: "name the plan"}
	}
	if utf8.RuneCountInString(p.Name) > planNameMax {
		return plan{}, &planError{Field: "name", Message: fmt.Sprintf("name is longer than %d characters", planNameMax)}
	}
	if len(p.Stops) == 0 {
		return plan{}, &planError{Field: "stops", Message: "add at least one stop"}
	}
	if len(p.Stops) > planMaxStops {
		return plan{}, &planError{Field: "stops", Message: fmt.Sprintf("a plan has at most %d stops", planMaxStops)}
	}
	p.Stops = copyStops(p.Stops)
	last := len(p.Stops) - 1
	for i := range p.Stops {
		s := &p.Stops[i]
		s.Name = strings.TrimSpace(s.Name)
		if s.Name == "" {
			return plan{}, &planError{Field: "stops.name", Message: fmt.Sprintf("name stop %d", i+1)}
		}
		if utf8.RuneCountInString(s.Name) > planNameMax {
			return plan{}, &planError{Field: "stops.name", Message: fmt.Sprintf("stop %d: name is longer than %d characters", i+1, planNameMax)}
		}
		if e := checkLatLon("stops.lat", s.Lat, s.Lon); e != nil {
			if strings.HasPrefix(e.Message, "lon") {
				e.Field = "stops.lon"
			}
			e.Message = fmt.Sprintf("stop %d: %s", i+1, e.Message)
			return plan{}, e
		}
		if s.Nights < 0 || s.Nights > planMaxNights {
			return plan{}, &planError{Field: "stops.nights", Message: fmt.Sprintf("stop %d: nights must be 0..%d", i+1, planMaxNights)}
		}
		if i == 0 {
			if _, e := parsePlanDate("stops.arrive", s.Arrive); e != nil {
				return plan{}, e
			}
		}
		if len(s.Vias) > planMaxVias {
			return plan{}, &planError{Field: "vias", Message: fmt.Sprintf("stop %d: at most %d via places", i+1, planMaxVias)}
		}
		for j := range s.Vias {
			v := &s.Vias[j]
			v.Name = strings.TrimSpace(v.Name)
			if v.Name == "" {
				return plan{}, &planError{Field: "vias", Message: fmt.Sprintf("stop %d: name via place %d", i+1, j+1)}
			}
			if e := checkLatLon("vias", v.Lat, v.Lon); e != nil {
				e.Message = fmt.Sprintf("stop %d via %d: %s", i+1, j+1, e.Message)
				return plan{}, e
			}
		}
		if s.DepartTime != "" && !planTimePattern.MatchString(s.DepartTime) {
			return plan{}, &planError{Field: "depart_time", Message: fmt.Sprintf("stop %d: departure time must be HH:MM", i+1)}
		}
		if i == last {
			if len(s.Vias) > 0 {
				return plan{}, &planError{Field: "vias", Message: "the last stop has no leg leaving it, so no via places"}
			}
			if s.DepartTime != "" {
				return plan{}, &planError{Field: "depart_time", Message: "the last stop has no leg leaving it, so no departure time"}
			}
		}
		if s.Vias == nil {
			s.Vias = []planVia{}
		}
	}
	rechainStops(p.Stops)
	return p, nil
}

// applyStopEdit changes one stop's arrival date or nights and shifts every
// later date with it. Moving the first arrival moves the whole plan; moving a
// later arrival is the nights of the stop before it. The input is not
// modified.
func applyStopEdit(stops []planStop, idx int, arrive *string, nights *int) ([]planStop, *planError) {
	if idx < 0 || idx >= len(stops) {
		return nil, &planError{Field: "index", Message: "there is no such stop"}
	}
	out := copyStops(stops)
	if nights != nil && (*nights < 0 || *nights > planMaxNights) {
		return nil, &planError{Field: "nights", Message: fmt.Sprintf("nights must be 0..%d", planMaxNights)}
	}
	if arrive != nil {
		want, e := parsePlanDate("arrive", *arrive)
		if e != nil {
			return nil, e
		}
		if idx == 0 {
			out[0].Arrive = *arrive
		} else {
			before, _ := time.Parse(planDateLayout, out[idx-1].Arrive)
			n := planDaysBetween(before, want)
			if n < 0 {
				return nil, &planError{Field: "arrive", Message: "that is before the boat reaches the stop before"}
			}
			if n > planMaxNights {
				return nil, &planError{Field: "arrive", Message: fmt.Sprintf("that is more than %d nights after the stop before", planMaxNights)}
			}
			out[idx-1].Nights = n
		}
	}
	if nights != nil {
		out[idx].Nights = *nights
	}
	rechainStops(out)
	return out, nil
}

// ── time: status and the current plan ───────────────────────────────────

// planToday is today's date at the plan's first stop.
func planToday(p plan, now time.Time) (string, error) {
	if len(p.Stops) == 0 {
		return "", errors.New("plan has no stops")
	}
	zone, err := stayZoneAt(p.Stops[0].Lat, p.Stops[0].Lon)
	if err != nil {
		return "", err
	}
	return now.In(zone.Loc).Format(planDateLayout), nil
}

func planDates(p plan) (first, last string) {
	if len(p.Stops) == 0 {
		return "", ""
	}
	end := p.Stops[len(p.Stops)-1]
	return p.Stops[0].Arrive, addPlanDays(end.Arrive, end.Nights)
}

// planStatus is "upcoming", "active" (the dates span today) or "past",
// judged at the first stop.
func planStatus(p plan, now time.Time) (string, error) {
	today, err := planToday(p, now)
	if err != nil {
		return "", err
	}
	first, last := planDates(p)
	switch {
	case today < first:
		return planStatusSoon, nil
	case today > last:
		return planStatusPast, nil
	}
	return planStatusToday, nil
}

// selectCurrentPlan picks the plan the table opens on: the one whose dates
// span today, else the next upcoming. ok is false when every plan is past.
func selectCurrentPlan(plans []plan, now time.Time) (int, bool, error) {
	active, soon := -1, -1
	for i, p := range plans {
		st, err := planStatus(p, now)
		if err != nil {
			return 0, false, err
		}
		first, _ := planDates(p)
		switch st {
		case planStatusToday:
			if active < 0 {
				active = i
			} else if f, _ := planDates(plans[active]); first < f {
				active = i
			}
		case planStatusSoon:
			if soon < 0 {
				soon = i
			} else if f, _ := planDates(plans[soon]); first < f {
				soon = i
			}
		}
	}
	if active >= 0 {
		return active, true, nil
	}
	if soon >= 0 {
		return soon, true, nil
	}
	return 0, false, nil
}

// decoratePlan adds what is worked out on every read: status, first and last
// dates, each stop's departure date and its zone. A position with no zone is
// an error, not UTC.
func decoratePlan(p plan, now time.Time) (plan, error) {
	p.Stops = copyStops(p.Stops)
	rechainStops(p.Stops)
	for i := range p.Stops {
		z, err := stayZoneAt(p.Stops[i].Lat, p.Stops[i].Lon)
		if err != nil {
			return p, fmt.Errorf("stop %d (%s): %w", i+1, p.Stops[i].Name, err)
		}
		p.Stops[i].Timezone = z.Name
	}
	st, err := planStatus(p, now)
	if err != nil {
		return p, err
	}
	p.Status = st
	p.FirstDate, p.LastDate = planDates(p)
	return p, nil
}

// checkPlanZones refuses a stop whose position has no time zone before
// anything is saved.
func checkPlanZones(p plan) *planError {
	for i, s := range p.Stops {
		if _, err := stayZoneAt(s.Lat, s.Lon); err != nil {
			return &planError{Field: "stops.lat", Message: fmt.Sprintf("stop %d: the place's time zone is not known: %v", i+1, err)}
		}
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
		if err := json.Unmarshal([]byte(vias), &s.Vias); err != nil {
			return plan{}, false, fmt.Errorf("plan %d stop %q: stored via places are unreadable: %w", id, s.Name, err)
		}
		if s.Vias == nil {
			s.Vias = []planVia{}
		}
		p.Stops = append(p.Stops, s)
	}
	if err := rows.Err(); err != nil {
		return plan{}, false, fmt.Errorf("read plan stops: %w", err)
	}
	rechainStops(p.Stops)
	return p, true, nil
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
	rows, err := s.db.Query(`SELECT id FROM plans ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("list plans: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	out := make([]plan, 0, len(ids))
	for _, id := range ids {
		p, ok, err := loadPlan(s.db, id)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, p)
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
	Error      string       `json:"error,omitempty"`
	// ErrorKind and ErrorStop are structured forms of Error for the page's
	// own wording: "on_land" and the stop whose end is off the water.
	ErrorKind string `json:"error_kind,omitempty"`
	ErrorStop string `json:"error_stop,omitempty"`
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

// planFailureTTL is how long a failed segment is remembered, so a plan table
// refetched every few seconds does not repeat a search that just failed.
var planFailureTTL = 30 * time.Second

const planRouteCacheMax = 256

// planSegmentKey names a segment on one state of the coastline: the stamp
// changes when a square is downloaded, replaced or removed.
type planSegmentKey struct {
	pts   [4]float64
	stamp string
}

type planSegmentEntry struct {
	route   waterRoute
	err     error
	expires time.Time // zero for a success: its stamp keeps it honest
}

type planRouteCache struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[planSegmentKey]planSegmentEntry
}

var planRoutes = &planRouteCache{now: time.Now}

func (c *planRouteCache) lookup(key planSegmentKey) (planSegmentEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if ok && !e.expires.IsZero() && !c.now().Before(e.expires) {
		delete(c.entries, key)
		return planSegmentEntry{}, false
	}
	return e, ok
}

func (c *planRouteCache) store(key planSegmentKey, route waterRoute, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil || len(c.entries) >= planRouteCacheMax {
		c.entries = map[planSegmentKey]planSegmentEntry{}
	}
	e := planSegmentEntry{route: route, err: err}
	if err != nil {
		e.expires = c.now().Add(planFailureTTL)
	}
	c.entries[key] = e
}

// planCoastline is the on-board shoreline, wired as the route_distance tool
// wires it (offlineCoastlineReaderFor). A variable so a test can stand in.
var planCoastline = func() *offlineCoastlineReader {
	return offlineCoastlineReaderFor(getEnv("SETTINGS_FILE", "../settings.yaml"))()
}

// planCoastlineStamp identifies the squares held and ready right now.
func planCoastlineStamp(r *offlineCoastlineReader) (string, error) {
	recs, err := r.Store.Records()
	if err != nil {
		return "", fmt.Errorf("could not read the Offline coastline squares: %w", err)
	}
	h := fnv.New64a()
	for _, rec := range recs {
		if rec.Status == coastlineStatusReady {
			fmt.Fprintf(h, "%v,%v,%d,%d;", rec.Square.Lat, rec.Square.Lon, rec.DownloadedAt.Unix(), rec.DataAsOf.Unix())
		}
	}
	return strconv.FormatUint(h.Sum64(), 16), nil
}

// planRouteSegment routes one point-to-point segment over water. A variable
// so a test can stand in for the coastline. A result is remembered only for
// the coastline it was found on: turning the coastline off ends it, a changed
// set of squares starts afresh, and a failure is kept only briefly.
var planRouteSegment = func(ctx context.Context, aLat, aLon, bLat, bLon float64) (waterRoute, error) {
	reader := planCoastline()
	if reader == nil || reader.Store == nil {
		return waterRoute{}, errors.New("Offline coastline is not available on this install, so the run cannot be routed over water")
	}
	enabled, err := reader.Enabled()
	if err != nil {
		return waterRoute{}, fmt.Errorf("could not read the Offline coastline setting: %w", err)
	}
	if !enabled {
		return waterRoute{}, errors.New("Offline coastline is off — turn it on in Settings → Offline coastline")
	}
	stamp, err := planCoastlineStamp(reader)
	if err != nil {
		return waterRoute{}, err
	}
	key := planSegmentKey{pts: [4]float64{aLat, aLon, bLat, bLon}, stamp: stamp}
	if e, ok := planRoutes.lookup(key); ok {
		return e.route, e.err
	}
	r, err := reader.RouteOverWater(ctx, aLat, aLon, bLat, bLon)
	// A failure caused by this request running out of time says nothing about
	// the segment.
	if ctx.Err() == nil {
		planRoutes.store(key, r, err)
	}
	return r, err
}

// planLegs routes every leg of the plan. A leg that fails carries its error.
func planLegs(ctx context.Context, p plan) []planLeg {
	ctx, cancel := context.WithTimeout(ctx, planLegsDeadline)
	defer cancel()
	var legs []planLeg
	for i := 0; i+1 < len(p.Stops); i++ {
		legs = append(legs, planLegFor(ctx, p, i))
	}
	if legs == nil {
		legs = []planLeg{}
	}
	return legs
}

func planLegFor(ctx context.Context, p plan, i int) planLeg {
	leg := planLeg{From: i, To: i + 1, Route: [][2]float64{}}
	from, to := p.Stops[i], p.Stops[i+1]
	points := [][2]float64{{from.Lat, from.Lon}}
	for _, v := range from.Vias {
		points = append(points, [2]float64{v.Lat, v.Lon})
	}
	points = append(points, [2]float64{to.Lat, to.Lon})

	var total float64
	var route [][2]float64
	var notes []string
	for k := 0; k+1 < len(points); k++ {
		a, b := points[k], points[k+1]
		r, err := planRouteSegment(ctx, a[0], a[1], b[0], b[1])
		if err == nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			err = fmt.Errorf("the plan's legs took longer than %v to route", planLegsDeadline)
		}
		if err != nil {
			leg.Error = fmt.Sprintf("no passage distance for %s to %s: %v", segmentName(p, i, k), segmentName(p, i, k+1), err)
			var onLand *waterRouteOnLandError
			if errors.As(err, &onLand) {
				leg.ErrorKind = "on_land"
				if onLand.End == "start" {
					leg.ErrorStop = segmentName(p, i, k)
				} else {
					leg.ErrorStop = segmentName(p, i, k+1)
				}
			}
			return leg
		}
		total += r.DistanceNm
		for j, pt := range r.Points {
			if k > 0 && j == 0 {
				continue
			}
			route = append(route, [2]float64{roundTo5(pt.Lat), roundTo5(pt.Lon)})
		}
		if r.StartMovedNm > 0 {
			leg.Moved = append(leg.Moved, planLegMoved{Stop: segmentName(p, i, k), Nm: r.StartMovedNm})
		}
		if r.EndMovedNm > 0 {
			leg.Moved = append(leg.Moved, planLegMoved{Stop: segmentName(p, i, k+1), Nm: r.EndMovedNm})
		}
		if n := routeMovedNote(r); n != "" {
			notes = append(notes, n)
		}
	}
	leg.DistanceNm = roundTo1(total)
	leg.Route = route
	leg.Note = strings.Join(notes, " ")
	// The course is the bearing of the longest segment, the same rule the
	// route_distance tool uses.
	longest := -1.0
	for j := 1; j < len(route); j++ {
		a, b := route[j-1], route[j]
		nm := haversineMeters(a[0], a[1], b[0], b[1]) / metersPerNauticalMile
		if nm > longest {
			longest = nm
			leg.CourseDeg = int(math.Round(bearingDeg(a[0], a[1], b[0], b[1]))) % 360
		}
	}
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
	now := plannedStayNow()
	out := make([]plan, 0, len(plans))
	for _, p := range plans {
		d, err := decoratePlan(p, now)
		if err != nil {
			return planZoneError(c, err)
		}
		out = append(out, d)
	}
	var current *int64
	idx, ok, err := selectCurrentPlan(out, now)
	if err != nil {
		return planZoneError(c, err)
	}
	if ok {
		id := out[idx].ID
		current = &id
	}
	return c.JSON(http.StatusOK, map[string]any{"plans": out, "current_id": current})
}

func respondPlan(c echo.Context, p plan) error {
	d, err := decoratePlan(p, plannedStayNow())
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
	}
}
