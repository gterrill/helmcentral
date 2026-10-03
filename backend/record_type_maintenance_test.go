package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// propose_changes (ADR 0146): the tool validates operations with
// the Maintenance handlers' own validators, renders operator-words summaries,
// snapshots the rules it will touch, and writes nothing.

func proposeDeps(t *testing.T) (assistantToolDeps, *documentStore) {
	t.Helper()
	deps, store := maintenanceToolDeps(t)
	withGlobalSnapshot(t, snapshotWithSelfValues(map[string]any{}))
	return deps, store
}

func runPropose(t *testing.T, deps assistantToolDeps, args string) assistantProposal {
	t.Helper()
	raw, err := deps.execute(context.Background(), "propose_changes", json.RawMessage(args))
	if err != nil {
		t.Fatalf("propose_changes: %v", err)
	}
	var result assistantProposeResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, raw)
	}
	return result.Proposal
}

// opsArgs is the propose_changes argument for a list of operations built in Go.
func opsArgs(ops ...changeOp) string {
	b, err := json.Marshal(assistantProposeArgs{Operations: ops})
	if err != nil {
		panic(err)
	}
	return string(b)
}

func proposeError(t *testing.T, deps assistantToolDeps, args string) string {
	t.Helper()
	_, err := deps.execute(context.Background(), "propose_changes", json.RawMessage(args))
	if err == nil {
		t.Fatalf("expected propose_changes to fail for %s", args)
	}
	return err.Error()
}

type maintenanceRowCounts struct{ rules, entries int }

func countMaintenanceRows(t *testing.T, store *documentStore) maintenanceRowCounts {
	t.Helper()
	rules, err := store.ListMaintenanceRuleRows(maintenanceRuleFilter{IncludeStored: true})
	if err != nil {
		t.Fatalf("ListMaintenanceRules: %v", err)
	}
	entries, err := store.ListMaintenanceLogEntries(maintenanceLogFilter{})
	if err != nil {
		t.Fatalf("ListMaintenanceLogEntries: %v", err)
	}
	return maintenanceRowCounts{len(rules), len(entries)}
}

// ── validators match the HTTP handlers (shared table cases) ─────────────

func fptr(v float64) *float64 { return &v }
func iptr(v int) *int         { return &v }

// ruleValidationCases feeds both the create handler and create_rule: the
// operator's forms and Mate's proposals are refused for the same reasons, and
// name the same field.
var ruleValidationCases = []struct {
	name           string
	withEquipment  bool
	description    string
	intervalHours  *float64
	intervalMonths *int
	dueSoonHours   *float64
	dueSoonMonths  *int
	fixedDueDate   string
	wantField      string
}{
	{"blank description", true, " ", nil, iptr(12), nil, nil, "", "description"},
	{"zero interval hours", true, "Oil", fptr(0), nil, nil, nil, "", "interval_hours"},
	{"interval hours without an item", false, "Oil", fptr(250), nil, nil, nil, "", "interval_hours"},
	{"zero interval months", true, "Oil", nil, iptr(0), nil, nil, "", "interval_months"},
	{"negative due-soon hours", true, "Oil", fptr(250), nil, fptr(-1), nil, "", "due_soon_hours"},
	{"negative due-soon months", true, "Oil", nil, iptr(12), nil, iptr(-1), "", "due_soon_months"},
	{"malformed fixed date", true, "Oil", nil, nil, nil, nil, "3 March", "fixed_due_date"},
	{"no interval at all", true, "Oil", nil, nil, nil, nil, "", "interval_hours"},
}

func TestRuleValidation_HandlerAndProposalRefuseTheSameCases(t *testing.T) {
	deps, store := proposeDeps(t)
	item := mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical"})

	for _, tc := range ruleValidationCases {
		t.Run(tc.name, func(t *testing.T) {
			var equipmentID *string
			if tc.withEquipment {
				equipmentID = &item.ID
			}
			body, err := json.Marshal(maintenanceRuleRequest{
				EquipmentID: equipmentID, Description: tc.description, IntervalHours: tc.intervalHours,
				IntervalMonths: tc.intervalMonths, DueSoonHours: tc.dueSoonHours, DueSoonMonths: tc.dueSoonMonths,
				FixedDueDate: tc.fixedDueDate,
			})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/maintenance/rules?today=2026-09-30", string(body), "")
			if err := createMaintenanceRuleHandler(c); err != nil {
				t.Fatalf("createMaintenanceRuleHandler: %v", err)
			}
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("handler: expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			var resp map[string]string
			_ = json.Unmarshal(rec.Body.Bytes(), &resp)
			if resp["field"] != tc.wantField {
				t.Fatalf("handler: expected field %q, got %v", tc.wantField, resp)
			}

			fields := map[string]any{"description": tc.description}
			if tc.intervalHours != nil {
				fields["interval_hours"] = *tc.intervalHours
			}
			if tc.intervalMonths != nil {
				fields["interval_months"] = *tc.intervalMonths
			}
			if tc.dueSoonHours != nil {
				fields["due_soon_hours"] = *tc.dueSoonHours
			}
			if tc.dueSoonMonths != nil {
				fields["due_soon_months"] = *tc.dueSoonMonths
			}
			if tc.fixedDueDate != "" {
				fields["fixed_due_date"] = tc.fixedDueDate
			}
			if tc.withEquipment {
				fields["equipment_id"] = item.ID
			}
			msg := proposeError(t, deps, opsArgs(changeOp{Type: recordTypeMaintenanceRule, Action: changeCreate, Fields: fields}))
			if !strings.Contains(msg, "ops[0] (maintenance_rule create): "+tc.wantField+":") {
				t.Fatalf("proposal: expected the error to name %q, got %q", tc.wantField, msg)
			}
		})
	}
}

func TestLastDoneValidation_HandlerAndProposalRefuseTheSameCases(t *testing.T) {
	deps, store := proposeDeps(t)
	item := mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical"})
	rule := mustToolRule(t, store, maintenanceRuleInput{EquipmentID: &item.ID, Description: "Oil", IntervalHours: fptr(250)})

	str := func(s string) *string { return &s }
	cases := []struct {
		name          string
		at            *string
		hours         *float64
		handlerField  string
		proposalField string
	}{
		{"nothing given", nil, nil, "last_done_at", "fields"},
		{"malformed date", str("9 Jan"), nil, "last_done_at", "last_done_at"},
		{"negative reading", nil, fptr(-5), "last_done_hours", "last_done_meter_reading"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(maintenanceLastDoneRequest{LastDoneAt: tc.at, LastDoneHours: tc.hours})
			c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/maintenance/rules/"+rule.ID+"/last-done?today=2026-09-30", string(body), rule.ID)
			if err := setMaintenanceRuleLastDoneHandler(c); err != nil {
				t.Fatalf("handler: %v", err)
			}
			var resp map[string]string
			_ = json.Unmarshal(rec.Body.Bytes(), &resp)
			if rec.Code != http.StatusBadRequest || resp["field"] != tc.handlerField {
				t.Fatalf("handler: expected 400 on %q, got %d %v", tc.handlerField, rec.Code, resp)
			}

			fields := map[string]any{}
			if tc.at != nil {
				fields["last_done_at"] = *tc.at
			}
			if tc.hours != nil {
				fields["last_done_meter_reading"] = *tc.hours
			}
			msg := proposeError(t, deps, opsArgs(changeOp{Type: recordTypeMaintenanceRule, Action: changeUpdate, ID: rule.ID, Fields: fields}))
			if !strings.Contains(msg, "ops[0] (maintenance_rule update): "+tc.proposalField+":") {
				t.Fatalf("proposal: expected the error to name %q, got %q", tc.proposalField, msg)
			}
		})
	}
}

func TestCompleteValidation_HandlerAndProposalRefuseTheSameCases(t *testing.T) {
	deps, store := proposeDeps(t)
	item := mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical"})
	hoursRule := mustToolRule(t, store, maintenanceRuleInput{EquipmentID: &item.ID, Description: "Oil", IntervalHours: fptr(250)})
	fixedRule := mustToolRule(t, store, maintenanceRuleInput{EquipmentID: &item.ID, Description: "Cert", FixedDueDate: "2026-12-01"})

	cases := []struct {
		name          string
		rule          maintenanceRule
		req           maintenanceLogEntryRequest
		handlerField  string
		proposalField string
	}{
		{"malformed date", hoursRule, maintenanceLogEntryRequest{PerformedAt: "yesterday", Hours: fptr(10)}, "performed_at", "performed_at"},
		{"negative reading", hoursRule, maintenanceLogEntryRequest{PerformedAt: "2026-09-01", Hours: fptr(-1)}, "hours", "meter_reading"},
		{"negative cost", hoursRule, maintenanceLogEntryRequest{PerformedAt: "2026-09-01", Hours: fptr(10), Cost: fptr(-3)}, "cost", "cost"},
		{"hours rule needs a reading", hoursRule, maintenanceLogEntryRequest{PerformedAt: "2026-09-01"}, "hours", "meter_reading"},
		{"fixed-date rule needs the next date", fixedRule, maintenanceLogEntryRequest{PerformedAt: "2026-09-01"}, "new_due_date", "new_due_date"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.req)
			c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/maintenance/rules/"+tc.rule.ID+"/complete?today=2026-09-30", string(body), tc.rule.ID)
			if err := completeMaintenanceRuleHandler(c); err != nil {
				t.Fatalf("handler: %v", err)
			}
			var resp map[string]string
			_ = json.Unmarshal(rec.Body.Bytes(), &resp)
			if rec.Code != http.StatusBadRequest || resp["field"] != tc.handlerField {
				t.Fatalf("handler: expected 400 on %q, got %d %v", tc.handlerField, rec.Code, resp)
			}

			fields := map[string]any{"rule_id": tc.rule.ID, "performed_at": tc.req.PerformedAt}
			if tc.req.Hours != nil {
				fields["meter_reading"] = *tc.req.Hours
			}
			if tc.req.Cost != nil {
				fields["cost"] = *tc.req.Cost
			}
			if tc.req.NewDueDate != "" {
				fields["new_due_date"] = tc.req.NewDueDate
			}
			msg := proposeError(t, deps, opsArgs(changeOp{Type: recordTypeMaintenanceLog, Action: changeCreate, Fields: fields}))
			if !strings.Contains(msg, "ops[0] (maintenance_log create): "+tc.proposalField+":") {
				t.Fatalf("proposal: expected the error to name %q, got %q", tc.proposalField, msg)
			}
		})
	}
}

// ── the tool writes nothing ─────────────────────────────────────────────

func TestProposeMaintenanceChanges_WritesNothing(t *testing.T) {
	deps, store := proposeDeps(t)
	oil := fptr(250)
	gen := mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical", ProfileID: "onan", HourMeterPath: "electrical.generator.0.runtime"})
	rule := mustToolRule(t, store, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Oil and filter", IntervalHours: oil})
	before := countMaintenanceRows(t, store)
	beforeRule, _ := store.GetMaintenanceRuleRow(rule.ID)

	args := fmt.Sprintf(`{"operations":[
		{"type":"maintenance_rule","action":"create","fields":{"equipment_id":%q,"description":"Belts","interval_months":12}},
		{"type":"maintenance_rule","action":"update","id":%q,"fields":{"interval_hours":300}},
		{"type":"maintenance_rule","action":"update","id":%q,"fields":{"last_done_at":"2025-01-09","last_done_meter_reading":239}},
		{"type":"maintenance_log","action":"create","fields":{"rule_id":%q,"performed_at":"2026-09-01","meter_reading":239}},
		{"type":"maintenance_rule","action":"update","id":%q,"fields":{"acknowledged_reason":"waiting for parts"}}]}`, gen.ID, rule.ID, rule.ID, rule.ID, rule.ID)
	proposal := runPropose(t, deps, args)

	if len(proposal.Ops) != 5 || proposal.ID == "" || proposal.Status != assistantProposalPending {
		t.Fatalf("expected a pending 5-op proposal with an id, got %+v", proposal)
	}
	if after := countMaintenanceRows(t, store); after != before {
		t.Fatalf("propose must write nothing: rows %+v before, %+v after", before, after)
	}
	afterRule, _ := store.GetMaintenanceRuleRow(rule.ID)
	if !afterRule.UpdatedAt.Equal(beforeRule.UpdatedAt) || afterRule.LastDoneAt != beforeRule.LastDoneAt || afterRule.Acknowledged {
		t.Fatalf("propose must not touch the rule: before %+v, after %+v", beforeRule, afterRule)
	}
	for i, op := range proposal.Ops {
		if strings.TrimSpace(op.Description) == "" {
			t.Errorf("op %d (%s) has no description", i, op.Type+" "+op.Action)
		}
	}
}

func TestProposeMaintenanceChanges_SummariesAreInOperatorWords(t *testing.T) {
	deps, store := proposeDeps(t)
	gen := mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical"})
	rule := mustToolRule(t, store, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Oil and filter", IntervalHours: fptr(250)})

	proposal := runPropose(t, deps, fmt.Sprintf(`{"operations":[
		{"type":"maintenance_rule","action":"create","fields":{"equipment_id":%q,"description":"Belts","interval_hours":250,"interval_months":12,"last_done_at":"2025-01-09","last_done_meter_reading":239}},
		{"type":"maintenance_rule","action":"create","fields":{"description":"Registration renewal","interval_months":12}},
		{"type":"maintenance_rule","action":"update","id":%q,"fields":{"last_done_at":"2025-01-09","last_done_meter_reading":239}},
		{"type":"maintenance_rule","action":"update","id":%q,"fields":{"acknowledged_reason":"waiting for parts"}}]}`, gen.ID, rule.ID, rule.ID))

	want := []string{
		"Add Generator · Belts: every 250 h or 12 mo, last done 9 Jan 2025 at 239 h (meter)",
		"Add Registration renewal: every 12 mo",
		"Generator · Oil and filter: last done 9 Jan 2025 at 239 h (meter)",
		"Acknowledge Generator · Oil and filter: waiting for parts",
	}
	for i, w := range want {
		if proposal.Ops[i].Description != w {
			t.Errorf("op %d summary:\n got  %q\n want %q", i, proposal.Ops[i].Description, w)
		}
	}
}

func TestProposeMaintenanceChanges_SnapshotsTheTargetRule(t *testing.T) {
	deps, store := proposeDeps(t)
	gen := mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical"})
	rule := mustToolRule(t, store, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Oil", IntervalMonths: iptr(12)})

	proposal := runPropose(t, deps, fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"acknowledged_reason":"later"}},{"type":"maintenance_rule","action":"create","fields":{"description":"X","interval_months":6}}]}`, rule.ID))
	want := rule.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	if proposal.Ops[0].BaseVersion != want {
		t.Fatalf("expected the rule's updated_at %q as the snapshot, got %q", want, proposal.Ops[0].BaseVersion)
	}
	if proposal.Ops[1].BaseVersion != "" {
		t.Fatalf("a create has no rule to snapshot, got %q", proposal.Ops[1].BaseVersion)
	}
}

func TestProposeMaintenanceChanges_UpdateMergesAndClears(t *testing.T) {
	deps, store := proposeDeps(t)
	gen := mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical"})
	rule := mustToolRule(t, store, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Oil and filter", IntervalHours: fptr(250), IntervalMonths: iptr(12)})

	proposal := runPropose(t, deps, fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"interval_hours":300,"interval_months":null}}]}`, rule.ID))
	if got, want := proposal.Ops[0].Description, "Change Generator · Oil and filter: now every 300 h"; got != want {
		t.Fatalf("summary %q, want %q", got, want)
	}
}

func TestProposeMaintenanceChanges_RefusesBadOps(t *testing.T) {
	deps, store := proposeDeps(t)
	gen := mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical"})
	rule := mustToolRule(t, store, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Oil", IntervalMonths: iptr(12)})

	cases := []struct{ name, args, want string }{
		{"no ops", `{"operations":[]}`, "ops is required"},
		{"unknown action", `{"operations":[{"type":"maintenance_rule","action":"rename","id":"x"}]}`, "action: must be create, update or delete"},
		{"delete is not offered", `{"operations":[{"type":"maintenance_rule","action":"delete","id":"x"}]}`, "maintenance_rule records cannot be deleted by a changeset"},
		{"unknown type", `{"operations":[{"type":"anchor","action":"create"}]}`, `type: unknown record type "anchor"`},
		{"unknown operation key", `{"operations":[{"type":"maintenance_rule","action":"update","id":"x","resaon":"typo"}]}`, "resaon"},
		{"unknown field", fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"resaon":"typo"}}]}`, rule.ID), "resaon: maintenance_rule has no such field"},
		{"field from another type", fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"meter_reading":3}}]}`, rule.ID), "meter_reading: maintenance_rule has no such field"},
		{"unknown rule", `{"operations":[{"type":"maintenance_rule","action":"update","id":"nope","fields":{"acknowledged_reason":"r"}}]}`, "id: no maintenance rule with id"},
		{"missing rule id", `{"operations":[{"type":"maintenance_rule","action":"update","fields":{"acknowledged_reason":"r"}}]}`, "id: is required"},
		{"copy op is gone", `{"operations":[{"type":"maintenance_rule","action":"copy_profile_schedule"}]}`, "action: must be create, update or delete"},
		{"log needs a rule", `{"operations":[{"type":"maintenance_log","action":"create","fields":{"performed_at":"2026-09-01"}}]}`, "rule_id: is required"},
		{"log for an unknown rule", `{"operations":[{"type":"maintenance_log","action":"create","fields":{"rule_id":"nope","performed_at":"2026-09-01"}}]}`, "rule_id: no maintenance rule with id"},
		{"a log entry cannot be changed", `{"operations":[{"type":"maintenance_log","action":"update","id":"x","fields":{"who":"me"}}]}`, "maintenance_log records cannot be updated by a changeset"},
		{"update changes nothing", fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"interval_months":12}}]}`, rule.ID), "exactly as it is"},
		{"clearing an acknowledgement that is not there", fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"acknowledged_reason":""}}]}`, rule.ID), "exactly as it is"},
		{"bad clear", fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"description":null}}]}`, rule.ID), "description: cannot be cleared"},
		{"an item's own rule cannot move", fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"equipment_id":"other"}}]}`, rule.ID), "equipment_id: a rule cannot move to another item"},
		{"reading with no item", `{"operations":[{"type":"maintenance_rule","action":"create","fields":{"description":"Cert","interval_months":12,"last_done_meter_reading":10}}]}`, "last_done_meter_reading"},
		{"second op is the bad one", fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"acknowledged_reason":"r"}},{"type":"maintenance_rule","action":"update","id":"nope","fields":{"acknowledged_reason":"r"}}]}`, rule.ID), "ops[1] (maintenance_rule update)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if msg := proposeError(t, deps, tc.args); !strings.Contains(msg, tc.want) {
				t.Fatalf("expected the error to contain %q, got %q", tc.want, msg)
			}
		})
	}
}

func TestProposeMaintenanceChanges_RequiresToday(t *testing.T) {
	deps, _ := proposeDeps(t)
	deps.today = time.Time{}
	msg := proposeError(t, deps, `{"operations":[{"type":"maintenance_rule","action":"create","fields":{"description":"X","interval_months":6}}]}`)
	if !strings.Contains(msg, "today") {
		t.Fatalf("expected a today error, got %q", msg)
	}
}

// ── acknowledgement through the registered field ────────────────────────

func TestProposeChanges_AcknowledgedReasonSetsAndClears(t *testing.T) {
	deps, store := proposeDeps(t)
	gen := mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical"})
	rule := mustToolRule(t, store, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Oil", IntervalMonths: iptr(12)})
	if _, err := store.AcknowledgeMaintenanceRule(rule.ID, "waiting for parts"); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}

	p := runPropose(t, deps, fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"acknowledged_reason":""}}]}`, rule.ID))
	if got, want := p.Ops[0].Description, "Clear the acknowledgement on Generator · Oil"; got != want {
		t.Fatalf("description %q, want %q", got, want)
	}
	if p.Ops[0].Before["acknowledged_reason"] != "waiting for parts" || p.Ops[0].After["acknowledged_reason"] != nil {
		t.Fatalf("before and after should show the reason going, got %v -> %v", p.Ops[0].Before, p.Ops[0].After)
	}
}

func TestProposeChanges_ARuleChangeCarriesItsBaselineAndAcknowledgement(t *testing.T) {
	deps, store := proposeDeps(t)
	gen := mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical"})
	rule := mustToolRule(t, store, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Oil", IntervalMonths: iptr(12)})
	p := runPropose(t, deps, fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"interval_months":6,"last_done_at":"2026-01-05","acknowledged_reason":"later"}}]}`, rule.ID))
	if got, want := p.Ops[0].Description, "Change Generator · Oil: now every 6 mo, last done 5 Jan 2026; acknowledged: later"; got != want {
		t.Fatalf("description %q, want %q", got, want)
	}
}

func TestProposeChanges_ReadOnlyFieldsAreRefused(t *testing.T) {
	deps, store := proposeDeps(t)
	gen := mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical"})
	rule := mustToolRule(t, store, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Oil", IntervalMonths: iptr(12)})
	msg := proposeError(t, deps, fmt.Sprintf(`{"operations":[{"type":"maintenance_log","action":"create","fields":{"rule_id":%q,"performed_at":"2026-09-01","engine_hours":10}}]}`, rule.ID))
	if !strings.Contains(msg, "engine_hours: cannot be changed") {
		t.Fatalf("got %q", msg)
	}
}

func TestProposeChanges_NotApplicableIsRefusedOnACreate(t *testing.T) {
	deps, store := proposeDeps(t)
	gen := mustToolEquipment(t, store, equipmentItem{Name: "Generator", System: "electrical"})
	msg := proposeError(t, deps, fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"create","fields":{"equipment_id":%q,"description":"Belts","interval_months":12,"not_applicable":true}}]}`, gen.ID))
	if !strings.Contains(msg, "not_applicable: only a profile job") {
		t.Fatalf("got %q", msg)
	}
}
