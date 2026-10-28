package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// equipment_source.go is the write-side rule for an item's SignalK source
// (ADR 0187): the source is one of the eight shapes in vessel_sources.go, no
// two items own the same one, and the bank figures sit only on a battery.

var (
	// errEquipmentSourceInvalid wraps the reason a source or a bank figure is
	// refused.
	errEquipmentSourceInvalid = errors.New("invalid SignalK source")
	// errEquipmentSourceMoved is a swap or move asked for against an owner
	// that no longer holds the source, because someone changed it first.
	errEquipmentSourceMoved = errors.New("that source has moved to another item since this page loaded; reload and pick it again")
	// errEquipmentSourceChanged is a swap or move whose result is not the one
	// the page described, because the item's own source changed.
	errEquipmentSourceChanged = errors.New("this item's source changed since the page loaded; reload and pick it again")
)

// equipmentSourceTakenError says which item already owns a source, so the
// operator can go and free it rather than guess.
type equipmentSourceTakenError struct {
	Source   string
	ItemID   string
	ItemName string
}

func (e *equipmentSourceTakenError) Error() string {
	return fmt.Sprintf("%s is already on %q. To give it to another item, pick it on that item's Source list.", e.Source, e.ItemName)
}

// validateEquipmentSource normalises item's source and bank figures in place
// and checks them. selfID is the item being updated ("" on create), so an
// item keeps its own source across a save.
func validateEquipmentSource(q sqlQueryer, selfID string, item *equipmentItem) error {
	item.SignalKSource = strings.TrimSpace(item.SignalKSource)
	if err := validateSource(item.SignalKSource); err != nil {
		return fmt.Errorf("%w: %s", errEquipmentSourceInvalid, err.Error())
	}
	if item.CapacityAh < 0 || item.Cells < 0 || item.WarnVoltage < 0 || item.HighVoltage < 0 {
		return fmt.Errorf("%w: capacity, cell count and voltages cannot be negative", errEquipmentSourceInvalid)
	}
	group, _, _ := parseSource(item.SignalKSource)
	if group != sourceBattery && (item.CapacityAh != 0 || item.Cells != 0 || item.WarnVoltage != 0 || item.HighVoltage != 0) {
		return fmt.Errorf("%w: capacity, cell count and voltages belong to a battery item, and this one is not mapped to a battery", errEquipmentSourceInvalid)
	}
	if err := validateTankOverrides(item, group); err != nil {
		return fmt.Errorf("%w: %s", errEquipmentSourceInvalid, err.Error())
	}
	if item.SignalKSource == "" {
		return nil
	}
	var otherID, otherName string
	err := q.QueryRow(`SELECT id, name FROM equipment WHERE signalk_source = ? AND id <> ?`, item.SignalKSource, selfID).Scan(&otherID, &otherName)
	switch {
	case err == nil:
		return &equipmentSourceTakenError{Source: item.SignalKSource, ItemID: otherID, ItemName: otherName}
	case errors.Is(err, sql.ErrNoRows):
		return nil
	default:
		return fmt.Errorf("check source owner: %w", err)
	}
}

func isEquipmentSourceTaken(err error) bool {
	var taken *equipmentSourceTakenError
	return errors.As(err, &taken)
}

// UpdateEquipmentTakingSource is UpdateEquipment where item's source is one
// that fromID owns now, and the operator has said to take it. mode is what the
// page told the operator would happen, "swap" or "move"; the server works out
// which it is and refuses when they differ, so the operator never gets the
// other result than the one they read.
//
// In one transaction fromID gives the source up, id gets it, and in a swap
// fromID gets id's old source. A swap needs the old source to be the same kind
// of thing (a tank for a tank). A move leaves fromID with no source and so, as
// the server refuses figures on an item with none, without its bank or tank
// figures. In a swap each item keeps its own figures: they describe the
// physical item, not the source.
func (s *documentStore) UpdateEquipmentTakingSource(id string, item equipmentItem, fromID, mode string) (updated equipmentItem, err error) {
	err = s.RunTx("swap equipment source", func(tx *sql.Tx, now time.Time) error {
		updated, err = takeEquipmentSourceTx(tx, now, id, item, fromID, mode)
		return err
	})
	return updated, err
}

func takeEquipmentSourceTx(tx *sql.Tx, now time.Time, id string, item equipmentItem, fromID, mode string) (equipmentItem, error) {
	if mode != "swap" && mode != "move" {
		return equipmentItem{}, fmt.Errorf("%w: say whether to swap or move the source", errEquipmentSourceInvalid)
	}
	if fromID == id {
		return equipmentItem{}, fmt.Errorf("%w: an item cannot take a source from itself", errEquipmentSourceInvalid)
	}
	source := strings.TrimSpace(item.SignalKSource)
	if source == "" {
		return equipmentItem{}, fmt.Errorf("%w: choose the source to take", errEquipmentSourceInvalid)
	}
	var oldSource string
	if err := tx.QueryRow(`SELECT signalk_source FROM equipment WHERE id = ?`, id).Scan(&oldSource); errors.Is(err, sql.ErrNoRows) {
		return equipmentItem{}, errEquipmentNotFound
	} else if err != nil {
		return equipmentItem{}, fmt.Errorf("swap equipment source: read item: %w", err)
	}
	var ownerSource string
	if err := tx.QueryRow(`SELECT signalk_source FROM equipment WHERE id = ?`, fromID).Scan(&ownerSource); errors.Is(err, sql.ErrNoRows) {
		return equipmentItem{}, errEquipmentSourceMoved
	} else if err != nil {
		return equipmentItem{}, fmt.Errorf("swap equipment source: read owner: %w", err)
	}
	if ownerSource != source {
		return equipmentItem{}, errEquipmentSourceMoved
	}

	oldGroup, _, _ := parseSource(oldSource)
	newGroup, _, _ := parseSource(source)
	swap := oldSource != "" && oldGroup == newGroup
	if (mode == "swap") != swap {
		return equipmentItem{}, errEquipmentSourceChanged
	}

	// The owner lets go first: the unique index allows one holder at a time.
	release := `UPDATE equipment SET signalk_source = '', category = ?, updated_at = ? WHERE id = ?`
	if !swap {
		release = `UPDATE equipment SET signalk_source = '', category = ?, updated_at = ?, capacity_ah = 0, cells = 0, warn_voltage = 0, high_voltage = 0,
			tank_warn_percent = 0, tank_alarm_percent = 0, tank_alarms_off = 0 WHERE id = ?`
	}
	if _, err := tx.Exec(release, deriveEquipmentCategory(""), now.Unix(), fromID); err != nil {
		return equipmentItem{}, fmt.Errorf("swap equipment source: release: %w", err)
	}
	updated, err := updateEquipmentTx(tx, now, id, item)
	if err != nil {
		return equipmentItem{}, err
	}
	if swap {
		if _, err := tx.Exec(`UPDATE equipment SET signalk_source = ?, category = ? WHERE id = ?`,
			oldSource, deriveEquipmentCategory(oldSource), fromID); err != nil {
			return equipmentItem{}, fmt.Errorf("swap equipment source: hand over: %w", err)
		}
	}
	return updated, nil
}
