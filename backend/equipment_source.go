package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// equipment_source.go is the write-side rule for an item's SignalK source
// (ADR 0187): the source is one of the eight shapes in vessel_sources.go, no
// two items own the same one, and the bank figures sit only on a battery.

var (
	// errEquipmentSourceInvalid wraps the reason a source or a bank figure is
	// refused.
	errEquipmentSourceInvalid = errors.New("invalid SignalK source")
)

// equipmentSourceTakenError says which item already owns a source, so the
// operator can go and free it rather than guess.
type equipmentSourceTakenError struct {
	Source   string
	ItemID   string
	ItemName string
}

func (e *equipmentSourceTakenError) Error() string {
	return fmt.Sprintf("%s is already assigned to %q; unassign it there first", e.Source, e.ItemName)
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
