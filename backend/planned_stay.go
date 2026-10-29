package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/labstack/echo/v4"
)

// plannedStay is the one place the operator is booked into and the dates they
// will be there: the Forecast page can show the weather for it. There is at
// most one, kept in helmcentral.sqlite so every device sees it. Arrive and
// Depart are calendar days in the place's own reckoning (YYYY-MM-DD), the way
// the operator reads them off a booking. The stay is treated as absent from
// the day after departure; the row itself is only replaced or cleared by the
// operator, never swept.
//
// No omitempty: the Forecast page binds it through a TypeScript interface
// that declares every field.
type plannedStay struct {
	Name      string     `json:"name"`
	Lat       float64    `json:"lat"`
	Lon       float64    `json:"lon"`
	Arrive    string     `json:"arrive"`
	Depart    string     `json:"depart"`
	UpdatedAt *time.Time `json:"updated_at"`
}

// plannedStayNameMax bounds the place name; it is shown in a chip and a
// header and sent to Mate, so it is a name and not a paragraph.
const plannedStayNameMax = 120

// plannedStayError names the one field a write failed on, in the
// {field, message} shape the other forms use.
type plannedStayError struct {
	Field   string
	Message string
}

func (e *plannedStayError) Error() string { return e.Field + ": " + e.Message }

func asPlannedStayError(err error, target **plannedStayError) bool {
	return errors.As(err, target)
}

func createPlannedStaySchema(db *sql.DB) error {
	// One row, id pinned to 1.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS planned_stay (
			id         INTEGER PRIMARY KEY CHECK (id = 1),
			name       TEXT NOT NULL,
			lat        REAL NOT NULL CHECK (lat >= -90 AND lat <= 90),
			lon        REAL NOT NULL CHECK (lon >= -180 AND lon <= 180),
			arrive     TEXT NOT NULL,
			depart     TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		)`); err != nil {
		return fmt.Errorf("create planned_stay table: %w", err)
	}
	return nil
}

func parseStayDate(field, v string) (time.Time, *plannedStayError) {
	if !installDatePattern.MatchString(v) {
		return time.Time{}, &plannedStayError{Field: field, Message: field + " must be a date, YYYY-MM-DD"}
	}
	d, err := time.Parse("2006-01-02", v)
	if err != nil {
		return time.Time{}, &plannedStayError{Field: field, Message: field + " is not a real date"}
	}
	return d, nil
}

// normalisePlannedStay is the one rule both writers share, the Forecast page's
// PUT and Mate's changeset: the name is trimmed, then every field is checked,
// and a stay whose departure has already passed at the place is refused (it
// would be saved and then read back as no stay at all).
func normalisePlannedStay(s plannedStay, now time.Time) (plannedStay, *plannedStayError) {
	s.Name = strings.TrimSpace(s.Name)
	if verr := validatePlannedStay(s); verr != nil {
		return plannedStay{}, verr
	}
	active, zerr := plannedStayActive(s, now)
	if zerr != nil {
		return plannedStay{}, &plannedStayError{Field: "lat", Message: "the place's time zone is not known: " + zerr.Error()}
	}
	if !active {
		return plannedStay{}, &plannedStayError{Field: "depart", Message: "depart has already passed at the place"}
	}
	return s, nil
}

func validatePlannedStay(s plannedStay) *plannedStayError {
	if s.Name == "" {
		return &plannedStayError{Field: "name", Message: "name the place"}
	}
	if utf8.RuneCountInString(s.Name) > plannedStayNameMax {
		return &plannedStayError{Field: "name", Message: fmt.Sprintf("name is longer than %d characters", plannedStayNameMax)}
	}
	if s.Lat < -90 || s.Lat > 90 {
		return &plannedStayError{Field: "lat", Message: "lat must be within -90..90"}
	}
	if s.Lon < -180 || s.Lon > 180 {
		return &plannedStayError{Field: "lon", Message: "lon must be within -180..180"}
	}
	arrive, verr := parseStayDate("arrive", s.Arrive)
	if verr != nil {
		return verr
	}
	depart, verr := parseStayDate("depart", s.Depart)
	if verr != nil {
		return verr
	}
	if depart.Before(arrive) {
		return &plannedStayError{Field: "depart", Message: "depart cannot be before arrive"}
	}
	return nil
}

// plannedStayNow is the clock the handler judges expiry by; tests replace it.
var plannedStayNow = time.Now

// plannedStayActive reports whether the stay still counts at now. Its dates
// are the place's own, so "today" is the date at the place (the zone found at
// its position), not the server's or the device's: it is gone the day after
// departure. A place whose zone cannot be found is an error, not a guess.
func plannedStayActive(s plannedStay, now time.Time) (bool, error) {
	zone, err := zoneAt(s.Lat, s.Lon)
	if err != nil {
		return false, err
	}
	today := now.In(zone.Loc).Format("2006-01-02")
	return s.Depart >= today, nil
}

func plannedStayFrom(q sqlQueryer) (plannedStay, bool, error) {
	var s plannedStay
	var updated int64
	err := q.QueryRow(`SELECT name, lat, lon, arrive, depart, updated_at FROM planned_stay WHERE id = 1`).
		Scan(&s.Name, &s.Lat, &s.Lon, &s.Arrive, &s.Depart, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return plannedStay{}, false, nil
	}
	if err != nil {
		return plannedStay{}, false, fmt.Errorf("get planned stay: %w", err)
	}
	t := time.Unix(updated, 0).UTC()
	s.UpdatedAt = &t
	return s, true, nil
}

func upsertPlannedStayTx(tx *sql.Tx, s plannedStay, now time.Time) error {
	if _, err := tx.Exec(`
		INSERT INTO planned_stay (id, name, lat, lon, arrive, depart, updated_at) VALUES (1, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, lat = excluded.lat, lon = excluded.lon,
			arrive = excluded.arrive, depart = excluded.depart, updated_at = excluded.updated_at`,
		s.Name, s.Lat, s.Lon, s.Arrive, s.Depart, now.Unix()); err != nil {
		return fmt.Errorf("set planned stay: %w", err)
	}
	return nil
}

// GetPlannedStay returns the saved stay whether or not it has expired; ok is
// false when nothing is saved. Callers that care about today use
// plannedStayActive.
func (s *documentStore) GetPlannedStay() (plannedStay, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return plannedStayFrom(s.db)
}

// SetPlannedStay replaces the stay.
func (s *documentStore) SetPlannedStay(in plannedStay) (plannedStay, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	in, verr := normalisePlannedStay(in, plannedStayNow())
	if verr != nil {
		return plannedStay{}, verr
	}
	var saved plannedStay
	err := s.runTxLocked("set planned stay", func(tx *sql.Tx, now time.Time) error {
		if err := upsertPlannedStayTx(tx, in, now); err != nil {
			return err
		}
		var err error
		saved, _, err = plannedStayFrom(tx)
		return err
	})
	if err != nil {
		return plannedStay{}, err
	}
	return saved, nil
}

// ClearPlannedStay removes the stay. Clearing when there is none is not an
// error.
func (s *documentStore) ClearPlannedStay() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runTxLocked("clear planned stay", func(tx *sql.Tx, _ time.Time) error {
		return deletePlannedStayTx(tx)
	})
}

func deletePlannedStayTx(tx *sql.Tx) error {
	if _, err := tx.Exec(`DELETE FROM planned_stay WHERE id = 1`); err != nil {
		return fmt.Errorf("clear planned stay: %w", err)
	}
	return nil
}

type plannedStayResponse struct {
	Stay *plannedStay `json:"stay"`
}

// getPlannedStayHandler is GET /api/planned-stay. The answer is {"stay": null}
// when nothing is planned or the stay ended before today at the place.
func getPlannedStayHandler(c echo.Context) error {
	stay, ok, err := globalDocumentStore.GetPlannedStay()
	if err != nil {
		return writeDocumentError(c, err)
	}
	if !ok {
		return c.JSON(http.StatusOK, plannedStayResponse{})
	}
	active, zerr := plannedStayActive(stay, plannedStayNow())
	if zerr != nil {
		return writeDocumentError(c, zerr)
	}
	if !active {
		return c.JSON(http.StatusOK, plannedStayResponse{})
	}
	return c.JSON(http.StatusOK, plannedStayResponse{Stay: &stay})
}

// putPlannedStayHandler is PUT /api/planned-stay: a whole replace. A bad
// field is a 400 {field, message}.
func putPlannedStayHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	var req plannedStay
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	saved, err := globalDocumentStore.SetPlannedStay(req)
	var verr *plannedStayError
	if errors.As(err, &verr) {
		return c.JSON(http.StatusBadRequest, map[string]string{"field": verr.Field, "message": verr.Message})
	}
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, plannedStayResponse{Stay: &saved})
}

// deletePlannedStayHandler is DELETE /api/planned-stay.
func deletePlannedStayHandler(c echo.Context) error {
	if err := globalDocumentStore.ClearPlannedStay(); err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, plannedStayResponse{})
}

// plannedStayRoutes returns the /api routes for the planned stay.
// Each row carries its own auth tier (docs/adr/0177).
func plannedStayRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/planned-stay", tierRead, getPlannedStayHandler},
		{http.MethodPut, "/api/planned-stay", tierWrite, putPlannedStayHandler},
		{http.MethodDelete, "/api/planned-stay", tierWrite, deletePlannedStayHandler},
	}
}

// ── Mate (ADR 0208) ─────────────────────────────────────────────────────
//
// The stay is one record with the fixed id "stay", like the vessel details:
// Mate proposes it through propose_changes and the operator applies the card,
// so it is never set silently. There is no create: the first update gives
// every field.

const (
	recordTypePlannedStay = "planned_stay"
	plannedStayRecordID   = "stay"
	// plannedStayUnsetVersion is the version of the record before any stay is
	// saved, so a changeset written against the empty record is still
	// checked for freshness when it is applied.
	plannedStayUnsetVersion = "unset"
)

func init() {
	defaultRecordRegistry.register(plannedStayRecordType())
}

func plannedStayRecordType() *recordType {
	return &recordType{
		Name:  recordTypePlannedStay,
		Label: "planned stay",
		Summary: "The one place and dates the operator is booked into (a marina berth for a week, say), which the Forecast page " +
			"can show the weather for. There is at most one, with the id \"stay\"; it can be updated or deleted, never created. " +
			"The first update gives every field: name, lat, lon (from find_places), arrive and depart (YYYY-MM-DD, local dates at the place). " +
			"A stay stops counting the day after depart. To see what is planned today use get_planned_stay.",
		Actions: []string{changeUpdate, changeDelete},
		Fields: []recordField{
			{Name: "name", Kind: kindString, Writable: true, Description: "the place as the operator names it, e.g. \"Coral Coast Marina\""},
			{Name: "lat", Kind: kindNumber, Writable: true, Description: "latitude of the place in decimal degrees, from find_places"},
			{Name: "lon", Kind: kindNumber, Writable: true, Description: "longitude of the place in decimal degrees, from find_places"},
			{Name: "arrive", Kind: kindDate, Writable: true, Description: "the day the boat arrives, YYYY-MM-DD"},
			{Name: "depart", Kind: kindDate, Writable: true, Description: "the day the boat leaves, YYYY-MM-DD, not before arrive"},
		},
		Get:      getPlannedStayRecord,
		List:     listPlannedStayRecord,
		Update:   updatePlannedStayRecord,
		Delete:   deletePlannedStayRecord,
		Describe: describePlannedStay,
		Href:     func(recordSnapshot) string { return "/forecast" },
	}
}

func plannedStaySnapshot(s plannedStay, ok bool) recordSnapshot {
	version := plannedStayUnsetVersion
	if ok && s.UpdatedAt != nil {
		version = s.UpdatedAt.UTC().Format(time.RFC3339)
	}
	fields := map[string]any{"name": "", "lat": nil, "lon": nil, "arrive": "", "depart": ""}
	label := "Planned stay"
	if ok {
		fields = map[string]any{"name": s.Name, "lat": s.Lat, "lon": s.Lon, "arrive": s.Arrive, "depart": s.Depart}
		label = "Planned stay · " + s.Name
	}
	return recordSnapshot{ID: plannedStayRecordID, Label: label, Version: version, Fields: fields}
}

func getPlannedStayRecord(q sqlQueryer, id string) (recordSnapshot, error) {
	if id != plannedStayRecordID {
		return recordSnapshot{}, errRecordNotFound
	}
	s, ok, err := plannedStayFrom(q)
	if err != nil {
		return recordSnapshot{}, err
	}
	return plannedStaySnapshot(s, ok), nil
}

func listPlannedStayRecord(s *documentStore, _ map[string]string) ([]recordSnapshot, error) {
	stay, ok, err := s.GetPlannedStay()
	if err != nil {
		return nil, err
	}
	return []recordSnapshot{plannedStaySnapshot(stay, ok)}, nil
}

func updatePlannedStayRecord(env changeEnv, before recordSnapshot, given fieldSet) error {
	cur, ok, err := plannedStayFrom(env.tx)
	if err != nil {
		return err
	}
	if !ok {
		for _, name := range []string{"name", "lat", "lon", "arrive", "depart"} {
			if _, has := given[name]; !has {
				return fieldErr(name, "no stay is planned yet, so the first update has to give name, lat, lon, arrive and depart; %s is missing", name)
			}
		}
	}
	if val, has := given["name"]; has {
		cur.Name = stringOf(val)
	}
	if val, has := given["lat"]; has {
		if f := floatPtrOf(val); f != nil {
			cur.Lat = *f
		}
	}
	if val, has := given["lon"]; has {
		if f := floatPtrOf(val); f != nil {
			cur.Lon = *f
		}
	}
	if val, has := given["arrive"]; has {
		cur.Arrive = stringOf(val)
	}
	if val, has := given["depart"]; has {
		cur.Depart = stringOf(val)
	}
	cur, verr := normalisePlannedStay(cur, plannedStayNow())
	if verr != nil {
		return fieldErr(verr.Field, "%s", verr.Message)
	}
	return upsertPlannedStayTx(env.tx, cur, env.now)
}

func deletePlannedStayRecord(env changeEnv, before recordSnapshot) error {
	if before.Version == plannedStayUnsetVersion {
		return fieldErr("id", "no stay is planned, so there is nothing to clear")
	}
	return deletePlannedStayTx(env.tx)
}

func describePlannedStay(d describeInput) string {
	if d.Op.Action == changeDelete {
		return "Clear the planned stay"
	}
	var parts []string
	for _, f := range d.Type.Fields {
		if val, ok := d.Op.Fields[f.Name]; ok {
			parts = append(parts, fmt.Sprintf("%s %v", f.Name, val))
		}
	}
	return "Save the planned stay: " + strings.Join(parts, ", ")
}

type assistantPlannedStayResult struct {
	Stay *plannedStay `json:"stay"`
	Note string       `json:"note,omitempty"`
}

func (d assistantToolDeps) executeGetPlannedStay(ctx context.Context, _ json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	store, err := d.recordStore("get_planned_stay")
	if err != nil {
		return "", err
	}
	stay, ok, err := store.GetPlannedStay()
	if err != nil {
		return "", fmt.Errorf("get_planned_stay: %w", err)
	}
	var res assistantPlannedStayResult
	active := false
	if ok {
		var zerr error
		if active, zerr = plannedStayActive(stay, d.now()); zerr != nil {
			return "", fmt.Errorf("get_planned_stay: the stay's time zone is not known: %w", zerr)
		}
	}
	switch {
	case !ok:
		res.Note = "No stay is planned."
	case !active:
		res.Note = "The last planned stay ended before today, so none is planned."
	default:
		res.Stay = &stay
		res.Note = "The operator set this stay on the Forecast page. Its dates are local to the place. " +
			"get_wind_forecast and get_tides take its lat and lon."
	}
	return capToolResultJSON(&res, nil)
}

var mateToolGetPlannedStay = mateTool{
	name: "get_planned_stay",
	function: openRouterFunctionDef{
		Description: "Read the stay the operator has planned: a place (name, lat, lon) and the dates they arrive and depart, " +
			"which the Forecast page shows the weather for. Returns no stay when none is planned or it has ended. " +
			"Use it when the operator talks about \"the marina\" or \"our stay\" or asks about weather or tides there; " +
			"pass its lat and lon to get_wind_forecast and get_tides.",
		Parameters: json.RawMessage(`{"type": "object", "properties": {}}`),
	},
	execute: assistantToolDeps.executeGetPlannedStay,
	describe: func(args json.RawMessage) string {
		return "Reading the planned stay…"
	},
}
