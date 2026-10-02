package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// Stored proposals written before ADR 0158 hold ADR 0146's maintenance
// operations ({op: "create_rule", rule_id, ...}). The table is upgraded to the
// changeset shape the first time the assistant store opens: each such row is
// converted in place, and a row already in the new shape is left alone, so
// the conversion is safe to run on every start. With one operator there is no
// reading of the old shape beyond this.

type legacyProposalOp struct {
	Op                   string   `json:"op"`
	EquipmentID          string   `json:"equipment_id"`
	RuleID               string   `json:"rule_id"`
	Description          string   `json:"description"`
	IntervalHours        *float64 `json:"interval_hours"`
	IntervalMonths       *int     `json:"interval_months"`
	DueSoonHours         *float64 `json:"due_soon_hours"`
	DueSoonMonths        *int     `json:"due_soon_months"`
	FixedDueDate         string   `json:"fixed_due_date"`
	Clear                []string `json:"clear"`
	NotApplicable        *bool    `json:"not_applicable"`
	LastDoneAt           string   `json:"last_done_at"`
	LastDoneMeterReading *float64 `json:"last_done_meter_reading"`
	PerformedAt          string   `json:"performed_at"`
	MeterReading         *float64 `json:"meter_reading"`
	Who                  string   `json:"who"`
	Cost                 *float64 `json:"cost"`
	NewDueDate           string   `json:"new_due_date"`
	Reason               string   `json:"reason"`
	Summary              string   `json:"summary"`
	RuleUpdatedAt        string   `json:"rule_updated_at"`
}

type legacyProposalOpResult struct {
	Op      string   `json:"op"`
	RuleIDs []string `json:"rule_ids"`
	EntryID string   `json:"entry_id"`
}

func (o legacyProposalOp) groupFields() map[string]any {
	f := map[string]any{}
	set := func(name string, v any, ok bool) {
		if ok {
			f[name] = v
		}
	}
	set("equipment_id", o.EquipmentID, o.EquipmentID != "")
	set("description", o.Description, o.Description != "")
	if o.IntervalHours != nil {
		f["interval_hours"] = *o.IntervalHours
	}
	if o.IntervalMonths != nil {
		f["interval_months"] = float64(*o.IntervalMonths)
	}
	if o.DueSoonHours != nil {
		f["due_soon_hours"] = *o.DueSoonHours
	}
	if o.DueSoonMonths != nil {
		f["due_soon_months"] = float64(*o.DueSoonMonths)
	}
	set("fixed_due_date", o.FixedDueDate, o.FixedDueDate != "")
	for _, name := range o.Clear {
		f[name] = nil
	}
	if o.NotApplicable != nil {
		f["not_applicable"] = *o.NotApplicable
	}
	set("last_done_at", o.LastDoneAt, o.LastDoneAt != "")
	if o.LastDoneMeterReading != nil {
		f["last_done_meter_reading"] = *o.LastDoneMeterReading
	}
	return f
}

// convertLegacyOp is one ADR 0146 operation as a changeset operation.
func convertLegacyOp(o legacyProposalOp) changeOp {
	op := changeOp{Type: recordTypeMaintenanceRule, Description: o.Summary}
	switch o.Op {
	case "create_rule":
		op.Action = changeCreate
		op.Fields = o.groupFields()
	case "update_rule":
		op.Action, op.ID, op.BaseVersion = changeUpdate, o.RuleID, o.RuleUpdatedAt
		op.Fields = o.groupFields()
	case "set_last_done":
		op.Action, op.ID, op.BaseVersion = changeUpdate, o.RuleID, o.RuleUpdatedAt
		op.Fields = o.groupFields()
	case "acknowledge":
		op.Action, op.ID, op.BaseVersion = changeUpdate, o.RuleID, o.RuleUpdatedAt
		op.Fields = map[string]any{"acknowledged_reason": o.Reason}
	case "complete_rule":
		op.Type, op.Action = recordTypeMaintenanceLog, changeCreate
		op.Fields = map[string]any{"rule_id": o.RuleID, "performed_at": o.PerformedAt}
		if o.MeterReading != nil {
			op.Fields["meter_reading"] = *o.MeterReading
		}
		if o.Description != "" {
			op.Fields["description"] = o.Description
		}
		if o.Who != "" {
			op.Fields["who"] = o.Who
		}
		if o.Cost != nil {
			op.Fields["cost"] = *o.Cost
		}
		if o.NewDueDate != "" {
			op.Fields["new_due_date"] = o.NewDueDate
		}
		if o.RuleUpdatedAt != "" {
			op.Watches = []recordWatch{{Type: recordTypeMaintenanceRule, ID: o.RuleID, Version: o.RuleUpdatedAt}}
		}
	default:
		// An operation the schedule no longer offers (copy_profile_schedule):
		// kept under its own name so Apply refuses it as stale with a reason.
		op.Action = o.Op
	}
	return op
}

func convertLegacyResult(raw string, ops []legacyProposalOp) (string, error) {
	if raw == "" {
		return "", nil
	}
	var old struct {
		Ops []legacyProposalOpResult `json:"ops"`
	}
	if err := json.Unmarshal([]byte(raw), &old); err != nil {
		return "", fmt.Errorf("decode result: %w", err)
	}
	out := changeResult{Ops: make([]changeOpResult, 0, len(old.Ops))}
	for i, r := range old.Ops {
		var conv changeOp
		if i < len(ops) {
			conv = convertLegacyOp(ops[i])
		}
		res := changeOpResult{Type: conv.Type, Action: conv.Action}
		if conv.Type != "" && (conv.Action == changeCreate || conv.Action == changeUpdate) {
			// The Maintenance list is where every rule and log entry shows.
			res.Href = "/inventory/maintenance"
		}
		switch {
		case r.EntryID != "":
			res.ID = r.EntryID
		case len(r.RuleIDs) > 0:
			res.ID = r.RuleIDs[0]
		}
		out.Ops = append(out.Ops, res)
	}
	b, err := json.Marshal(out)
	return string(b), err
}

// convertLegacyProposals upgrades every stored proposal still in ADR 0146's
// shape, in one transaction.
func convertLegacyProposals(db *sql.DB) error {
	rows, err := db.Query(`SELECT id, ops, result FROM assistant_message_proposals`)
	if err != nil {
		return fmt.Errorf("read proposals to convert: %w", err)
	}
	type row struct{ id, ops, result string }
	var legacy []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.ops, &r.result); err != nil {
			rows.Close()
			return fmt.Errorf("scan proposal to convert: %w", err)
		}
		var probe []map[string]json.RawMessage
		if err := json.Unmarshal([]byte(r.ops), &probe); err != nil {
			rows.Close()
			return fmt.Errorf("decode proposal %s ops: %w", r.id, err)
		}
		if len(probe) > 0 {
			if _, isOld := probe[0]["op"]; isOld {
				legacy = append(legacy, r)
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read proposals to convert: %w", err)
	}
	if len(legacy) == 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin proposal conversion: %w", err)
	}
	defer tx.Rollback()
	for _, r := range legacy {
		var old []legacyProposalOp
		if err := json.Unmarshal([]byte(r.ops), &old); err != nil {
			return fmt.Errorf("decode proposal %s ops: %w", r.id, err)
		}
		conv := make([]changeOp, len(old))
		for i, o := range old {
			conv[i] = convertLegacyOp(o)
		}
		opsJSON, err := json.Marshal(conv)
		if err != nil {
			return fmt.Errorf("encode proposal %s ops: %w", r.id, err)
		}
		result, err := convertLegacyResult(r.result, old)
		if err != nil {
			return fmt.Errorf("convert proposal %s: %w", r.id, err)
		}
		if _, err := tx.Exec(`UPDATE assistant_message_proposals SET ops = ?, result = ? WHERE id = ?`, string(opsJSON), result, r.id); err != nil {
			return fmt.Errorf("update proposal %s: %w", r.id, err)
		}
	}
	return tx.Commit()
}
