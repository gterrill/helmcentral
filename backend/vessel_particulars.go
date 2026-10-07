package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// vesselParticulars is the one record of facts about the boat that SignalK
// does not publish (vessel_particulars, import_store.go). The fields SignalK
// does publish - name, MMSI, call sign, length, beam, draft, air height - are
// not stored here; the boat's own data feed stays their source.
//
// No omitempty, ADR 0115 section 7: the settings page binds this through a
// TypeScript interface that declares every field.
type vesselParticulars struct {
	Builder        string   `json:"builder"`
	Model          string   `json:"model"`
	Year           *int     `json:"year"`
	HIN            string   `json:"hin"`
	Flag           string   `json:"flag"`
	HailingPort    string   `json:"hailing_port"`
	HullType       string   `json:"hull_type"`
	HullMaterial   string   `json:"hull_material"`
	DisplacementKG *float64 `json:"displacement_kg"`
	ShorePower     string   `json:"shore_power"`
	SystemVoltage  string   `json:"system_voltage"`
	Registration   string   `json:"registration"`
	IMO            string   `json:"imo"`
	EPIRBID        string   `json:"epirb_id"`
	DateAcquired   string   `json:"date_acquired"`
	// Length and beam as the paperwork wants them. SignalK's design.length is
	// the live source for LOA when the boat publishes it; the stored value is
	// what the operator typed, for forms filled in with nothing connected.
	LOAM  *float64 `json:"loa_m"`
	BeamM *float64 `json:"beam_m"`
	// Owner and insurance: what every insurer declaration and berth
	// application asks again. StormDelegate is the person who prepares the
	// boat when the owner is away, free text.
	OwnerName     string     `json:"owner_name"`
	OwnerPhone    string     `json:"owner_phone"`
	OwnerEmail    string     `json:"owner_email"`
	Insurer       string     `json:"insurer"`
	PolicyNumber  string     `json:"policy_number"`
	HomeMarina    string     `json:"home_marina"`
	Berth         string     `json:"berth"`
	StormDelegate string     `json:"storm_delegate"`
	UpdatedAt     *time.Time `json:"updated_at"`
}

// vesselParticularsError names the one field a PUT failed on, in the
// {field, message} shape the equipment form already uses.
type vesselParticularsError struct {
	Field   string
	Message string
}

func (e *vesselParticularsError) Error() string { return e.Field + ": " + e.Message }

func asVesselParticularsError(err error, target **vesselParticularsError) bool {
	return errors.As(err, target)
}

// trimParticulars trims every text field so a stray space never becomes part
// of a hull number.
func trimParticulars(v vesselParticulars) vesselParticulars {
	v.Builder = strings.TrimSpace(v.Builder)
	v.Model = strings.TrimSpace(v.Model)
	v.HIN = strings.TrimSpace(v.HIN)
	v.Flag = strings.TrimSpace(v.Flag)
	v.HailingPort = strings.TrimSpace(v.HailingPort)
	v.HullType = strings.TrimSpace(v.HullType)
	v.HullMaterial = strings.TrimSpace(v.HullMaterial)
	v.ShorePower = strings.TrimSpace(v.ShorePower)
	v.SystemVoltage = strings.TrimSpace(v.SystemVoltage)
	v.Registration = strings.TrimSpace(v.Registration)
	v.IMO = strings.TrimSpace(v.IMO)
	v.EPIRBID = strings.TrimSpace(v.EPIRBID)
	v.DateAcquired = strings.TrimSpace(v.DateAcquired)
	v.OwnerName = strings.TrimSpace(v.OwnerName)
	v.OwnerPhone = strings.TrimSpace(v.OwnerPhone)
	v.OwnerEmail = strings.TrimSpace(v.OwnerEmail)
	v.Insurer = strings.TrimSpace(v.Insurer)
	v.PolicyNumber = strings.TrimSpace(v.PolicyNumber)
	v.HomeMarina = strings.TrimSpace(v.HomeMarina)
	v.Berth = strings.TrimSpace(v.Berth)
	v.StormDelegate = strings.TrimSpace(v.StormDelegate)
	return v
}

// The years a vessel record accepts.
const (
	vesselYearMin = 1800
	vesselYearMax = 2200
)

func validateParticulars(v vesselParticulars) *vesselParticularsError {
	if v.Year != nil && (*v.Year < vesselYearMin || *v.Year > vesselYearMax) {
		return &vesselParticularsError{Field: "year", Message: "year must be between 1800 and 2200"}
	}
	if v.DisplacementKG != nil && *v.DisplacementKG < 0 {
		return &vesselParticularsError{Field: "displacement_kg", Message: "displacement cannot be negative"}
	}
	if v.LOAM != nil && *v.LOAM < 0 {
		return &vesselParticularsError{Field: "loa_m", Message: "length overall cannot be negative"}
	}
	if v.BeamM != nil && *v.BeamM < 0 {
		return &vesselParticularsError{Field: "beam_m", Message: "beam cannot be negative"}
	}
	if v.DateAcquired != "" && !installDatePattern.MatchString(v.DateAcquired) {
		return &vesselParticularsError{Field: "date_acquired", Message: "date_acquired must be blank or YYYY-MM-DD"}
	}
	return nil
}

const vesselParticularsColumns = `builder, model, year, hin, flag, hailing_port, hull_type, hull_material,
	displacement_kg, shore_power, system_voltage, registration, imo, epirb_id, date_acquired,
	loa_m, beam_m, owner_name, owner_phone, owner_email, insurer, policy_number, home_marina, berth, storm_delegate, updated_at`

func scanVesselParticulars(row rowScanner) (vesselParticulars, error) {
	var v vesselParticulars
	var year sql.NullInt64
	var kg, loa, beam sql.NullFloat64
	var updated int64
	if err := row.Scan(&v.Builder, &v.Model, &year, &v.HIN, &v.Flag, &v.HailingPort, &v.HullType, &v.HullMaterial,
		&kg, &v.ShorePower, &v.SystemVoltage, &v.Registration, &v.IMO, &v.EPIRBID, &v.DateAcquired,
		&loa, &beam, &v.OwnerName, &v.OwnerPhone, &v.OwnerEmail, &v.Insurer, &v.PolicyNumber, &v.HomeMarina, &v.Berth, &v.StormDelegate,
		&updated); err != nil {
		return vesselParticulars{}, err
	}
	if year.Valid {
		y := int(year.Int64)
		v.Year = &y
	}
	if kg.Valid {
		k := kg.Float64
		v.DisplacementKG = &k
	}
	if loa.Valid {
		l := loa.Float64
		v.LOAM = &l
	}
	if beam.Valid {
		b := beam.Float64
		v.BeamM = &b
	}
	t := time.Unix(updated, 0).UTC()
	v.UpdatedAt = &t
	return v, nil
}

func vesselParticularsFrom(q sqlQueryer) (vesselParticulars, error) {
	v, err := scanVesselParticulars(q.QueryRow(`SELECT ` + vesselParticularsColumns + ` FROM vessel_particulars WHERE id = 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return vesselParticulars{}, nil
	}
	if err != nil {
		return vesselParticulars{}, fmt.Errorf("get vessel particulars: %w", err)
	}
	return v, nil
}

// GetVesselParticulars returns the record, or the empty one (UpdatedAt nil)
// when nothing has been saved yet. Never an error for "not set": a boat that
// has not filled this in is a normal state.
func (s *documentStore) GetVesselParticulars() (vesselParticulars, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return vesselParticularsFrom(s.db)
}

// SetVesselParticulars replaces the whole record.
func (s *documentStore) SetVesselParticulars(v vesselParticulars) (vesselParticulars, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v = trimParticulars(v)
	if verr := validateParticulars(v); verr != nil {
		return vesselParticulars{}, verr
	}

	tx, err := s.db.Begin()
	if err != nil {
		return vesselParticulars{}, fmt.Errorf("set vessel particulars: begin: %w", err)
	}
	defer tx.Rollback()

	if err := upsertVesselParticularsTx(tx, v, s.now()); err != nil {
		return vesselParticulars{}, err
	}
	saved, err := vesselParticularsFrom(tx)
	if err != nil {
		return vesselParticulars{}, err
	}
	if err := tx.Commit(); err != nil {
		return vesselParticulars{}, fmt.Errorf("set vessel particulars: commit: %w", err)
	}
	return saved, nil
}

// upsertVesselParticularsTx writes the single row inside tx; the import commit
// uses it too so particulars land in the same transaction as everything else.
func upsertVesselParticularsTx(tx *sql.Tx, v vesselParticulars, now time.Time) error {
	var year, kg, loa, beam any
	if v.LOAM != nil {
		loa = *v.LOAM
	}
	if v.BeamM != nil {
		beam = *v.BeamM
	}
	if v.Year != nil {
		year = *v.Year
	}
	if v.DisplacementKG != nil {
		kg = *v.DisplacementKG
	}
	if _, err := tx.Exec(`
		INSERT INTO vessel_particulars (id, builder, model, year, hin, flag, hailing_port, hull_type, hull_material,
			displacement_kg, shore_power, system_voltage, registration, imo, epirb_id, date_acquired,
			loa_m, beam_m, owner_name, owner_phone, owner_email, insurer, policy_number, home_marina, berth, storm_delegate, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			builder = excluded.builder, model = excluded.model, year = excluded.year, hin = excluded.hin,
			flag = excluded.flag, hailing_port = excluded.hailing_port, hull_type = excluded.hull_type,
			hull_material = excluded.hull_material, displacement_kg = excluded.displacement_kg,
			shore_power = excluded.shore_power, system_voltage = excluded.system_voltage,
			registration = excluded.registration, imo = excluded.imo, epirb_id = excluded.epirb_id,
			date_acquired = excluded.date_acquired, loa_m = excluded.loa_m, beam_m = excluded.beam_m,
			owner_name = excluded.owner_name, owner_phone = excluded.owner_phone, owner_email = excluded.owner_email,
			insurer = excluded.insurer, policy_number = excluded.policy_number, home_marina = excluded.home_marina,
			berth = excluded.berth, storm_delegate = excluded.storm_delegate, updated_at = excluded.updated_at`,
		v.Builder, v.Model, year, v.HIN, v.Flag, v.HailingPort, v.HullType, v.HullMaterial,
		kg, v.ShorePower, v.SystemVoltage, v.Registration, v.IMO, v.EPIRBID, v.DateAcquired,
		loa, beam, v.OwnerName, v.OwnerPhone, v.OwnerEmail, v.Insurer, v.PolicyNumber, v.HomeMarina, v.Berth, v.StormDelegate, now.Unix(),
	); err != nil {
		return fmt.Errorf("set vessel particulars: %w", err)
	}
	return nil
}

// getVesselParticularsHandler is GET /api/vessel/particulars.
func getVesselParticularsHandler(c echo.Context) error {
	v, err := globalDocumentStore.GetVesselParticulars()
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, v)
}

// putVesselParticularsHandler is PUT /api/vessel/particulars: a whole-record
// replace, like the equipment PUT. A bad field is a 400 {field, message}.
func putVesselParticularsHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	var req vesselParticulars
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	saved, err := globalDocumentStore.SetVesselParticulars(req)
	var verr *vesselParticularsError
	if errors.As(err, &verr) {
		return c.JSON(http.StatusBadRequest, map[string]string{"field": verr.Field, "message": verr.Message})
	}
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, saved)
}
