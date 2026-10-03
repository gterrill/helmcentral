package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Persistence and apply for Mate's maintenance proposals (ADR 0146): the card
// is saved with the assistant message, Apply runs every operation in one
// transaction, and a stale, dismissed or repeated apply never writes.

// proposalEnv is a document store and an assistant store on ONE database file
// (as in production, ADR 0141) with one shared, controllable clock, so a rule
// edited after a proposal has a different updated_at.
type proposalEnv struct {
	docs  *documentStore
	asst  *assistantStore
	clock time.Time
	conv  assistantConversation
	deps  assistantToolDeps
}

func newProposalEnv(t *testing.T) *proposalEnv {
	t.Helper()
	path := filepath.Join(t.TempDir(), "helmcentral.sqlite")
	docs, err := newDocumentStore(path)
	if err != nil {
		t.Fatalf("newDocumentStore: %v", err)
	}
	t.Cleanup(func() { docs.Close() })
	asst, err := newAssistantStore(path)
	if err != nil {
		t.Fatalf("newAssistantStore: %v", err)
	}
	t.Cleanup(func() { asst.Close() })

	env := &proposalEnv{docs: docs, asst: asst, clock: time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)}
	now := func() time.Time { return env.clock }
	docs.now, asst.now = now, now

	prevDocs, prevAsst := globalDocumentStore, globalAssistantStore
	globalDocumentStore, globalAssistantStore = docs, asst
	t.Cleanup(func() { globalDocumentStore, globalAssistantStore = prevDocs, prevAsst })
	withGlobalSnapshot(t, snapshotWithSelfValues(map[string]any{}))

	env.conv, err = asst.CreateConversation("Maintenance")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	env.deps = assistantToolDeps{
		documents: func() *documentStore { return docs },
		today:     mustParseDate(t, "2026-09-30"),
	}
	return env
}

func (e *proposalEnv) advance() { e.clock = e.clock.Add(time.Hour) }

// propose runs the real tool and saves its proposal on a fresh assistant
// message, the way a run does, returning the saved (pending) proposal.
func (e *proposalEnv) propose(t *testing.T, args string) assistantProposal {
	t.Helper()
	p := runPropose(t, e.deps, args)
	e.saveOnMessage(t, "Here is the change. Tap Apply.", p)
	return p
}

func (e *proposalEnv) saveOnMessage(t *testing.T, content string, proposals ...assistantProposal) assistantMessage {
	t.Helper()
	msg, err := e.asst.AppendMessage(assistantMessage{ConversationID: e.conv.ID, Role: "assistant", Content: content, Proposals: proposals})
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	return msg
}

func (e *proposalEnv) apply(id string) (assistantProposal, error) {
	return e.asst.ApplyProposal(id, e.deps.today)
}

func (e *proposalEnv) equipment(t *testing.T, item equipmentItem) equipmentItem {
	t.Helper()
	return mustToolEquipment(t, e.docs, item)
}

func (e *proposalEnv) counts(t *testing.T) maintenanceRowCounts {
	return countMaintenanceRows(t, e.docs)
}

// ── saving with the message ─────────────────────────────────────────────

func TestAppendMessage_SavesProposalsWithTheAssistantRowAndListMessagesReturnsThem(t *testing.T) {
	env := newProposalEnv(t)
	env.propose(t, `{"operations":[{"type":"maintenance_rule","action":"create","fields":{"description":"Registration renewal","interval_months":12}}]}`)
	env.propose(t, `{"operations":[{"type":"maintenance_rule","action":"create","fields":{"description":"Liferaft service","interval_months":36}}]}`)

	msgs, err := env.asst.ListMessages(env.conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 2 || len(msgs[0].Proposals) != 1 || len(msgs[1].Proposals) != 1 {
		t.Fatalf("expected each assistant message to carry its proposal, got %+v", msgs)
	}
	p := msgs[0].Proposals[0]
	if p.Status != assistantProposalPending || p.MessageID != msgs[0].ID || len(p.Ops) != 1 || p.Ops[0].Description == "" {
		t.Fatalf("unexpected stored proposal: %+v", p)
	}
}

func TestAppendMessage_ProposalFailureLeavesNoMessage(t *testing.T) {
	env := newProposalEnv(t)
	p := runPropose(t, env.deps, `{"operations":[{"type":"maintenance_rule","action":"create","fields":{"description":"X","interval_months":12}}]}`)
	// The same proposal id twice trips the primary key on the second insert.
	_, err := env.asst.AppendMessage(assistantMessage{ConversationID: env.conv.ID, Role: "assistant", Content: "hi", Proposals: []assistantProposal{p, p}})
	if err == nil {
		t.Fatal("expected a duplicate proposal id to fail the append")
	}
	msgs, err := env.asst.ListMessages(env.conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("the message and its proposals save together or not at all, got %+v", msgs)
	}
}

func TestAppendMessage_OnlyAnAssistantMessageCarriesProposals(t *testing.T) {
	env := newProposalEnv(t)
	p := runPropose(t, env.deps, `{"operations":[{"type":"maintenance_rule","action":"create","fields":{"description":"X","interval_months":12}}]}`)
	if _, err := env.asst.AppendMessage(assistantMessage{ConversationID: env.conv.ID, Role: "user", Content: "hi", Proposals: []assistantProposal{p}}); err == nil {
		t.Fatal("expected a user message with proposals to be refused")
	}
}

func TestDeleteConversation_RemovesItsProposals(t *testing.T) {
	env := newProposalEnv(t)
	p := env.propose(t, `{"operations":[{"type":"maintenance_rule","action":"create","fields":{"description":"X","interval_months":12}}]}`)
	if err := env.asst.DeleteConversation(env.conv.ID); err != nil {
		t.Fatalf("DeleteConversation: %v", err)
	}
	if _, err := env.asst.GetProposal(p.ID); !errors.Is(err, errAssistantProposalNotFound) {
		t.Fatalf("expected the proposal to go with its conversation, got %v", err)
	}
}

// ── apply ───────────────────────────────────────────────────────────────

func TestApplyProposal_RunsEveryOperationInOneTransaction(t *testing.T) {
	env := newProposalEnv(t)
	gen := env.equipment(t, equipmentItem{Name: "Generator", System: "electrical", ProfileID: "onan"})
	// A meter replacement on 2025-06-01: old 1000, new 0, so a meter reading
	// after it is 1000 h short of true engine hours.
	if _, err := env.docs.RecordHourMeterReset(gen.ID, 1000, 0, "2025-06-01"); err != nil {
		t.Fatalf("RecordHourMeterReset: %v", err)
	}
	oil := mustToolRule(t, env.docs, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Oil and filter", IntervalHours: fptr(250)})
	belts := mustToolRule(t, env.docs, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Belts", IntervalMonths: iptr(12)})
	anodes := mustToolRule(t, env.docs, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Anodes", IntervalMonths: iptr(12)})
	env.advance()

	p := env.propose(t, fmt.Sprintf(`{"operations":[
		{"type":"maintenance_rule","action":"create","fields":{"equipment_id":%q,"description":"Coolant","interval_months":24,"last_done_at":"2025-09-01","last_done_meter_reading":39}},
		{"type":"maintenance_rule","action":"update","id":%q,"fields":{"interval_hours":300}},
		{"type":"maintenance_log","action":"create","fields":{"rule_id":%q,"performed_at":"2025-10-01","meter_reading":45,"who":"Gavin","cost":120}},
		{"type":"maintenance_rule","action":"update","id":%q,"fields":{"acknowledged_reason":"parts on order"}}]}`, gen.ID, oil.ID, belts.ID, anodes.ID))
	before := env.counts(t)
	env.advance()

	applied, err := env.apply(p.ID)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if applied.Status != assistantProposalApplied || applied.ResolvedAt == nil || len(applied.Result) == 0 {
		t.Fatalf("expected an applied proposal with a stored result, got %+v", applied)
	}

	after := env.counts(t)
	// create_rule = 1 new rule; complete_rule = 1 log entry.
	if after.rules != before.rules+1 || after.entries != before.entries+1 {
		t.Fatalf("expected +1 rule and +1 log entry, got %+v -> %+v", before, after)
	}

	updated, _ := env.docs.GetMaintenanceRuleRow(oil.ID)
	if updated.IntervalHours == nil || *updated.IntervalHours != 300 {
		t.Fatalf("update_rule: expected 300 h, got %+v", updated.IntervalHours)
	}
	done, _ := env.docs.GetMaintenanceRuleRow(belts.ID)
	if done.LastDoneAt != "2025-10-01" || done.LastDoneHours == nil || *done.LastDoneHours != 1045 {
		t.Fatalf("complete_rule: expected last done 2025-10-01 at true 1045 h (meter 45 + 1000 offset), got %+v", done)
	}
	acked, _ := env.docs.GetMaintenanceRuleRow(anodes.ID)
	if !acked.Acknowledged || acked.AckReason != "parts on order" {
		t.Fatalf("acknowledge: got %+v", acked)
	}

	rules, _ := env.docs.ListMaintenanceRuleRows(maintenanceRuleFilter{EquipmentID: gen.ID, IncludeStored: true})
	var coolant *maintenanceRule
	for i := range rules {
		if rules[i].Description == "Coolant" {
			coolant = &rules[i]
		}
	}
	if coolant == nil || coolant.LastDoneAt != "2025-09-01" || coolant.LastDoneHours == nil || *coolant.LastDoneHours != 1039 {
		t.Fatalf("create_rule with a last-done meter reading of 39 after the reset should store true 1039 h, got %+v", coolant)
	}
}

func TestApplyProposal_StaleRuleIs409AndNothingIsWritten(t *testing.T) {
	env := newProposalEnv(t)
	gen := env.equipment(t, equipmentItem{Name: "Generator", System: "electrical"})
	rule := mustToolRule(t, env.docs, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Belts", IntervalMonths: iptr(12)})
	env.advance()

	p := env.propose(t, fmt.Sprintf(`{"operations":[
		{"type":"maintenance_rule","action":"create","fields":{"description":"Registration renewal","interval_months":12}},
		{"type":"maintenance_rule","action":"update","id":%q,"fields":{"acknowledged_reason":"later"}}]}`, rule.ID))
	before := env.counts(t)

	// The operator edits the rule after Mate proposed.
	env.advance()
	at := "2026-01-01"
	if _, err := env.docs.SetMaintenanceRuleLastDone(rule.ID, &at, nil); err != nil {
		t.Fatalf("SetMaintenanceRuleLastDone: %v", err)
	}

	_, err := env.apply(p.ID)
	if !errors.Is(err, errAssistantProposalStale) {
		t.Fatalf("expected the stale refusal, got %v", err)
	}
	if after := env.counts(t); after != before {
		t.Fatalf("a stale proposal writes nothing, rows %+v -> %+v", before, after)
	}
	got, _ := env.asst.GetProposal(p.ID)
	if got.Status != assistantProposalStale || !strings.Contains(got.StaleReason, "changed since Mate proposed") {
		t.Fatalf("a stale proposal is stored as stale with its reason, got %q %q", got.Status, got.StaleReason)
	}
	stored, _ := env.docs.GetMaintenanceRuleRow(rule.ID)
	if stored.Acknowledged {
		t.Fatal("the acknowledge must not have run")
	}
}

func TestApplyProposal_DeletedTargetRuleCountsAsStale(t *testing.T) {
	env := newProposalEnv(t)
	rule := mustToolRule(t, env.docs, maintenanceRuleInput{Description: "Cert", IntervalMonths: iptr(12)})
	p := env.propose(t, fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"acknowledged_reason":"later"}}]}`, rule.ID))
	if err := env.docs.DeleteMaintenanceRule(rule.ID); err != nil {
		t.Fatalf("DeleteMaintenanceRule: %v", err)
	}
	if _, err := env.apply(p.ID); !errors.Is(err, errAssistantProposalStale) {
		t.Fatalf("expected stale, got %v", err)
	}
}

func TestApplyProposal_AFailingOperationMidwayRollsEverythingBack(t *testing.T) {
	env := newProposalEnv(t)
	gen := env.equipment(t, equipmentItem{Name: "Generator", System: "electrical"})
	p := env.propose(t, fmt.Sprintf(`{"operations":[
		{"type":"maintenance_rule","action":"create","fields":{"description":"Registration renewal","interval_months":12}},
		{"type":"maintenance_rule","action":"create","fields":{"equipment_id":%q,"description":"Belts","interval_months":12}}]}`, gen.ID))
	before := env.counts(t)

	// The second operation's item is deleted after the proposal was made: the
	// first operation has already run inside the transaction when this fails.
	if _, err := env.docs.DeleteEquipment(gen.ID, false); err != nil {
		t.Fatalf("DeleteEquipment: %v", err)
	}
	_, err := env.apply(p.ID)
	if !errors.Is(err, errAssistantProposalStale) || !strings.Contains(err.Error(), "no equipment with id") {
		t.Fatalf("expected a stale refusal naming the missing equipment, got %v", err)
	}
	if !strings.Contains(err.Error(), "change 2 of 2") {
		t.Fatalf("expected the error to say which change failed, got %q", err)
	}
	if after := env.counts(t); after != before {
		t.Fatalf("the first operation must be rolled back, rows %+v -> %+v", before, after)
	}
	got, _ := env.asst.GetProposal(p.ID)
	if got.Status != assistantProposalStale || !strings.Contains(got.StaleReason, "change 2 of 2") {
		t.Fatalf("a failed apply is stored as stale with the failing change, got %q %q", got.Status, got.StaleReason)
	}
}

func TestApplyProposal_SecondApplyReturnsTheStoredResultAndWritesNothing(t *testing.T) {
	env := newProposalEnv(t)
	p := env.propose(t, `{"operations":[{"type":"maintenance_rule","action":"create","fields":{"description":"Registration renewal","interval_months":12}}]}`)

	first, err := env.apply(p.ID)
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}
	after := env.counts(t)
	env.advance()
	second, err := env.apply(p.ID)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if again := env.counts(t); again != after {
		t.Fatalf("a repeated apply writes nothing, rows %+v -> %+v", after, again)
	}
	if second.Status != assistantProposalApplied || string(second.Result) != string(first.Result) || !second.ResolvedAt.Equal(*first.ResolvedAt) {
		t.Fatalf("expected the stored result back, first %+v, second %+v", first, second)
	}
}

func TestApplyProposal_DismissedCannotBeAppliedAndAppliedCannotBeDismissed(t *testing.T) {
	env := newProposalEnv(t)
	dismissed := env.propose(t, `{"operations":[{"type":"maintenance_rule","action":"create","fields":{"description":"A","interval_months":12}}]}`)
	applied := env.propose(t, `{"operations":[{"type":"maintenance_rule","action":"create","fields":{"description":"B","interval_months":12}}]}`)

	if _, err := env.asst.DismissProposal(dismissed.ID); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	if again, err := env.asst.DismissProposal(dismissed.ID); err != nil || again.Status != assistantProposalDismissed {
		t.Fatalf("dismissing twice is a no-op, got %+v %v", again, err)
	}
	before := env.counts(t)
	if _, err := env.apply(dismissed.ID); !errors.Is(err, errAssistantProposalDismissed) {
		t.Fatalf("expected a dismissed proposal to be refused, got %v", err)
	}
	if after := env.counts(t); after != before {
		t.Fatalf("a refused apply writes nothing, %+v -> %+v", before, after)
	}

	if _, err := env.apply(applied.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := env.asst.DismissProposal(applied.ID); !errors.Is(err, errAssistantProposalApplied) {
		t.Fatalf("expected an applied proposal to refuse dismissal, got %v", err)
	}
	if _, err := env.asst.GetProposal("nope"); !errors.Is(err, errAssistantProposalNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

// ── endpoints ───────────────────────────────────────────────────────────

func TestApplyAssistantProposalHandler_StatusCodes(t *testing.T) {
	env := newProposalEnv(t)
	p := env.propose(t, `{"operations":[{"type":"maintenance_rule","action":"create","fields":{"description":"Registration renewal","interval_months":12}}]}`)

	// today is required, like every maintenance write.
	c, rec := newAssistantEchoContext(http.MethodPost, "/api/assistant/proposals/"+p.ID+"/apply", "", p.ID)
	if err := applyAssistantProposalHandler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if rec.Code != http.StatusBadRequest || resp["field"] != "today" {
		t.Fatalf("expected 400 naming today, got %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := env.asst.GetProposal(p.ID); got.Status != assistantProposalPending {
		t.Fatalf("a 400 must not apply, got %q", got.Status)
	}

	c, rec = newAssistantEchoContext(http.MethodPost, "/api/assistant/proposals/"+p.ID+"/apply?today=2026-09-30", "", p.ID)
	if err := applyAssistantProposalHandler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	var ok struct {
		Proposal assistantProposal `json:"proposal"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &ok) != nil || ok.Proposal.Status != assistantProposalApplied {
		t.Fatalf("expected 200 with an applied proposal, got %d %s", rec.Code, rec.Body.String())
	}

	// A second tap is the same 200 with no write.
	rows := env.counts(t)
	c, rec = newAssistantEchoContext(http.MethodPost, "/api/assistant/proposals/"+p.ID+"/apply?today=2026-09-30", "", p.ID)
	_ = applyAssistantProposalHandler(c)
	if rec.Code != http.StatusOK || env.counts(t) != rows {
		t.Fatalf("expected an idempotent 200, got %d", rec.Code)
	}

	// Dismissing an applied proposal is a 409.
	c, rec = newAssistantEchoContext(http.MethodPost, "/api/assistant/proposals/"+p.ID+"/dismiss", "", p.ID)
	_ = dismissAssistantProposalHandler(c)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 dismissing an applied proposal, got %d %s", rec.Code, rec.Body.String())
	}

	c, rec = newAssistantEchoContext(http.MethodPost, "/api/assistant/proposals/nope/apply?today=2026-09-30", "", "nope")
	_ = applyAssistantProposalHandler(c)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown proposal, got %d", rec.Code)
	}
}

func TestApplyAssistantProposalHandler_StaleIs409WithTheReason(t *testing.T) {
	env := newProposalEnv(t)
	rule := mustToolRule(t, env.docs, maintenanceRuleInput{Description: "Cert", IntervalMonths: iptr(12)})
	env.advance()
	p := env.propose(t, fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"acknowledged_reason":"later"}}]}`, rule.ID))
	env.advance()
	if _, err := env.docs.AcknowledgeMaintenanceRule(rule.ID, "someone else"); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}

	c, rec := newAssistantEchoContext(http.MethodPost, "/api/assistant/proposals/"+p.ID+"/apply?today=2026-09-30", "", p.ID)
	_ = applyAssistantProposalHandler(c)
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if rec.Code != http.StatusConflict || !strings.Contains(resp["error"], "changed since Mate proposed") {
		t.Fatalf("expected 409 with the reason, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestApplyAssistantProposalHandler_ConflictNamesTheProposalStatus(t *testing.T) {
	env := newProposalEnv(t)
	p := env.propose(t, `{"operations":[{"type":"maintenance_rule","action":"create","fields":{"description":"Dismissed one","interval_months":12}}]}`)
	if _, err := env.asst.DismissProposal(p.ID); err != nil {
		t.Fatalf("dismiss: %v", err)
	}

	c, rec := newAssistantEchoContext(http.MethodPost, "/api/assistant/proposals/"+p.ID+"/apply?today=2026-09-30", "", p.ID)
	_ = applyAssistantProposalHandler(c)
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if rec.Code != http.StatusConflict || resp["proposal_status"] != assistantProposalDismissed {
		t.Fatalf("expected 409 naming the dismissed status, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestGetAssistantConversationHandler_ReturnsEachMessagesProposalsWithTheirCurrentStatus(t *testing.T) {
	env := newProposalEnv(t)
	p := env.propose(t, `{"operations":[{"type":"maintenance_rule","action":"create","fields":{"description":"Registration renewal","interval_months":12}}]}`)
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}

	c, rec := newAssistantEchoContext(http.MethodGet, "/api/assistant/conversations/"+env.conv.ID, "", env.conv.ID)
	if err := getAssistantConversationHandler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	var resp struct {
		Messages []assistantMessage `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Messages) != 1 || len(resp.Messages[0].Proposals) != 1 {
		t.Fatalf("expected the proposal on its message, got %s", rec.Body.String())
	}
	got := resp.Messages[0].Proposals[0]
	if got.ID != p.ID || got.Status != assistantProposalApplied || got.Ops[0].Description == "" {
		t.Fatalf("expected the applied status and the summary after a reload, got %+v", got)
	}
}

// ── history ─────────────────────────────────────────────────────────────

func TestAssistantHistoryMessages_CarriesEachProposalsSummaryAndStatus(t *testing.T) {
	env := newProposalEnv(t)
	pending := env.propose(t, `{"operations":[{"type":"maintenance_rule","action":"create","fields":{"description":"Pending one","interval_months":12}}]}`)
	applied := env.propose(t, `{"operations":[{"type":"maintenance_rule","action":"create","fields":{"description":"Applied one","interval_months":12}}]}`)
	dismissed := env.propose(t, `{"operations":[{"type":"maintenance_rule","action":"create","fields":{"description":"Dismissed one","interval_months":12}}]}`)
	if _, err := env.apply(applied.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := env.asst.DismissProposal(dismissed.ID); err != nil {
		t.Fatalf("dismiss: %v", err)
	}

	msgs, err := env.asst.ListMessages(env.conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	history, err := assistantHistoryMessages(msgs, noDocumentLookup(t))
	if err != nil {
		t.Fatalf("assistantHistoryMessages: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("expected three turns, got %d", len(history))
	}
	want := []struct{ status, summary, marker string }{
		{"pending", pending.Ops[0].Description, "NONE of these changes have been made yet"},
		{"applied", applied.Ops[0].Description, "ARE now made"},
		{"dismissed", dismissed.Ops[0].Description, "do not propose them again unless asked"},
	}
	for i, w := range want {
		text := string(history[i].Content)
		for _, needle := range []string{"[Proposal " + w.status, w.summary, w.marker} {
			if !strings.Contains(text, needle) {
				t.Errorf("turn %d (%s): expected %q in:\n%s", i, w.status, needle, text)
			}
		}
	}
}

// ── the runner collects proposals; a failed run saves none ──────────────

func proposalToolCallRound(t *testing.T) *http.Response {
	t.Helper()
	return chatResponse(t, http.StatusOK, openRouterChatResponse{
		Model: "m",
		Choices: []openRouterChoice{{Message: openRouterMessage{
			Role: "assistant",
			ToolCalls: []openRouterToolCall{{ID: "call_1", Type: "function", Function: openRouterToolCallFunction{
				Name: "propose_changes", Arguments: openRouterArguments(`{"operations":[]}`),
			}}},
		}}},
		Usage: usage(10, 5, 0),
	})
}

func TestAssistantRunner_CollectsProposalsFromTheToolResult(t *testing.T) {
	env := newProposalEnv(t)
	result, err := env.deps.execute(context.Background(), "propose_changes",
		json.RawMessage(`{"operations":[{"type":"maintenance_rule","action":"create","fields":{"description":"Registration renewal","interval_months":12}}]}`))
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	doer := &queuedChatDoer{responses: []*http.Response{proposalToolCallRound(t), finalResponse(t, "Tap Apply.", "m", usage(10, 5, 0))}, errs: []error{nil, nil}}
	tools := &fakeToolExecutor{results: map[string]string{"propose_changes": result}}
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: tools, emit: emit}

	reply, err := runner.run(context.Background(), "system", "", nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(reply.Proposals) != 1 || reply.Proposals[0].ID == "" || len(reply.Proposals[0].Ops) != 1 {
		t.Fatalf("expected the run to carry the proposal, got %+v", reply.Proposals)
	}
}

func TestAssistantRunner_AFailedProposeCallCollectsNothing(t *testing.T) {
	doer := &queuedChatDoer{responses: []*http.Response{proposalToolCallRound(t), finalResponse(t, "That did not validate.", "m", usage(10, 5, 0))}, errs: []error{nil, nil}}
	tools := &fakeToolExecutor{errs: map[string]error{"propose_changes": errors.New("ops[0] (create_rule): description: description is required")}}
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: tools, emit: emit}

	reply, err := runner.run(context.Background(), "system", "", nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(reply.Proposals) != 0 {
		t.Fatalf("a failed call has no proposal, got %+v", reply.Proposals)
	}
}

func TestPostAssistantMessageHandler_PersistsProposalsWithTheReplyAndAFailedRunSavesNone(t *testing.T) {
	env := newProposalEnv(t)
	secrets := withTestSecretsStore(t)
	if err := secrets.Set("OPENROUTER_API_KEY", "sk-test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	t.Setenv("SETTINGS_FILE", writeAssistantSettingsFixture(t, true, "openai/gpt-4o"))
	p := runPropose(t, env.deps, `{"operations":[{"type":"maintenance_rule","action":"create","fields":{"description":"Registration renewal","interval_months":12}}]}`)

	// A run that fails after proposing saves no message and so no proposal.
	swapAssistantRunner(t, func(apiKey, model, settingsPath string, autoRouter assistantAutoRouterOptions, webSearch bool, today time.Time, emit assistantEmitter) assistantRunnerFace {
		return &fakeAssistantRunner{emit: emit, err: errors.New("stopped")}
	})
	c, _ := newAssistantEchoContext(http.MethodPost, "/api/assistant/conversations/"+env.conv.ID+"/messages", `{"today":"2026-09-30","content":"add a renewal"}`, env.conv.ID)
	if err := postAssistantMessageHandler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if _, err := env.asst.GetProposal(p.ID); !errors.Is(err, errAssistantProposalNotFound) {
		t.Fatalf("a failed run saves no proposal, got %v", err)
	}

	// A run that succeeds saves the proposal with the assistant row and puts it
	// on the SSE message frame.
	swapAssistantRunner(t, func(apiKey, model, settingsPath string, autoRouter assistantAutoRouterOptions, webSearch bool, today time.Time, emit assistantEmitter) assistantRunnerFace {
		return &fakeAssistantRunner{emit: emit, reply: assistantReply{Content: "Tap Apply.", Model: "openai/gpt-4o", Proposals: []assistantProposal{p}}}
	})
	c, rec := newAssistantEchoContext(http.MethodPost, "/api/assistant/conversations/"+env.conv.ID+"/messages", `{"today":"2026-09-30","content":"add a renewal"}`, env.conv.ID)
	if err := postAssistantMessageHandler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	var frame struct {
		Message assistantMessage `json:"message"`
	}
	if err := json.Unmarshal([]byte(extractSSEEventData(t, rec.Body.String(), "message")), &frame); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(frame.Message.Proposals) != 1 || frame.Message.Proposals[0].ID != p.ID || frame.Message.Proposals[0].Status != assistantProposalPending {
		t.Fatalf("expected the SSE message to carry the pending proposal, got %+v", frame.Message)
	}
	if got, err := env.asst.GetProposal(p.ID); err != nil || got.MessageID != frame.Message.ID {
		t.Fatalf("expected the proposal stored on the reply, got %+v %v", got, err)
	}
}

// ── dry run, copy staleness, stored stale ───────────────────────────────

func TestPropose_RejectsWhatAnEarlierOpInTheSameProposalMakesInvalid(t *testing.T) {
	env := newProposalEnv(t)
	gen := env.equipment(t, equipmentItem{Name: "Generator", System: "electrical"})
	rule := mustToolRule(t, env.docs, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Belts", IntervalMonths: iptr(12)})
	before := env.counts(t)

	// Alone, completing a months-only rule needs no reading. After op 0 gives
	// it an hours interval it does.
	msg := proposeError(t, env.deps, fmt.Sprintf(`{"operations":[
		{"type":"maintenance_rule","action":"update","id":%q,"fields":{"interval_hours":250}},
		{"type":"maintenance_log","action":"create","fields":{"rule_id":%q,"performed_at":"2026-09-01"}}]}`, rule.ID, rule.ID))
	if !strings.Contains(msg, "ops[1] (maintenance_log create): meter_reading:") {
		t.Fatalf("expected the dry run to refuse op 1 naming meter_reading, got %q", msg)
	}
	if after := env.counts(t); after != before {
		t.Fatalf("the dry run must roll back, rows %+v -> %+v", before, after)
	}
	stored, _ := env.docs.GetMaintenanceRuleRow(rule.ID)
	if stored.IntervalHours != nil {
		t.Fatal("the dry run's update must not have been committed")
	}
}

func TestPropose_CompletionSummaryShowsTheNewFixedDueDate(t *testing.T) {
	env := newProposalEnv(t)
	cert := mustToolRule(t, env.docs, maintenanceRuleInput{Description: "Registration", IntervalMonths: iptr(12), FixedDueDate: "2026-10-01"})
	oneOff := mustToolRule(t, env.docs, maintenanceRuleInput{Description: "Liferaft", FixedDueDate: "2026-10-05"})

	p := runPropose(t, env.deps, fmt.Sprintf(`{"operations":[
		{"type":"maintenance_log","action":"create","fields":{"rule_id":%q,"performed_at":"2026-09-29"}},
		{"type":"maintenance_log","action":"create","fields":{"rule_id":%q,"performed_at":"2026-09-29","new_due_date":"2028-09-29"}}]}`, cert.ID, oneOff.ID))
	if !strings.HasSuffix(p.Ops[0].Description, ", next due 29 Sep 2027") {
		t.Errorf("computed from interval_months, got %q", p.Ops[0].Description)
	}
	if !strings.HasSuffix(p.Ops[1].Description, ", next due 29 Sep 2028") {
		t.Errorf("operator-supplied new_due_date, got %q", p.Ops[1].Description)
	}
	stored, _ := env.docs.GetMaintenanceRuleRow(cert.ID)
	if stored.FixedDueDate != "2026-10-01" {
		t.Fatal("the dry run must not move the due date")
	}
}

func TestApplyProposal_ApplyTimeValidationFailureIsStoredAsStaleWithTheReason(t *testing.T) {
	env := newProposalEnv(t)
	// Built by hand, past the tool's dry run: an operation the commands refuse.
	bad := assistantProposal{ID: "p-bad", Ops: []changeOp{{Type: recordTypeMaintenanceRule, Action: changeCreate, Fields: map[string]any{"description": " ", "interval_months": 12.0}, Description: "Add nothing"}}}
	env.saveOnMessage(t, "Tap Apply.", bad)

	_, err := env.apply("p-bad")
	if !errors.Is(err, errAssistantProposalStale) || !strings.Contains(err.Error(), "description is required") {
		t.Fatalf("expected a stale refusal carrying the validation reason, got %v", err)
	}
	got, _ := env.asst.GetProposal("p-bad")
	if got.Status != assistantProposalStale || !strings.Contains(got.StaleReason, "description is required") {
		t.Fatalf("expected stored stale with the reason, got %q %q", got.Status, got.StaleReason)
	}
	msgs, _ := env.asst.ListMessages(env.conv.ID)
	if msgs[0].Proposals[0].Status != assistantProposalStale || msgs[0].Proposals[0].StaleReason == "" {
		t.Fatalf("a reload must return the stale status and reason, got %+v", msgs[0].Proposals[0])
	}
}

func TestApplyProposal_AStaleProposalCanNeitherBeAppliedNorDismissedAndHistoryTellsMate(t *testing.T) {
	env := newProposalEnv(t)
	bad := assistantProposal{ID: "p-bad", Ops: []changeOp{{Type: recordTypeMaintenanceRule, Action: changeCreate, Fields: map[string]any{"description": " ", "interval_months": 12.0}, Description: "Add nothing"}}}
	env.saveOnMessage(t, "Tap Apply.", bad)
	_, _ = env.apply("p-bad")
	before := env.counts(t)

	if _, err := env.apply("p-bad"); !errors.Is(err, errAssistantProposalStale) {
		t.Fatalf("a stale proposal cannot be applied, got %v", err)
	}
	if _, err := env.asst.DismissProposal("p-bad"); !errors.Is(err, errAssistantProposalStale) {
		t.Fatalf("a stale proposal cannot be dismissed, got %v", err)
	}
	if env.counts(t) != before {
		t.Fatal("nothing may be written")
	}

	msgs, _ := env.asst.ListMessages(env.conv.ID)
	history, err := assistantHistoryMessages(msgs, noDocumentLookup(t))
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	text := string(history[0].Content)
	for _, needle := range []string{"[Proposal stale", "went stale", "description is required", "fresh proposal"} {
		if !strings.Contains(text, needle) {
			t.Errorf("expected %q in:\n%s", needle, text)
		}
	}

	c, rec := newAssistantEchoContext(http.MethodPost, "/api/assistant/proposals/p-bad/dismiss", "", "p-bad")
	_ = dismissAssistantProposalHandler(c)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 dismissing a stale proposal, got %d", rec.Code)
	}
}

// ── profile jobs (ADR 0148) ─────────────────────────────────────────────

func TestProposal_UpdateOnAProfileJobAppliesAsAnOverride(t *testing.T) {
	env := newProposalEnv(t)
	setupEngineProfiles(t, map[string]string{"a.json": scheduleProfileA})
	item := env.equipment(t, equipmentItem{Name: "Main engine", System: "propulsion", ProfileID: "sched-a", HourMeterPath: "propulsion.main.runTime"})
	job := maintenanceJobID(item.ID, "engine-oil")

	p := env.propose(t, fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"interval_hours":500}}]}`, job))
	if got, want := p.Ops[0].Description, "Change Main engine · Engine oil and filter for this item: now every 500 h or 12 mo"; got != want {
		t.Fatalf("summary %q, want %q", got, want)
	}
	if n := env.counts(t).rules; n != 0 {
		t.Fatalf("propose must not create the job's row, got %d rows", n)
	}

	env.advance()
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	eff, err := env.docs.ResolveMaintenanceRule(job)
	if err != nil {
		t.Fatal(err)
	}
	if eff.IntervalHours == nil || *eff.IntervalHours != 500 || strings.Join(eff.OverriddenFields, ",") != "interval_hours" {
		t.Fatalf("expected a 500 h override, got %+v %v", eff.IntervalHours, eff.OverriddenFields)
	}
	if eff.IntervalMonths == nil || *eff.IntervalMonths != 12 {
		t.Errorf("months must still follow the profile, got %+v", eff.IntervalMonths)
	}
}

func TestProposal_UpdateOnAProfileJobRefusesWhatItCannotChange(t *testing.T) {
	env := newProposalEnv(t)
	setupEngineProfiles(t, map[string]string{"a.json": scheduleProfileA})
	item := env.equipment(t, equipmentItem{Name: "Main engine", System: "propulsion", ProfileID: "sched-a"})
	job := maintenanceJobID(item.ID, "engine-oil")

	msg := proposeError(t, env.deps, fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"description":null}}]}`, job))
	if !strings.Contains(msg, "description: cannot be cleared") {
		t.Fatalf("got %q", msg)
	}
	msg = proposeError(t, env.deps, fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"interval_hours":250}}]}`, job))
	if !strings.Contains(msg, "exactly as it is") {
		t.Fatalf("an override equal to the current values is no change, got %q", msg)
	}
}

func TestProposal_CompletingAProfileJobUsesTheEffectiveInterval(t *testing.T) {
	env := newProposalEnv(t)
	setupEngineProfiles(t, map[string]string{"a.json": scheduleProfileA})
	item := env.equipment(t, equipmentItem{Name: "Main engine", System: "propulsion", ProfileID: "sched-a", HourMeterPath: "propulsion.main.runTime"})
	oil := maintenanceJobID(item.ID, "engine-oil")
	impeller := maintenanceJobID(item.ID, "impeller")

	// The profile's interval is in hours, so a reading is needed though the
	// job has no row.
	msg := proposeError(t, env.deps, fmt.Sprintf(`{"operations":[{"type":"maintenance_log","action":"create","fields":{"rule_id":%q,"performed_at":"2026-09-01"}}]}`, oil))
	if !strings.Contains(msg, "meter_reading") {
		t.Fatalf("got %q", msg)
	}
	// A slot has no hours interval, so it completes without one.
	p := env.propose(t, fmt.Sprintf(`{"operations":[{"type":"maintenance_log","action":"create","fields":{"rule_id":%q,"performed_at":"2026-09-01"}}]}`, impeller))
	env.advance()
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	entries, err := env.docs.ListMaintenanceLogEntries(maintenanceLogFilter{EquipmentID: item.ID})
	if err != nil || len(entries) != 1 || entries[0].RuleID == nil || *entries[0].RuleID != impeller {
		t.Fatalf("expected one entry on the job, got %+v %v", entries, err)
	}
}

func TestProposal_ATouchedJobMakesAnEarlierProposalStale(t *testing.T) {
	env := newProposalEnv(t)
	setupEngineProfiles(t, map[string]string{"a.json": scheduleProfileA})
	item := env.equipment(t, equipmentItem{Name: "Main engine", System: "propulsion", ProfileID: "sched-a"})
	job := maintenanceJobID(item.ID, "impeller")

	p := env.propose(t, fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"acknowledged_reason":"later"}}]}`, job))
	env.advance()
	if _, err := env.docs.AcknowledgeMaintenanceRule(job, "someone else got there first"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.apply(p.ID); !errors.Is(err, errAssistantProposalStale) {
		t.Fatalf("expected stale, got %v", err)
	}
}

func TestProposal_UpdateOnAProfileJobCanSetDueSoonAndFixedDate(t *testing.T) {
	env := newProposalEnv(t)
	setupEngineProfiles(t, map[string]string{"a.json": scheduleProfileA})
	item := env.equipment(t, equipmentItem{Name: "Main engine", System: "propulsion", ProfileID: "sched-a"})
	job := maintenanceJobID(item.ID, "engine-oil")

	p := env.propose(t, fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"due_soon_hours":20,"fixed_due_date":"2027-03-01"}}]}`, job))
	if !strings.Contains(p.Ops[0].Description, "due 1 Mar 2027") || !strings.Contains(p.Ops[0].Description, "due soon at 20 h") {
		t.Fatalf("summary %q", p.Ops[0].Description)
	}
	env.advance()
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	eff, err := env.docs.ResolveMaintenanceRule(job)
	if err != nil {
		t.Fatal(err)
	}
	if eff.DueSoonHours == nil || *eff.DueSoonHours != 20 || eff.FixedDueDate != "2027-03-01" || len(eff.OverriddenFields) != 0 {
		t.Fatalf("got %+v", eff)
	}
	p = env.propose(t, fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"fixed_due_date":null,"due_soon_hours":null}}]}`, job))
	env.advance()
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply clear: %v", err)
	}
	eff, _ = env.docs.ResolveMaintenanceRule(job)
	if eff.DueSoonHours != nil || eff.FixedDueDate != "" {
		t.Fatalf("clear failed: %+v", eff)
	}
}

func TestProposal_UpdateOnAProfileJobSetsAndClearsNotApplicable(t *testing.T) {
	env := newProposalEnv(t)
	setupEngineProfiles(t, map[string]string{"a.json": scheduleProfileA})
	item := env.equipment(t, equipmentItem{Name: "Main engine", System: "propulsion", ProfileID: "sched-a"})
	job := maintenanceJobID(item.ID, "impeller")

	p := env.propose(t, fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"not_applicable":true}}]}`, job))
	if got, want := p.Ops[0].Description, "Main engine · Raw water impeller: mark not applicable to this item"; got != want {
		t.Fatalf("summary %q, want %q", got, want)
	}
	env.advance()
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	eff, err := env.docs.ResolveMaintenanceRule(job)
	if err != nil {
		t.Fatal(err)
	}
	if !eff.NotApplicable || strings.Join(eff.OverriddenFields, ",") != "not_applicable" {
		t.Fatalf("expected not applicable, got %+v", eff)
	}

	// Setting it again is no change; clearing it is.
	msg := proposeError(t, env.deps, fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"not_applicable":true}}]}`, job))
	if !strings.Contains(msg, "exactly as it is") {
		t.Fatalf("got %q", msg)
	}
	env.advance()
	p = env.propose(t, fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"not_applicable":false}}]}`, job))
	if got, want := p.Ops[0].Description, "Main engine · Raw water impeller: applies to this item again"; got != want {
		t.Fatalf("summary %q, want %q", got, want)
	}
	env.advance()
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	eff, err = env.docs.ResolveMaintenanceRule(job)
	if err != nil {
		t.Fatal(err)
	}
	if eff.NotApplicable || len(eff.OverriddenFields) != 0 {
		t.Fatalf("expected the profile's own applicability, got %+v", eff)
	}
}

func TestProposal_NotApplicableIsRefusedOnAHandRule(t *testing.T) {
	env := newProposalEnv(t)
	gen := env.equipment(t, equipmentItem{Name: "Generator", System: "electrical"})
	rule := mustToolRule(t, env.docs, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Belts", IntervalMonths: iptr(12)})
	msg := proposeError(t, env.deps, fmt.Sprintf(`{"operations":[{"type":"maintenance_rule","action":"update","id":%q,"fields":{"not_applicable":true}}]}`, rule.ID))
	if !strings.Contains(msg, "not_applicable") {
		t.Fatalf("got %q", msg)
	}
}

