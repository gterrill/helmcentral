package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Stored proposals in ADR 0146's shape are converted to changesets when the
// table is upgraded (ADR 0158).

func (e *proposalEnv) insertLegacy(t *testing.T, id, ops, result, status string) {
	t.Helper()
	msg := e.saveOnMessage(t, "old card")
	if _, err := e.asst.db.Exec(
		`INSERT INTO assistant_message_proposals (id, message_id, position, ops, status, result, created_at) VALUES (?, ?, 0, ?, ?, ?, ?)`,
		id, msg.ID, ops, status, result, e.clock.Unix()); err != nil {
		t.Fatalf("insert legacy proposal: %v", err)
	}
}

func TestLegacyProposals_EveryOldOperationBecomesAChangesetOperation(t *testing.T) {
	env := newProposalEnv(t)
	env.insertLegacy(t, "old-1", `[
		{"op":"create_rule","equipment_id":"e1","description":"Belts","interval_months":12,"last_done_at":"2025-01-09","last_done_meter_reading":239,"summary":"Add Belts"},
		{"op":"update_rule","rule_id":"r1","interval_hours":300,"clear":["interval_months"],"summary":"Change Oil","rule_updated_at":"2026-09-01T00:00:00Z"},
		{"op":"set_last_done","rule_id":"r1","last_done_at":"2025-01-09","summary":"Oil: last done","rule_updated_at":"2026-09-01T00:00:00Z"},
		{"op":"complete_rule","rule_id":"r1","performed_at":"2026-09-01","meter_reading":45,"who":"Gavin","cost":120,"summary":"Log Oil","rule_updated_at":"2026-09-01T00:00:00Z"},
		{"op":"acknowledge","rule_id":"r1","reason":"parts","summary":"Acknowledge Oil","rule_updated_at":"2026-09-01T00:00:00Z"}]`, "", "pending")
	if err := convertLegacyProposals(env.asst.db); err != nil {
		t.Fatalf("convert: %v", err)
	}
	p, err := env.asst.GetProposal("old-1")
	if err != nil {
		t.Fatalf("GetProposal: %v", err)
	}
	if len(p.Ops) != 5 {
		t.Fatalf("expected 5 operations, got %+v", p.Ops)
	}
	create, update, last, complete, ack := p.Ops[0], p.Ops[1], p.Ops[2], p.Ops[3], p.Ops[4]
	if create.Type != recordTypeMaintenanceRule || create.Action != changeCreate || create.Fields["description"] != "Belts" ||
		create.Fields["interval_months"] != 12.0 || create.Fields["last_done_meter_reading"] != 239.0 || create.Description != "Add Belts" {
		t.Errorf("create_rule: %+v", create)
	}
	if update.Action != changeUpdate || update.ID != "r1" || update.BaseVersion != "2026-09-01T00:00:00Z" ||
		update.Fields["interval_hours"] != 300.0 || update.Fields["interval_months"] != nil {
		t.Errorf("update_rule: %+v", update)
	}
	if _, has := update.Fields["interval_months"]; !has {
		t.Errorf("clear must become a null field: %+v", update.Fields)
	}
	if last.Action != changeUpdate || last.Fields["last_done_at"] != "2025-01-09" || last.ID != "r1" {
		t.Errorf("set_last_done: %+v", last)
	}
	if complete.Type != recordTypeMaintenanceLog || complete.Action != changeCreate || complete.Fields["rule_id"] != "r1" ||
		complete.Fields["meter_reading"] != 45.0 || complete.Fields["who"] != "Gavin" || len(complete.Watches) != 1 || complete.Watches[0].ID != "r1" {
		t.Errorf("complete_rule: %+v", complete)
	}
	if ack.Action != changeUpdate || ack.Fields["acknowledged_reason"] != "parts" {
		t.Errorf("acknowledge: %+v", ack)
	}
}

func TestLegacyProposals_AppliedResultsAndStatusesSurvive(t *testing.T) {
	env := newProposalEnv(t)
	env.insertLegacy(t, "old-applied", `[{"op":"create_rule","description":"Belts","interval_months":12,"summary":"Add Belts"},{"op":"complete_rule","rule_id":"r1","performed_at":"2026-09-01","summary":"Log"}]`,
		`{"ops":[{"op":"create_rule","rule_ids":["rule-new"]},{"op":"complete_rule","rule_ids":["r1"],"entry_id":"entry-1"}]}`, "applied")
	env.insertLegacy(t, "old-dismissed", `[{"op":"create_rule","description":"X","interval_months":12,"summary":"Add X"}]`, "", "dismissed")
	if err := convertLegacyProposals(env.asst.db); err != nil {
		t.Fatalf("convert: %v", err)
	}
	applied, _ := env.asst.GetProposal("old-applied")
	var res changeResult
	if err := json.Unmarshal(applied.Result, &res); err != nil {
		t.Fatalf("result: %v (%s)", err, applied.Result)
	}
	if applied.Status != assistantProposalApplied || len(res.Ops) != 2 ||
		res.Ops[0] != (changeOpResult{Type: recordTypeMaintenanceRule, Action: changeCreate, ID: "rule-new"}) ||
		res.Ops[1] != (changeOpResult{Type: recordTypeMaintenanceLog, Action: changeCreate, ID: "entry-1"}) {
		t.Fatalf("unexpected applied proposal %+v result %+v", applied, res)
	}
	if d, _ := env.asst.GetProposal("old-dismissed"); d.Status != assistantProposalDismissed {
		t.Fatalf("status must survive, got %q", d.Status)
	}
}

func TestLegacyProposals_ConversionIsIdempotentAndLeavesNewRowsAlone(t *testing.T) {
	env := newProposalEnv(t)
	fresh := env.propose(t, `{"operations":[{"type":"maintenance_rule","action":"create","fields":{"description":"Registration renewal","interval_months":12}}]}`)
	env.insertLegacy(t, "old-1", `[{"op":"create_rule","description":"Belts","interval_months":12,"summary":"Add Belts"}]`, "", "pending")
	for i := 0; i < 2; i++ {
		if err := convertLegacyProposals(env.asst.db); err != nil {
			t.Fatalf("convert %d: %v", i, err)
		}
	}
	got, _ := env.asst.GetProposal(fresh.ID)
	if len(got.Ops) != 1 || got.Ops[0].Description != fresh.Ops[0].Description || got.Ops[0].BaseVersion != fresh.Ops[0].BaseVersion {
		t.Fatalf("a row already in the new shape must not change: %+v vs %+v", got.Ops, fresh.Ops)
	}
}

func TestLegacyProposals_APendingConvertedProposalAppliesAndAStaleOneIsRefused(t *testing.T) {
	env := newProposalEnv(t)
	gen := env.equipment(t, equipmentItem{Name: "Generator", System: "electrical"})
	rule := mustToolRule(t, env.docs, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Oil", IntervalMonths: iptr(12)})
	snapshot := rule.UpdatedAt.UTC().Format(time.RFC3339)
	env.insertLegacy(t, "old-ok", fmt.Sprintf(`[{"op":"acknowledge","rule_id":%q,"reason":"later","summary":"Acknowledge","rule_updated_at":%q}]`, rule.ID, snapshot), "", "pending")
	env.insertLegacy(t, "old-stale", fmt.Sprintf(`[{"op":"acknowledge","rule_id":%q,"reason":"later","summary":"Acknowledge","rule_updated_at":"2020-01-01T00:00:00Z"}]`, rule.ID), "", "pending")
	env.insertLegacy(t, "old-copy", `[{"op":"copy_profile_schedule","equipment_id":"x","summary":"Copy the profile schedule"}]`, "", "pending")
	if err := convertLegacyProposals(env.asst.db); err != nil {
		t.Fatalf("convert: %v", err)
	}
	env.advance()

	if _, err := env.apply("old-stale"); !errors.Is(err, errAssistantProposalStale) {
		t.Fatalf("a snapshot that no longer matches is stale, got %v", err)
	}
	_, err := env.apply("old-copy")
	if !errors.Is(err, errAssistantProposalStale) || !strings.Contains(err.Error(), "no longer makes") || !strings.Contains(err.Error(), "Copy the profile schedule") {
		t.Fatalf("a card for a removed operation is stale with a reason, got %v", err)
	}
	if _, err := env.apply("old-ok"); err != nil {
		t.Fatalf("a converted pending proposal applies: %v", err)
	}
	row, _ := env.docs.GetMaintenanceRuleRow(rule.ID)
	if !row.Acknowledged || row.AckReason != "later" {
		t.Fatalf("expected the rule acknowledged, got %+v", row)
	}
}
