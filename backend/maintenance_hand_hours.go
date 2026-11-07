package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// Hand-read hours (ADR 0268). Gear with no SignalK hour meter (a watermaker,
// say) has nothing for the maintenance engine to read, so its hours-based
// rules read "Hours unknown" for ever. The operator can read the gauge and
// type the figure in. An item with no meter takes its current hours from the
// latest-dated figure typed for it: a reading logged here, the hours on a
// Complete form or standalone service log entry, or the hours on Set last
// done.
//
// The fallback policy bounds this. A hand figure never stands in for a meter
// that exists: recording one on an item with a source is refused, and
// equipmentHourReading never looks at hand figures for such an item. The
// reading it produces says it was read by hand (maintenanceHourReading.ByHand)
// and on which date, so no view or Mate answer presents it as live.

// handHourReading is one row of hand_hour_readings: a gauge figure the
// operator typed for an item with no hour meter. Gauge is what was typed;
// Hours is the same figure as true hours, converted with the meter-reset
// offset in force on ReadOn (gaugeToTrueHours) so a later meter replacement
// leaves it correct.
type handHourReading struct {
	ID          string    `json:"id"`
	EquipmentID string    `json:"equipment_id"`
	ReadOn      string    `json:"read_on"`
	Gauge       float64   `json:"gauge"`
	Hours       float64   `json:"hours"`
	Note        string    `json:"note"`
	CreatedAt   time.Time `json:"created_at"`
}

// handHourReadingRequest is the body of POST .../hour-readings. Gauge is a
// pointer so a missing figure is refused instead of recorded as 0.
type handHourReadingRequest struct {
	ReadOn string   `json:"read_on"`
	Gauge  *float64 `json:"gauge"`
	Note   string   `json:"note"`
}

const handHourNoteMaxRunes = 500

// handHourSample is one dated true-hours figure the operator typed, from any
// of the places they can type one.
type handHourSample struct {
	ReadOn string
	Hours  float64
}

// handEquipmentHours is the hand-read counterpart of currentEquipmentHours:
// the latest-dated sample is the item's current hours. On the same date the
// larger figure wins, since hours only run forward. Gauge is true hours less
// the offset in force now, so it is what the gauge shows today. Known is false
// with no samples at all; nothing is guessed.
func handEquipmentHours(samples []handHourSample, resets []hourMeterReset, now time.Time) maintenanceHourReading {
	// A figure dated more than a day ahead (a service entry typed with the
	// wrong date) is not a reading yet. The day of slack covers an operator
	// whose date is ahead of the server's.
	horizon := now.AddDate(0, 0, 1).Format("2006-01-02")
	var best handHourSample
	found := false
	for _, s := range samples {
		if s.ReadOn > horizon {
			continue
		}
		if !found {
			best, found = s, true
			continue
		}
		if s.ReadOn > best.ReadOn || (s.ReadOn == best.ReadOn && s.Hours > best.Hours) {
			best = s
		}
	}
	if !found {
		return maintenanceHourReading{}
	}
	return maintenanceHourReading{
		Known:  true,
		Hours:  best.Hours,
		Gauge:  best.Hours - offsetInForceAt(resets, now),
		ByHand: true,
		ReadOn: best.ReadOn,
	}
}

// ── store ────────────────────────────────────────────────────────────────

const handHourReadingColumns = `id, equipment_id, read_on, gauge, hours, note, created_at`

func scanHandHourReading(row rowScanner) (handHourReading, error) {
	var r handHourReading
	var createdAt int64
	if err := row.Scan(&r.ID, &r.EquipmentID, &r.ReadOn, &r.Gauge, &r.Hours, &r.Note, &createdAt); err != nil {
		return handHourReading{}, err
	}
	r.CreatedAt = time.Unix(createdAt, 0).UTC()
	return r, nil
}

func insertHandHourReadingTx(tx *sql.Tx, now time.Time, equipmentID, readOn string, gauge, hours float64, note string) (handHourReading, error) {
	r := handHourReading{ID: uuid.NewString(), EquipmentID: equipmentID, ReadOn: readOn, Gauge: gauge, Hours: hours, Note: note, CreatedAt: now.UTC()}
	if _, err := tx.Exec(`INSERT INTO hand_hour_readings (`+handHourReadingColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.EquipmentID, r.ReadOn, r.Gauge, r.Hours, r.Note, now.Unix()); err != nil {
		return handHourReading{}, fmt.Errorf("record hand hour reading"+": %w", err)
	}
	return r, nil
}

// ListHandHourReadings returns equipmentID's kept readings, latest date first.
func (s *documentStore) ListHandHourReadings(equipmentID string) ([]handHourReading, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT `+handHourReadingColumns+` FROM hand_hour_readings WHERE equipment_id = ? ORDER BY read_on DESC, created_at DESC, id DESC`, equipmentID)
	if err != nil {
		return nil, fmt.Errorf("list hand hour readings"+": %w", err)
	}
	defer rows.Close()
	out := []handHourReading{}
	for rows.Next() {
		r, err := scanHandHourReading(rows)
		if err != nil {
			return nil, fmt.Errorf("list hand hour readings: scan"+": %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list hand hour readings: %w", err)
	}
	return out, nil
}

// HandHourSamples returns every figure the operator typed for equipmentID,
// with its meter-reset history, under one lock: the kept readings plus the
// hours on the item's service log entries (a Complete form writes one).
func (s *documentStore) HandHourSamples(equipmentID string) ([]handHourSample, []hourMeterReset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`
		SELECT read_on, hours FROM hand_hour_readings WHERE equipment_id = ?
		UNION ALL
		SELECT performed_at, hours FROM maintenance_log_entries WHERE equipment_id = ? AND hours IS NOT NULL`,
		equipmentID, equipmentID)
	if err != nil {
		return nil, nil, fmt.Errorf("hand hour samples"+": %w", err)
	}
	defer rows.Close()
	var samples []handHourSample
	for rows.Next() {
		var smp handHourSample
		if err := rows.Scan(&smp.ReadOn, &smp.Hours); err != nil {
			return nil, nil, fmt.Errorf("hand hour samples: scan"+": %w", err)
		}
		samples = append(samples, smp)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("hand hour samples: %w", err)
	}
	if len(samples) == 0 {
		return nil, nil, nil // nothing typed: the reset history is not needed
	}
	resets, err := hourMeterResetsFor(s.db, equipmentID)
	if err != nil {
		return nil, nil, err
	}
	return samples, resets, nil
}

// DeleteHandHourReading removes one kept reading of equipmentID.
func (s *documentStore) DeleteHandHourReading(equipmentID, readingID string) error {
	return s.RunTx("delete hand hour reading", func(tx *sql.Tx, now time.Time) error {
		res, err := tx.Exec(`DELETE FROM hand_hour_readings WHERE id = ? AND equipment_id = ?`, readingID, equipmentID)
		if err != nil {
			return fmt.Errorf("delete hand hour reading"+": %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return &maintenanceCommandError{Status: http.StatusNotFound, Message: "reading not found"}
		}
		return nil
	})
}

// ── command ──────────────────────────────────────────────────────────────

// itemHasHourMeterTx reports whether equipmentID has a SignalK hour meter,
// failing with errEquipmentNotFound for an item that does not exist.
func itemHasHourMeterTx(tx *sql.Tx, equipmentID string) (bool, error) {
	var source string
	err := tx.QueryRow(`SELECT COALESCE(signalk_source, '') FROM equipment WHERE id = ?`, equipmentID).Scan(&source)
	if err == sql.ErrNoRows {
		return false, errEquipmentNotFound
	}
	if err != nil {
		return false, fmt.Errorf("check hour meter"+": %w", err)
	}
	return sourceHourMeterPath(source) != "", nil
}

// cmdRecordHandHourReading keeps one hand reading. It refuses an item that has
// an hour meter (the fallback policy: a hand figure must never stand in for a
// meter that has gone quiet) and a date after the operator's today.
func cmdRecordHandHourReading(tx *sql.Tx, now, today time.Time, equipmentID string, req handHourReadingRequest) (handHourReading, error) {
	readOn := strings.TrimSpace(req.ReadOn)
	if !installDatePattern.MatchString(readOn) {
		return handHourReading{}, &inventoryValidationError{Field: "read_on", Message: "read_on is required and must be YYYY-MM-DD"}
	}
	if readOn > today.Format("2006-01-02") {
		return handHourReading{}, &inventoryValidationError{Field: "read_on", Message: "read_on cannot be after today"}
	}
	if req.Gauge == nil || *req.Gauge < 0 {
		return handHourReading{}, &inventoryValidationError{Field: "gauge", Message: "gauge is required and cannot be negative"}
	}
	note := strings.TrimSpace(req.Note)
	if utf8.RuneCountInString(note) > handHourNoteMaxRunes {
		return handHourReading{}, &inventoryValidationError{Field: "note", Message: "note is too long"}
	}
	hasMeter, err := itemHasHourMeterTx(tx, equipmentID)
	if err != nil {
		return handHourReading{}, err
	}
	if hasMeter {
		return handHourReading{}, &maintenanceCommandError{
			Status:  http.StatusConflict,
			Message: "this item has an hour meter, so its hours come from the meter; a hand reading cannot replace it",
		}
	}
	at, err := time.Parse("2006-01-02", readOn)
	if err != nil {
		return handHourReading{}, err
	}
	hours, err := convertGaugeHoursToTrueTx(tx, equipmentID, *req.Gauge, at)
	if err != nil {
		return handHourReading{}, err
	}
	return insertHandHourReadingTx(tx, now, equipmentID, readOn, *req.Gauge, hours, note)
}

func (s *documentStore) RecordHandHourReadingCmd(today time.Time, equipmentID string, req handHourReadingRequest) (handHourReading, error) {
	return runCmd(s, "record hand hour reading", func(tx *sql.Tx, now time.Time) (handHourReading, error) {
		return cmdRecordHandHourReading(tx, now, today, equipmentID, req)
	})
}

// ── handlers ─────────────────────────────────────────────────────────────

// recordHandHourReadingHandler is POST
// /api/inventory/equipment/:id/maintenance/hour-readings?today=: {read_on,
// gauge, note?}.
func recordHandHourReadingHandler(c echo.Context) error {
	today, verr := requireTodayParam(c)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	limitNoteRequestBody(c)
	var req handHourReadingRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	reading, err := globalDocumentStore.RecordHandHourReadingCmd(today, c.Param("id"), req)
	if err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	return c.JSON(http.StatusCreated, map[string]any{"reading": reading})
}

// listHandHourReadingsHandler is GET
// /api/inventory/equipment/:id/maintenance/hour-readings.
func listHandHourReadingsHandler(c echo.Context) error {
	readings, err := globalDocumentStore.ListHandHourReadings(c.Param("id"))
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"readings": readings})
}

// deleteHandHourReadingHandler is DELETE
// /api/inventory/equipment/:id/maintenance/hour-readings/:readingId.
func deleteHandHourReadingHandler(c echo.Context) error {
	if err := globalDocumentStore.DeleteHandHourReading(c.Param("id"), c.Param("readingId")); err != nil {
		return writeMaintenanceCommandError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// recordLastDoneHandReadingTx keeps the hours typed on Set last done as a
// reading dated that day (today when only hours were given), when the item has
// no hour meter. An item with a meter keeps nothing: its hours are the meter's.
func recordLastDoneHandReadingTx(tx *sql.Tx, now, today time.Time, equipmentID string, lastDoneAt *string, gauge, hours float64) error {
	hasMeter, err := itemHasHourMeterTx(tx, equipmentID)
	if err != nil || hasMeter {
		return err
	}
	readOn := today.Format("2006-01-02")
	if lastDoneAt != nil && *lastDoneAt != "" {
		readOn = *lastDoneAt
	}
	if readOn > today.Format("2006-01-02") {
		return nil // a date still to come is not a reading
	}
	_, err = insertHandHourReadingTx(tx, now, equipmentID, readOn, gauge, hours, "Set last done")
	return err
}
