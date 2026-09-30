package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// Profile guards (ADR 0148). An equipment item's maintenance schedule follows
// its profile live, so editing or deleting a profile reaches into every item
// that uses it. These checks run in the same transaction as the profile write
// (profileStore borrows the document store's connection), so a maintenance
// write cannot slip in between the check and the change.

// profileGuardError is a refusal that names who it would hurt. Handlers answer
// it with a 409 carrying Affected.
type profileGuardError struct {
	Message  string
	Affected []profileGuardEntry
}

func (e *profileGuardError) Error() string { return e.Message }

// profileGuardEntry is one affected equipment item, and (for a service that
// would be removed) the job on it that holds state.
type profileGuardEntry struct {
	EquipmentID   string `json:"equipment_id"`
	EquipmentName string `json:"equipment_name"`
	ServiceID     string `json:"service_id,omitempty"`
	Description   string `json:"description,omitempty"`
}

func writeProfileGuardError(c echo.Context, err *profileGuardError) error {
	return c.JSON(http.StatusConflict, map[string]any{"error": err.Message, "affected": err.Affected})
}

// itemsUsingProfileTx lists the equipment items whose profile_id is id.
func itemsUsingProfileTx(q sqlQueryer, id string) ([]profileGuardEntry, error) {
	rows, err := q.Query(`SELECT id, name FROM equipment WHERE profile_id = ? ORDER BY name, id`, id)
	if err != nil {
		return nil, fmt.Errorf("profile guard: list items: %w", err)
	}
	defer rows.Close()
	var out []profileGuardEntry
	for rows.Next() {
		var e profileGuardEntry
		if err := rows.Scan(&e.EquipmentID, &e.EquipmentName); err != nil {
			return nil, fmt.Errorf("profile guard: scan item: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// jobsWithStateTx lists job rows, on items that use profile id, for the given
// service ids.
func jobsWithStateTx(q sqlQueryer, profileID string, serviceIDs []string) ([]profileGuardEntry, error) {
	if len(serviceIDs) == 0 {
		return nil, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(serviceIDs)), ",")
	args := []any{profileID}
	for _, id := range serviceIDs {
		args = append(args, id)
	}
	rows, err := q.Query(`SELECT e.id, e.name, r.profile_service_id, r.description
		FROM maintenance_rules r JOIN equipment e ON e.id = r.equipment_id
		WHERE e.profile_id = ? AND r.profile_service_id IN (`+marks+`)
		ORDER BY e.name, e.id, r.profile_service_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("profile guard: list jobs: %w", err)
	}
	defer rows.Close()
	var out []profileGuardEntry
	for rows.Next() {
		var e profileGuardEntry
		if err := rows.Scan(&e.EquipmentID, &e.EquipmentName, &e.ServiceID, &e.Description); err != nil {
			return nil, fmt.Errorf("profile guard: scan job: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// guardProfileUpdateTx refuses an edit that removes service ids which items
// using the profile hold state for, unless confirmed. The removed ids are the
// stored profile's less the new one's; when the stored document cannot be
// decoded (an invalid profile being repaired) every service id that such an
// item holds state for and the new profile lacks counts, since the old list
// is unknowable.
func guardProfileUpdateTx(tx *sql.Tx, updated engineProfile, confirmRemoved bool) error {
	if confirmRemoved {
		return nil
	}
	keep := map[string]bool{}
	for _, svc := range updated.Service {
		keep[svc.ID] = true
	}
	var removed []string
	if old, _, err := getProfileTx(tx, updated.ID); err == nil {
		for _, svc := range old.Service {
			if !keep[svc.ID] {
				removed = append(removed, svc.ID)
			}
		}
	} else if err != errProfileNotFound {
		rows, qerr := tx.Query(`SELECT DISTINCT r.profile_service_id FROM maintenance_rules r JOIN equipment e ON e.id = r.equipment_id
			WHERE e.profile_id = ? AND r.profile_service_id != ''`, updated.ID)
		if qerr != nil {
			return fmt.Errorf("profile guard: %w", qerr)
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return fmt.Errorf("profile guard: %w", err)
			}
			if !keep[id] {
				removed = append(removed, id)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()
	}
	affected, err := jobsWithStateTx(tx, updated.ID, removed)
	if err != nil {
		return err
	}
	if len(affected) == 0 {
		return nil
	}
	return &profileGuardError{
		Message: fmt.Sprintf("this edit removes %d maintenance job(s) that equipment items already hold history or settings for. "+
			"The jobs would leave those items' schedules (their history is kept). Repeat the save with ?confirm_removed=1 to go ahead", len(affected)),
		Affected: affected,
	}
}

// guardProfileDeleteTx refuses deleting a profile any equipment item uses.
func guardProfileDeleteTx(tx *sql.Tx, id string) error {
	items, err := itemsUsingProfileTx(tx, id)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	return &profileGuardError{
		Message:  fmt.Sprintf("%d equipment item(s) still use this profile; change their profile first", len(items)),
		Affected: items,
	}
}
