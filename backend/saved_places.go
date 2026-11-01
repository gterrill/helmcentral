package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// Saved places: named positions the operator gave Mate ("Thomas Island South:
// 20°32.676' S 149°07.136' E") that no map names. Mate offers to save one
// through the ordinary proposal card; find_places answers from them before it
// asks the place-names provider; the Saved places settings section lets the
// operator rename, move or delete them.

const (
	recordTypeSavedPlace = "saved_place"

	maxSavedPlaceNameChars = 80
	maxSavedPlaceNoteChars = 300
)

var (
	errSavedPlaceNotFound  = errors.New("saved place not found")
	errSavedPlaceNameTaken = errors.New("saved place name taken")
)

type savedPlace struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Note string  `json:"note"`
	// DepthM is the water at the place in metres at chart datum, nil when no
	// depth is known (a drying spot is negative). DepthSource says where the
	// figure came from (savedPlaceDepthCharted or savedPlaceDepthSounded) and
	// DepthDate is the day it was taken or entered, YYYY-MM-DD. The three are
	// set together or not at all, and a new depth replaces the old one.
	DepthM      *float64  `json:"depth_m"`
	DepthSource string    `json:"depth_source"`
	DepthDate   string    `json:"depth_date"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

const (
	// savedPlaceDepthCharted is a figure the operator read off a chart or was
	// told; savedPlaceDepthSounded is one the boat measured at anchor.
	savedPlaceDepthCharted = "charted"
	savedPlaceDepthSounded = "sounded"

	// The bounds on a depth at chart datum: a drying height is negative.
	minSavedPlaceDepthM = -10.0
	maxSavedPlaceDepthM = 1000.0
)

func createSavedPlacesSchema(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS saved_places (
			id              TEXT PRIMARY KEY,
			name            TEXT NOT NULL,
			name_key        TEXT NOT NULL UNIQUE,
			lat             REAL NOT NULL,
			lon             REAL NOT NULL,
			note            TEXT NOT NULL DEFAULT '',
			created_at      INTEGER NOT NULL,
			updated_at      INTEGER NOT NULL
		)`); err != nil {
		return fmt.Errorf("create saved_places table: %w", err)
	}
	// The depth columns came after the table did; the single operator has no
	// installed base, so adding them in place is the whole migration.
	for _, col := range [][2]string{
		{"depth_m", "REAL"},
		{"depth_source", "TEXT NOT NULL DEFAULT ''"},
		{"depth_date", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := addColumnIfMissing(db, "saved_places", col[0], col[1]); err != nil {
			return err
		}
	}
	return nil
}

// savedPlaceKey is both the uniqueness key and find_places' match key: case,
// punctuation and spacing are ignored and "Is" or "Isl" read as "Island".
// Nothing fuzzier, since a near miss that returns the wrong anchorage is worse
// than none.
func savedPlaceKey(name string) string {
	words := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for i, w := range words {
		if w == "is" || w == "isl" {
			words[i] = "island"
		}
	}
	return strings.Join(words, " ")
}

// validateSavedPlace trims and checks a place. The proposal path and the REST
// path both come through it.
func validateSavedPlace(p *savedPlace) error {
	p.Name = strings.Join(strings.Fields(p.Name), " ")
	p.Note = strings.TrimSpace(p.Note)
	if p.Name == "" || savedPlaceKey(p.Name) == "" {
		return fieldErr("name", "a saved place needs a name")
	}
	if strings.IndexFunc(p.Name, unicode.IsControl) >= 0 {
		return fieldErr("name", "can't contain control characters such as line breaks or tabs")
	}
	if utf8.RuneCountInString(p.Name) > maxSavedPlaceNameChars {
		return fieldErr("name", "must be %d characters or fewer", maxSavedPlaceNameChars)
	}
	if utf8.RuneCountInString(p.Note) > maxSavedPlaceNoteChars {
		return fieldErr("note", "must be %d characters or fewer", maxSavedPlaceNoteChars)
	}
	if !validLatLon(p.Lat, 0) {
		return fieldErr("lat", "must be a latitude between -90 and 90 degrees")
	}
	if !validLatLon(0, p.Lon) {
		return fieldErr("lon", "must be a longitude between -180 and 180 degrees")
	}
	return validateSavedPlaceDepth(p)
}

// validDatumDepthM says whether v is a depth in metres at chart datum a saved
// place, or an anchorage, can have.
func validDatumDepthM(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= minSavedPlaceDepthM && v <= maxSavedPlaceDepthM
}

// validateSavedPlaceDepth checks the three depth fields as a group: none, or a
// finite depth with a known source and a calendar date.
func validateSavedPlaceDepth(p *savedPlace) error {
	p.DepthSource, p.DepthDate = strings.TrimSpace(p.DepthSource), strings.TrimSpace(p.DepthDate)
	if p.DepthM == nil {
		if p.DepthSource != "" || p.DepthDate != "" {
			return fieldErr("depth_m", "is required when a depth source or date is given")
		}
		return nil
	}
	if !validDatumDepthM(*p.DepthM) {
		return fieldErr("depth_m", "must be a depth in metres at chart datum between %g and %g (a drying height is negative)", minSavedPlaceDepthM, maxSavedPlaceDepthM)
	}
	if p.DepthSource != savedPlaceDepthCharted && p.DepthSource != savedPlaceDepthSounded {
		return fieldErr("depth_source", "must be %q or %q when a depth is set", savedPlaceDepthCharted, savedPlaceDepthSounded)
	}
	if _, err := time.Parse("2006-01-02", p.DepthDate); err != nil {
		return fieldErr("depth_date", "must be the date the depth was taken, as YYYY-MM-DD")
	}
	return nil
}

const savedPlaceColumns = `id, name, lat, lon, note, depth_m, depth_source, depth_date, created_at, updated_at`

func scanSavedPlace(row rowScanner) (savedPlace, error) {
	var p savedPlace
	var created, updated int64
	var depth sql.NullFloat64
	if err := row.Scan(&p.ID, &p.Name, &p.Lat, &p.Lon, &p.Note, &depth, &p.DepthSource, &p.DepthDate, &created, &updated); err != nil {
		return savedPlace{}, err
	}
	if depth.Valid {
		p.DepthM = &depth.Float64
	}
	p.CreatedAt = time.Unix(0, created).UTC()
	p.UpdatedAt = time.Unix(0, updated).UTC()
	return p, nil
}

// querySavedPlace reads the one place whose column equals arg,
// errSavedPlaceNotFound when there is none.
func querySavedPlace(q sqlQueryer, column, arg string) (savedPlace, error) {
	p, err := scanSavedPlace(q.QueryRow(`SELECT `+savedPlaceColumns+` FROM saved_places WHERE `+column+` = ?`, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return savedPlace{}, errSavedPlaceNotFound
	}
	if err != nil {
		return savedPlace{}, fmt.Errorf("get saved place: %w", err)
	}
	return p, nil
}

func savedPlaceByIDQ(q sqlQueryer, id string) (savedPlace, error) {
	return querySavedPlace(q, "id", id)
}

// savedPlaceByNameQ finds the place a name refers to, errSavedPlaceNotFound when
// there is none.
func savedPlaceByNameQ(q sqlQueryer, name string) (savedPlace, error) {
	return querySavedPlace(q, "name_key", savedPlaceKey(name))
}

// findSavedPlaceByName is savedPlaceByNameQ in its own read: nil, nil when no
// place has the name.
func findSavedPlaceByName(store *documentStore, name string) (*savedPlace, error) {
	var found *savedPlace
	err := store.Read(func(q sqlQueryer) error {
		p, err := savedPlaceByNameQ(q, name)
		if errors.Is(err, errSavedPlaceNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = &p
		return nil
	})
	return found, err
}

// querySavedPlaces runs the saved places select with a WHERE clause (empty for
// every place) and its arguments, in name order.
func querySavedPlaces(q sqlQueryer, what, where string, args ...any) ([]savedPlace, error) {
	rows, err := q.Query(`SELECT `+savedPlaceColumns+` FROM saved_places `+where+` ORDER BY name_key`, args...)
	if err != nil {
		return nil, fmt.Errorf("list saved places%s: %w", what, err)
	}
	defer rows.Close()
	out := []savedPlace{}
	for rows.Next() {
		p, err := scanSavedPlace(rows)
		if err != nil {
			return nil, fmt.Errorf("scan saved place: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func listSavedPlacesQ(q sqlQueryer) ([]savedPlace, error) {
	return querySavedPlaces(q, "", "")
}

// listSavedPlacesNearQ lists the places in the latitude and longitude box that
// holds the circle of radiusM metres round a point. The box is a little
// larger than the circle, so the caller still measures the distance. Near the
// poles or the antimeridian the longitude bound is dropped rather than
// wrapped, which only widens the box.
func listSavedPlacesNearQ(q sqlQueryer, lat, lon, radiusM float64) ([]savedPlace, error) {
	const metersPerDegree = 111320.0
	dLat := 1.1 * radiusM / metersPerDegree
	lonLo, lonHi := -180.0, 180.0
	if c := math.Cos(lat * math.Pi / 180); c > 0.01 {
		if dLon := dLat / c; lon-dLon >= -180 && lon+dLon <= 180 {
			lonLo, lonHi = lon-dLon, lon+dLon
		}
	}
	return querySavedPlaces(q, " near a point", `WHERE lat BETWEEN ? AND ? AND lon BETWEEN ? AND ?`, lat-dLat, lat+dLat, lonLo, lonHi)
}

// readSavedPlacesNear is listSavedPlacesNearQ in its own read.
func readSavedPlacesNear(store *documentStore, lat, lon, radiusM float64) ([]savedPlace, error) {
	var places []savedPlace
	err := store.Read(func(q sqlQueryer) error {
		var err error
		places, err = listSavedPlacesNearQ(q, lat, lon, radiusM)
		return err
	})
	return places, err
}

// readSavedPlaces is listSavedPlacesQ in its own read.
func readSavedPlaces(store *documentStore) ([]savedPlace, error) {
	var places []savedPlace
	err := store.Read(func(q sqlQueryer) error {
		var err error
		places, err = listSavedPlacesQ(q)
		return err
	})
	return places, err
}

// checkSavedPlaceNameFree refuses a name another saved place already has.
func checkSavedPlaceNameFree(q sqlQueryer, name, exceptID string) error {
	other, err := savedPlaceByNameQ(q, name)
	if errors.Is(err, errSavedPlaceNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if other.ID == exceptID {
		return nil
	}
	return &fieldedError{Field: "name", Message: fmt.Sprintf("a saved place called %q already exists; change that one instead", other.Name), cause: errSavedPlaceNameTaken}
}

// savedPlaceWriteError names a name_key collision for what it is. The name
// check beforehand cannot see a write that lands between it and the insert.
func savedPlaceWriteError(err error) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: saved_places.name_key") {
		return &fieldedError{Field: "name", Message: "a saved place with that name already exists", cause: errSavedPlaceNameTaken}
	}
	return err
}

func createSavedPlaceTx(x alarmSQLExecer, p savedPlace, now time.Time) (savedPlace, error) {
	if err := validateSavedPlace(&p); err != nil {
		return savedPlace{}, err
	}
	if err := checkSavedPlaceNameFree(x, p.Name, ""); err != nil {
		return savedPlace{}, err
	}
	p.ID = uuid.NewString()
	p.CreatedAt, p.UpdatedAt = now.UTC(), now.UTC()
	if _, err := x.Exec(`INSERT INTO saved_places (id, name, name_key, lat, lon, note, depth_m, depth_source, depth_date, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, savedPlaceKey(p.Name), p.Lat, p.Lon, p.Note, p.DepthM, p.DepthSource, p.DepthDate, p.CreatedAt.UnixNano(), p.UpdatedAt.UnixNano()); err != nil {
		return savedPlace{}, fmt.Errorf("insert saved place: %w", savedPlaceWriteError(err))
	}
	return p, nil
}

func updateSavedPlaceTx(x alarmSQLExecer, next savedPlace, now time.Time) (savedPlace, error) {
	if err := validateSavedPlace(&next); err != nil {
		return savedPlace{}, err
	}
	if err := checkSavedPlaceNameFree(x, next.Name, next.ID); err != nil {
		return savedPlace{}, err
	}
	next.UpdatedAt = now.UTC()
	res, err := x.Exec(`UPDATE saved_places SET name = ?, name_key = ?, lat = ?, lon = ?, note = ?, depth_m = ?, depth_source = ?, depth_date = ?, updated_at = ? WHERE id = ?`,
		next.Name, savedPlaceKey(next.Name), next.Lat, next.Lon, next.Note, next.DepthM, next.DepthSource, next.DepthDate, next.UpdatedAt.UnixNano(), next.ID)
	if err != nil {
		return savedPlace{}, fmt.Errorf("update saved place: %w", savedPlaceWriteError(err))
	}
	if err := checkRowsAffected(res, errSavedPlaceNotFound); err != nil {
		return savedPlace{}, err
	}
	return next, nil
}

func deleteSavedPlaceTx(x alarmSQLExecer, id string) error {
	res, err := x.Exec(`DELETE FROM saved_places WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete saved place: %w", err)
	}
	return checkRowsAffected(res, errSavedPlaceNotFound)
}

func formatDegreesMinutes(v float64, degreeDigits int, pos, neg string) string {
	hemi := pos
	if v < 0 {
		hemi, v = neg, -v
	}
	deg := math.Floor(v)
	min := math.Round((v-deg)*60*1000) / 1000
	if min >= 60 {
		deg, min = deg+1, 0
	}
	return fmt.Sprintf("%0*d°%06.3f' %s", degreeDigits, int(deg), min, hemi)
}

// formatSavedPlacePosition writes a position the way a chart plotter does:
// degrees and decimal minutes, "20°32.676' S 149°07.136' E".
func formatSavedPlacePosition(lat, lon float64) string {
	return formatDegreesMinutes(lat, 2, "N", "S") + " " + formatDegreesMinutes(lon, 3, "E", "W")
}

// ── record type ──────────────────────────────────────────────────────────

func init() {
	defaultRecordRegistry.register(savedPlaceRecordType())
}

func savedPlaceRecordType() *recordType {
	return &recordType{
		Name:  recordTypeSavedPlace,
		Label: "saved place",
		Summary: "A named position the operator gave, such as an anchorage or a waypoint no chart or map names. " +
			"find_places answers from saved places before it asks the map. Names are unique ignoring case and punctuation. " +
			"Use save_place to propose one; the operator taps Apply.",
		Actions: []string{changeCreate, changeUpdate, changeDelete},
		Fields: []recordField{
			{Name: "name", Kind: kindString, Writable: true, Description: "the operator's name for the spot, up to 80 characters"},
			{Name: "lat", Kind: kindNumber, Writable: true, Description: "latitude in decimal degrees, south negative"},
			{Name: "lon", Kind: kindNumber, Writable: true, Description: "longitude in decimal degrees, west negative"},
			{Name: "note", Kind: kindString, Writable: true, Description: "an optional note, up to 300 characters"},
			{Name: "depth_m", Kind: kindNumber, Writable: true, Nullable: true, Description: "the water here in metres at chart datum, the figure the operator gave (a drying height is negative); null clears the depth, source and date. Set it together with depth_source and depth_date"},
			{Name: "depth_source", Kind: kindString, Writable: true, Enum: []string{savedPlaceDepthCharted, savedPlaceDepthSounded}, Description: "where the depth came from: charted for a figure the operator gave you, sounded only for one the boat measured at anchor"},
			{Name: "depth_date", Kind: kindDate, Writable: true, Description: "the day the depth was taken or given, YYYY-MM-DD"},
		},
		Get: func(q sqlQueryer, id string) (recordSnapshot, error) {
			p, err := savedPlaceByIDQ(q, id)
			if errors.Is(err, errSavedPlaceNotFound) {
				return recordSnapshot{}, errRecordNotFound
			}
			if err != nil {
				return recordSnapshot{}, err
			}
			return savedPlaceSnapshot(p), nil
		},
		List: func(s *documentStore, _ map[string]string) ([]recordSnapshot, error) {
			places, err := readSavedPlaces(s)
			if err != nil {
				return nil, err
			}
			out := make([]recordSnapshot, 0, len(places))
			for _, p := range places {
				out = append(out, savedPlaceSnapshot(p))
			}
			return out, nil
		},
		Create: func(env changeEnv, given fieldSet) (createdRecord, error) {
			for _, n := range []string{"name", "lat", "lon"} {
				if _, ok := given[n]; !ok {
					return createdRecord{}, fieldErr(n, "is required")
				}
			}
			p, err := applySavedPlaceFields(savedPlace{}, given)
			if err != nil {
				return createdRecord{}, err
			}
			created, err := createSavedPlaceTx(env.tx, p, env.now)
			if err != nil {
				return createdRecord{}, err
			}
			return createdRecord{ID: created.ID}, nil
		},
		Update: func(env changeEnv, before recordSnapshot, given fieldSet) error {
			current, err := savedPlaceByIDQ(env.tx, before.ID)
			if err != nil {
				return err
			}
			next, err := applySavedPlaceFields(current, given)
			if err != nil {
				return err
			}
			_, err = updateSavedPlaceTx(env.tx, next, env.now)
			return err
		},
		Delete: func(env changeEnv, before recordSnapshot) error {
			return deleteSavedPlaceTx(env.tx, before.ID)
		},
		Describe: describeSavedPlace,
		Display:  displaySavedPlaceFields,
		Href:     func(recordSnapshot) string { return "/settings/saved-places" },
	}
}

func savedPlaceVersion(p savedPlace) string {
	return p.UpdatedAt.UTC().Format(time.RFC3339Nano)
}

func savedPlaceSnapshot(p savedPlace) recordSnapshot {
	// A nil *float64 inside an any would not read as null, so the depth is nil
	// or a plain number.
	var depth any
	if p.DepthM != nil {
		depth = *p.DepthM
	}
	return recordSnapshot{
		ID: p.ID, Label: p.Name, Version: savedPlaceVersion(p),
		Fields: map[string]any{
			"name": p.Name, "lat": p.Lat, "lon": p.Lon, "note": p.Note,
			"depth_m": depth, "depth_source": p.DepthSource, "depth_date": p.DepthDate,
		},
	}
}

func applySavedPlaceFields(p savedPlace, given fieldSet) (savedPlace, error) {
	if v, ok := given["name"]; ok {
		p.Name = stringOf(v)
	}
	if v, ok := given["note"]; ok {
		p.Note = stringOf(v)
	}
	for _, f := range []struct {
		name string
		dst  *float64
	}{{"lat", &p.Lat}, {"lon", &p.Lon}} {
		if v, ok := given[f.name]; ok {
			n := floatPtrOf(v)
			if n == nil {
				return savedPlace{}, fieldErr(f.name, "must be a number")
			}
			*f.dst = *n
		}
	}
	return applySavedPlaceDepthFields(p, given)
}

// applySavedPlaceDepthFields lays the depth group over a place. A depth_m of
// null clears the group; a number replaces it and must come with its own source
// and date, so a new figure never inherits the old one's provenance. A source
// or date with no depth_m is refused.
func applySavedPlaceDepthFields(p savedPlace, given fieldSet) (savedPlace, error) {
	depth, hasDepth := given["depth_m"]
	_, hasSource := given["depth_source"]
	_, hasDate := given["depth_date"]
	switch {
	case !hasDepth && (hasSource || hasDate):
		return savedPlace{}, fieldErr("depth_m", "is required when a depth source or date is given")
	case !hasDepth:
		return p, nil
	case depth == nil:
		p.DepthM, p.DepthSource, p.DepthDate = nil, "", ""
		return p, nil
	}
	n := floatPtrOf(depth)
	if n == nil {
		return savedPlace{}, fieldErr("depth_m", "must be a number")
	}
	if !hasSource || !hasDate {
		return savedPlace{}, fieldErr("depth_source", "and depth_date are required with a new depth_m")
	}
	p.DepthM, p.DepthSource, p.DepthDate = n, stringOf(given["depth_source"]), stringOf(given["depth_date"])
	return p, nil
}

func savedPlacePositionOf(f map[string]any) string {
	lat, lon := floatPtrOf(f["lat"]), floatPtrOf(f["lon"])
	if lat == nil || lon == nil {
		return ""
	}
	return formatSavedPlacePosition(*lat, *lon)
}

func describeSavedPlace(d describeInput) string {
	switch d.Op.Action {
	case changeCreate:
		return "Save " + d.After.Label + " at " + savedPlacePositionOf(d.After.Fields)
	case changeDelete:
		return "Forget saved place " + d.Before.Label
	}
	if savedPlacePositionOf(d.Before.Fields) != savedPlacePositionOf(d.After.Fields) {
		return "Move saved place " + d.Before.Label + " to " + savedPlacePositionOf(d.After.Fields)
	}
	if savedPlaceDepthOf(d.Before.Fields) != savedPlaceDepthOf(d.After.Fields) {
		if after := savedPlaceDepthOf(d.After.Fields); after != "" {
			return "Set the depth of saved place " + d.Before.Label + " to " + after
		}
		return "Clear the depth of saved place " + d.Before.Label
	}
	return "Change saved place " + d.Before.Label
}

// savedPlaceDepthOf writes a place's depth the way the operator reads it,
// "4.0 m at datum (charted 2026-11-01)", or "" when it has none.
func savedPlaceDepthOf(f map[string]any) string {
	depth := floatPtrOf(f["depth_m"])
	if depth == nil {
		return ""
	}
	return fmt.Sprintf("%.1f m at datum (%s %s)", *depth, stringOf(f["depth_source"]), stringOf(f["depth_date"]))
}

// displaySavedPlaceFields shows lat and lon as one position in degrees and
// minutes.
func displaySavedPlaceFields(snap recordSnapshot, fields map[string]any) map[string]any {
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		switch k {
		case "lat", "lon":
			out["position"] = savedPlacePositionOf(mergeFields(snap.Fields, fields))
			continue
		case "depth_m", "depth_source", "depth_date":
			// One line for the three, from the whole record the display is of.
			if d := savedPlaceDepthOf(mergeFields(snap.Fields, fields)); d != "" {
				out["depth"] = d
			} else {
				out["depth"] = "none"
			}
			continue
		}
		out[k] = v
	}
	return out
}

// ── REST ─────────────────────────────────────────────────────────────────

func savedPlaceRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/saved-places", tierRead, listSavedPlacesHandler},
		{http.MethodPost, "/api/saved-places", tierWrite, createSavedPlaceHandler},
		{http.MethodPatch, "/api/saved-places/:id", tierWrite, patchSavedPlaceHandler},
		{http.MethodDelete, "/api/saved-places/:id", tierWrite, deleteSavedPlaceHandler},
	}
}

func savedPlaceStore() (*documentStore, error) {
	return requireDocumentStore("saved places")
}

func savedPlaceError(c echo.Context, err error) error {
	var fe *fieldedError
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, errSavedPlaceNotFound):
		status = http.StatusNotFound
	case errors.Is(err, errSavedPlaceNameTaken):
		status = http.StatusConflict
	case errors.As(err, &fe):
		status = http.StatusBadRequest
	}
	return c.JSON(status, map[string]string{"error": err.Error()})
}

// GET /api/saved-places
func listSavedPlacesHandler(c echo.Context) error {
	docs, err := savedPlaceStore()
	if err != nil {
		return savedPlaceError(c, err)
	}
	places, err := readSavedPlaces(docs)
	if err != nil {
		return savedPlaceError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"places": places})
}

// POST /api/saved-places adds a place by hand. It runs the validator and the
// name rule the proposal path does.
func createSavedPlaceHandler(c echo.Context) error {
	docs, err := savedPlaceStore()
	if err != nil {
		return savedPlaceError(c, err)
	}
	var req struct {
		Name        string   `json:"name"`
		Lat         *float64 `json:"lat"`
		Lon         *float64 `json:"lon"`
		Note        string   `json:"note"`
		DepthM      *float64 `json:"depth_m"`
		DepthSource string   `json:"depth_source"`
		DepthDate   string   `json:"depth_date"`
	}
	dec := json.NewDecoder(c.Request().Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return savedPlaceError(c, fieldErr("body", "%s", err.Error()))
	}
	if req.Lat == nil {
		return savedPlaceError(c, fieldErr("lat", "is required"))
	}
	if req.Lon == nil {
		return savedPlaceError(c, fieldErr("lon", "is required"))
	}
	var created savedPlace
	err = docs.RunTx("create saved place", func(tx *sql.Tx, now time.Time) error {
		var err error
		created, err = createSavedPlaceTx(tx, savedPlace{
			Name: req.Name, Lat: *req.Lat, Lon: *req.Lon, Note: req.Note,
			DepthM: req.DepthM, DepthSource: req.DepthSource, DepthDate: req.DepthDate,
		}, now)
		return err
	})
	if err != nil {
		return savedPlaceError(c, err)
	}
	return c.JSON(http.StatusCreated, created)
}

// PATCH /api/saved-places/:id renames, moves or re-notes a place, or sets or
// clears its depth; only the fields sent change. A depth_m of null clears the
// depth, its source and its date.
func patchSavedPlaceHandler(c echo.Context) error {
	docs, err := savedPlaceStore()
	if err != nil {
		return savedPlaceError(c, err)
	}
	var given fieldSet
	if err := json.NewDecoder(c.Request().Body).Decode(&given); err != nil {
		return savedPlaceError(c, fieldErr("body", "%s", err.Error()))
	}
	for k, v := range given {
		switch {
		case k != "name" && k != "lat" && k != "lon" && k != "note" && k != "depth_m" && k != "depth_source" && k != "depth_date":
			return savedPlaceError(c, fieldErr("body", "json: unknown field %q", k))
		case v == nil && k != "depth_m":
			delete(given, k) // null leaves the field as it is; a null depth_m clears the depth
		}
	}
	var updated savedPlace
	err = docs.RunTx("change saved place", func(tx *sql.Tx, now time.Time) error {
		p, err := savedPlaceByIDQ(tx, c.Param("id"))
		if err != nil {
			return err
		}
		if p, err = applySavedPlaceFields(p, given); err != nil {
			return err
		}
		updated, err = updateSavedPlaceTx(tx, p, now)
		return err
	})
	if err != nil {
		return savedPlaceError(c, err)
	}
	return c.JSON(http.StatusOK, updated)
}

// DELETE /api/saved-places/:id
func deleteSavedPlaceHandler(c echo.Context) error {
	docs, err := savedPlaceStore()
	if err != nil {
		return savedPlaceError(c, err)
	}
	if err := docs.RunTx("delete saved place", func(tx *sql.Tx, _ time.Time) error {
		return deleteSavedPlaceTx(tx, c.Param("id"))
	}); err != nil {
		return savedPlaceError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
