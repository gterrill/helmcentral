package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Mate's memory: lasting facts about the boat the skipper asked to keep.

func addMemory(t *testing.T, env *proposalEnv, text string) mateMemory {
	t.Helper()
	var m mateMemory
	err := env.docs.RunTx("test memory", func(tx *sql.Tx, now time.Time) error {
		var err error
		m, err = createMateMemoryTx(tx, text, now)
		return err
	})
	if err != nil {
		t.Fatalf("add %q: %v", text, err)
	}
	return m
}

func memoriesOf(t *testing.T, env *proposalEnv) []mateMemory {
	t.Helper()
	got, err := readMateMemories(env.docs)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestMateMemory_AddEditDelete(t *testing.T) {
	env := newProposalEnv(t)
	m := addMemory(t, env, "  The fuel polisher has no sampling port on its discharge manifold.  ")
	if m.Text != "The fuel polisher has no sampling port on its discharge manifold." || m.ID == "" {
		t.Fatalf("added %+v", m)
	}
	env.advance()
	err := env.docs.RunTx("edit", func(tx *sql.Tx, now time.Time) error {
		_, err := updateMateMemoryTx(tx, m.ID, "The fuel polisher has a sampling port on its suction side only.", now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	got := memoriesOf(t, env)
	if len(got) != 1 || !strings.Contains(got[0].Text, "suction side only") || !got[0].UpdatedAt.After(got[0].CreatedAt) {
		t.Fatalf("after edit %+v", got)
	}
	if err := env.docs.RunTx("delete", func(tx *sql.Tx, _ time.Time) error { return deleteMateMemoryTx(tx, m.ID) }); err != nil {
		t.Fatal(err)
	}
	if got := memoriesOf(t, env); len(got) != 0 {
		t.Fatalf("after delete %+v", got)
	}
	err = env.docs.RunTx("delete again", func(tx *sql.Tx, _ time.Time) error { return deleteMateMemoryTx(tx, m.ID) })
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("deleting a missing memory: %v", err)
	}
}

func TestMateMemory_TextIsCheckedWithAPlainError(t *testing.T) {
	env := newProposalEnv(t)
	for name, text := range map[string]string{
		"empty":     "   ",
		"too long":  strings.Repeat("x", maxMateMemoryChars+1),
		"line feed": "first line\nsecond line",
	} {
		err := env.docs.RunTx("add", func(tx *sql.Tx, now time.Time) error {
			_, err := createMateMemoryTx(tx, text, now)
			return err
		})
		if err == nil {
			t.Errorf("%s should be refused", name)
		}
	}
	if err := env.docs.RunTx("add", func(tx *sql.Tx, now time.Time) error {
		_, err := createMateMemoryTx(tx, strings.Repeat("x", maxMateMemoryChars), now)
		return err
	}); err != nil {
		t.Errorf("exactly %d characters is allowed: %v", maxMateMemoryChars, err)
	}
}

func TestMateMemory_TheListHoldsAtMostFiftyAndNeverDropsOne(t *testing.T) {
	env := newProposalEnv(t)
	for i := 0; i < maxMateMemories; i++ {
		addMemory(t, env, "Fact number "+string(rune('A'+i%26))+strings.Repeat("z", i))
	}
	err := env.docs.RunTx("one too many", func(tx *sql.Tx, now time.Time) error {
		_, err := createMateMemoryTx(tx, "One more fact", now)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "remove one") {
		t.Fatalf("a full list says to remove one: %v", err)
	}
	if got := memoriesOf(t, env); len(got) != maxMateMemories {
		t.Errorf("the list holds %d, want %d", len(got), maxMateMemories)
	}
}

func TestMateMemory_TheTableIsCreatedWithTheAssistantSchema(t *testing.T) {
	env := newProposalEnv(t)
	var n int
	if err := env.asst.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'mate_memories'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("mate_memories table: %d %v", n, err)
	}
	// Creating the schema again changes nothing.
	if err := createAssistantSchema(env.asst.db); err != nil {
		t.Fatal(err)
	}
}

func TestUserAsksToRemember(t *testing.T) {
	yes := []string{
		"Remember that the fuel polisher has no sampling port.",
		"remember this: the port tank sender reads 10% high",
		"Please remember the Racor bowl drains to the port bilge.",
		"Keep in mind that the watermaker is off in the anchorage.",
		"Make a note that the stern light is LED only.",
		"Don't forget that the dinghy engine needs mixed fuel.",
		"Ok. Remember it.",
	}
	no := []string{
		"What is the fuel polisher's discharge like?",
		"Do you remember what I said about the polisher?",
		"We're at anchor in Tongue Bay.",
		"I remembered the sampling port after all",
		"I can't remember that filter's part number",
		"I don't remember the filter's part number, remember?",
		"Did you remember the filter?",
		"Do you remember that filter's part number",
		"",
	}
	for _, s := range yes {
		if !userAsksToRemember(s) {
			t.Errorf("%q asks to remember", s)
		}
	}
	for _, s := range no {
		if userAsksToRemember(s) {
			t.Errorf("%q does not ask to remember", s)
		}
	}
}

func rememberArgs(fact, replaces string) json.RawMessage {
	raw, _ := json.Marshal(map[string]string{"fact": fact, "replaces": replaces})
	return raw
}

func runRemember(t *testing.T, env *proposalEnv, operator string, args json.RawMessage) (rememberResult, error) {
	t.Helper()
	ctx := context.Background()
	if operator != "" {
		ctx = withSkipperMessage(ctx, operator)
	}
	raw, _, err := env.deps.execute(ctx, "remember_fact", args)
	if err != nil {
		return rememberResult{}, err
	}
	var res rememberResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("result: %v (%s)", err, raw)
	}
	return res, nil
}

func TestRememberFact_SavesStraightAwayWhenTheSkipperAskedToRemember(t *testing.T) {
	env := newProposalEnv(t)
	res, err := runRemember(t, env, "Remember that the fuel polisher has no sampling port.", rememberArgs("The fuel polisher has no sampling port on its discharge manifold.", ""))
	if err != nil {
		t.Fatal(err)
	}
	// The fact commits with the reply that carries its card, not before.
	env.saveOnMessage(t, "Kept.", *res.Proposal)
	got := memoriesOf(t, env)
	if len(got) != 1 || !strings.Contains(got[0].Text, "no sampling port") {
		t.Fatalf("saved %+v", got)
	}
	if !res.Saved || res.Proposal.Status != assistantProposalApplied || res.MemoryID != got[0].ID {
		t.Errorf("result %+v", res)
	}
}

func TestRememberFact_AFailedRunLeavesNoFact(t *testing.T) {
	env := newProposalEnv(t)
	res, err := runRemember(t, env, "Remember that the fuel polisher has no sampling port.", rememberArgs("The fuel polisher has no sampling port on its discharge manifold.", ""))
	if err != nil || !res.Saved {
		t.Fatalf("remember: %v %+v", err, res)
	}
	// The run then fails or is stopped: no assistant message is saved, so no
	// card exists to undo a fact. None must be kept either.
	if got := memoriesOf(t, env); len(got) != 0 {
		t.Fatalf("a fact outlived a run that saved no reply: %+v", got)
	}
}

func TestRememberFact_TheFactAndItsCardCommitTogether(t *testing.T) {
	env := newProposalEnv(t)
	res, _ := runRemember(t, env, "Remember that the hatch sticks.", rememberArgs("The hatch sticks.", ""))
	// A reply that cannot be saved (here: an unknown conversation) keeps no fact.
	if _, err := env.asst.AppendMessage(assistantMessage{ConversationID: "no-such-chat", Role: "assistant", Content: "Kept.", Proposals: []assistantProposal{*res.Proposal}}); err == nil {
		t.Fatal("the reply should not save")
	}
	if got := memoriesOf(t, env); len(got) != 0 {
		t.Fatalf("a fact was kept without its card: %+v", got)
	}
	msg := env.saveOnMessage(t, "Kept.", *res.Proposal)
	got := memoriesOf(t, env)
	if len(got) != 1 || got[0].ID != res.MemoryID {
		t.Fatalf("the fact keeps the id its card names: %+v want %s", got, res.MemoryID)
	}
	msgs, _ := env.asst.ListMessages(msg.ConversationID)
	if len(msgs[0].Proposals) != 1 || !strings.Contains(string(msgs[0].Proposals[0].Result), got[0].ID) {
		t.Errorf("the card names the kept fact: %+v", msgs[0].Proposals)
	}
}

func TestRememberFact_AFactThatCannotCommitNeverCostsTheReply(t *testing.T) {
	cases := []struct {
		name       string
		preload    int
		facts      [2]string
		wantReason string
	}{
		{"full list", maxMateMemories - 1, [2]string{"The hatch sticks.", "The locker leaks."}, "remove one"},
		{"same fact twice", 0, [2]string{"The hatch sticks.", "The hatch sticks."}, "already kept"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newProposalEnv(t)
			for i := 0; i < tc.preload; i++ {
				addMemory(t, env, "Fact "+strings.Repeat("q", i+1))
			}
			var cards []assistantProposal
			for _, f := range tc.facts {
				res, err := runRemember(t, env, "Remember that "+f, rememberArgs(f, ""))
				if err != nil || !res.Saved {
					t.Fatalf("remember %q: %v %+v", f, err, res)
				}
				cards = append(cards, *res.Proposal)
			}
			msg, err := env.asst.AppendMessage(assistantMessage{ConversationID: env.conv.ID, Role: "assistant", Content: "Kept both.", Proposals: cards})
			if err != nil {
				t.Fatalf("the reply must still save: %v", err)
			}
			if got := memoriesOf(t, env); len(got) != tc.preload+1 {
				t.Errorf("kept %d facts, want %d", len(got), tc.preload+1)
			}
			msgs, _ := env.asst.ListMessages(msg.ConversationID)
			ps := msgs[len(msgs)-1].Proposals
			if len(ps) != 2 || ps[0].Status != assistantProposalApplied || ps[1].Status != assistantProposalStale {
				t.Fatalf("want one applied and one stale card: %+v", ps)
			}
			if !strings.Contains(ps[1].StaleReason, tc.wantReason) || ps[1].ResolvedAt == nil {
				t.Errorf("stale reason %q", ps[1].StaleReason)
			}
			if _, err := undoAssistantProposal(env.asst, env.docs, ps[1].ID); err == nil {
				t.Error("a card that kept nothing has nothing to undo")
			}
		})
	}
}

func TestRememberFact_OnlyTheSkippersOwnWordsAreSavedAtOnce(t *testing.T) {
	cases := []struct {
		name, said, fact string
		saved            bool
	}{
		{"the fact is what they said", "Remember that the tank holds 400 L. What's my endurance?", "the tank holds 400 L", true},
		{"Mate's own conclusion in the same message", "Remember that the tank holds 400 L. What's my endurance?", "Endurance is 40 hours.", false},
		{"a document's text", "Remember that. Here is the manual page.", "The pump flow is 12 L/min.", false},
		{"a negated ask", "I can't remember that filter's part number", "The filter part number is 4521.", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newProposalEnv(t)
			res, err := runRemember(t, env, tc.said, rememberArgs(tc.fact, ""))
			if err != nil {
				t.Fatal(err)
			}
			if res.Saved != tc.saved {
				t.Errorf("saved = %v, want %v (%+v)", res.Saved, tc.saved, res)
			}
			if !tc.saved && (res.Proposal == nil || res.Proposal.Status != assistantProposalPending) {
				t.Errorf("an unsaved fact becomes a card: %+v", res)
			}
		})
	}
}

func TestFactInSkipperWords(t *testing.T) {
	cases := []struct {
		said, fact string
		want       bool
	}{
		{"Remember that the tank holds 400 L.", "the tank holds 400 L", true},
		{"Remember that the tank holds 400 L.", "The tank holds 400 litres", true},
		{"Remember that the tank holds 400 L.", "The tank holds 40 litres", false},
		{"Remember that the tank holds 400 L.", "The tank holds 400 litres and the endurance is 40 hours", false},
		{"Remember that the tanks hold 400 L", "The tank holds 400 L.", true},
		{"Remember that the fuel polisher has no sampling port.", "Endurance is 40 hours.", false},
		{"Remember that the polisher has no sampling port", "The polisher has a sampling port", false},
		{"Remember that the polisher has a sampling port", "The polisher has no sampling port", false},
		{"Remember that.", "The hatch sticks.", false},
		{"remember that the hatch sticks", "", false},
		{"remember that the hatch sticks", "the", false},
	}
	for _, tc := range cases {
		if got := factInSkipperWords(tc.said, tc.fact); got != tc.want {
			t.Errorf("said %q, fact %q: got %v, want %v", tc.said, tc.fact, got, tc.want)
		}
	}
}

func TestRememberFact_ASavedFactShowsInTheThreadAndCanBeUndone(t *testing.T) {
	env := newProposalEnv(t)
	res, err := runRemember(t, env, "remember that the polisher has no port", rememberArgs("The polisher has no port.", ""))
	if err != nil {
		t.Fatal(err)
	}
	msg := env.saveOnMessage(t, "Kept.", *res.Proposal)
	msgs, err := env.asst.ListMessages(msg.ConversationID)
	if err != nil || len(msgs) != 1 || len(msgs[0].Proposals) != 1 {
		t.Fatalf("thread %v %v", msgs, err)
	}
	card := msgs[0].Proposals[0]
	if card.Status != assistantProposalApplied || card.ResolvedAt == nil || !strings.Contains(string(card.Result), res.MemoryID) {
		t.Errorf("the card shows it saved, with the memory to undo: %+v", card)
	}
	c, rec := watchRequestTo(http.MethodDelete, "/api/assistant/memories/"+res.MemoryID, "", "id", res.MemoryID)
	if err := deleteMateMemoryHandler(c); err != nil || rec.Code != http.StatusNoContent {
		t.Fatalf("undo: %v %d", err, rec.Code)
	}
	if got := memoriesOf(t, env); len(got) != 0 {
		t.Errorf("after undo %+v", got)
	}
}

func TestRememberFact_WithoutAnAskItBecomesAProposalTheOperatorApplies(t *testing.T) {
	env := newProposalEnv(t)
	for _, operator := range []string{"", "Why does the polisher not have a port?", "We're at anchor."} {
		res, err := runRemember(t, env, operator, rememberArgs("The polisher has no port.", ""))
		if err != nil {
			t.Fatal(err)
		}
		if res.Saved || res.Proposal.Status != assistantProposalPending {
			t.Fatalf("%q: Mate's own conclusion must not be saved silently: %+v", operator, res)
		}
	}
	if got := memoriesOf(t, env); len(got) != 0 {
		t.Fatalf("nothing is saved by a proposal alone: %+v", got)
	}
	res, _ := runRemember(t, env, "", rememberArgs("The polisher has no port.", ""))
	env.saveOnMessage(t, "Shall I keep that?", *res.Proposal)
	applied, err := env.apply(res.Proposal.ID)
	if err != nil || applied.Status != assistantProposalApplied {
		t.Fatalf("apply: %v %+v", err, applied)
	}
	got := memoriesOf(t, env)
	if len(got) != 1 || got[0].Text != "The polisher has no port." {
		t.Errorf("after Apply %+v", got)
	}
}

func TestRememberFact_AReplacementIsAlwaysAProposalShowingBoth(t *testing.T) {
	env := newProposalEnv(t)
	old := addMemory(t, env, "The polisher has a sampling port.")
	res, err := runRemember(t, env, "Remember that the polisher has NO sampling port.", rememberArgs("The polisher has no sampling port.", old.ID))
	if err != nil {
		t.Fatal(err)
	}
	if res.Saved || res.Proposal.Status != assistantProposalPending {
		t.Fatalf("a replacement waits for the tap even after remember that: %+v", res)
	}
	if got := memoriesOf(t, env); len(got) != 1 || got[0].Text != "The polisher has a sampling port." {
		t.Fatalf("the old memory is untouched until Apply: %+v", got)
	}
	op := res.Proposal.Ops[0]
	if op.Action != changeUpdate || op.ID != old.ID || !strings.Contains(op.Description, "no sampling port") {
		t.Errorf("op %+v", op)
	}
	if !strings.Contains(op.Before["text"].(string), "has a sampling port") || !strings.Contains(op.After["text"].(string), "no sampling port") {
		t.Errorf("the card shows both: before %v after %v", op.Before, op.After)
	}
	env.saveOnMessage(t, "Replace it?", *res.Proposal)
	if _, err := env.apply(res.Proposal.ID); err != nil {
		t.Fatal(err)
	}
	got := memoriesOf(t, env)
	if len(got) != 1 || got[0].ID != old.ID || got[0].Text != "The polisher has no sampling port." {
		t.Errorf("after Apply %+v", got)
	}
}

func TestRememberFact_ReplacingAMissingMemoryIsAnError(t *testing.T) {
	env := newProposalEnv(t)
	if _, err := runRemember(t, env, "remember that x is y", rememberArgs("X is y.", "no-such-id")); err == nil {
		t.Fatal("a replacement of a memory that is not there is refused")
	}
}

func TestRememberFact_AFullListFailsWithAMessageAndKeepsEveryFact(t *testing.T) {
	env := newProposalEnv(t)
	for i := 0; i < maxMateMemories; i++ {
		addMemory(t, env, "Fact "+strings.Repeat("q", i+1))
	}
	for _, operator := range []string{"remember that the hatch sticks", ""} {
		_, err := runRemember(t, env, operator, rememberArgs("The hatch sticks.", ""))
		if err == nil || !strings.Contains(err.Error(), "remove one") {
			t.Errorf("%q: a full list says to remove one: %v", operator, err)
		}
	}
	if got := memoriesOf(t, env); len(got) != maxMateMemories {
		t.Errorf("Mate dropped a fact: %d left", len(got))
	}
}

func TestRememberFact_ATooLongFactIsAPlainError(t *testing.T) {
	env := newProposalEnv(t)
	_, err := runRemember(t, env, "remember that", rememberArgs(strings.Repeat("x", maxMateMemoryChars+1), ""))
	if err == nil || !strings.Contains(err.Error(), "300") {
		t.Errorf("error: %v", err)
	}
}

func TestSkipperMessage_IsTheTriggeringUserRowOnly(t *testing.T) {
	user := assistantMessage{Role: "user", Content: "Remember that the hatch sticks."}
	if got := skipperMessage([]assistantMessage{{Role: "assistant", Content: "hi"}, user}); got != user.Content {
		t.Errorf("the skipper's own words were missed: %q", got)
	}
	// An end-of-watch turn is triggered by a report, not by the skipper.
	if got := skipperMessage([]assistantMessage{user, {Role: "watch", Content: "Remember that the report says so."}}); got != "" {
		t.Errorf("a watch report is not the skipper: %q", got)
	}
	if got := skipperMessage(nil); got != "" {
		t.Errorf("no rows has no words: %q", got)
	}
}

func TestMateMemory_RecordTypeIsRegisteredForProposals(t *testing.T) {
	typ, ok := defaultRecordRegistry.lookup(recordTypeMateMemory)
	if !ok || !typ.allows(changeCreate) || !typ.allows(changeUpdate) || !typ.allows(changeDelete) {
		t.Fatalf("memory record type: %v %+v", ok, typ)
	}
}

// Prompts.

func TestMemoriesReachTheChatPromptInTheStablePart(t *testing.T) {
	when := time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)
	pc := assistantPromptContext{Memories: []mateMemory{{ID: "m1", Text: "The polisher has no sampling port.", CreatedAt: when}}}
	stable, live := assistantSystemPromptParts(pc)
	for _, want := range []string{"The polisher has no sampling port.", "m1", "2 Nov 2026", "asked you to keep"} {
		if !strings.Contains(stable, want) {
			t.Errorf("the stable prompt lacks %q", want)
		}
	}
	if strings.Contains(live, "sampling port") {
		t.Error("memories belong in the stable part, where caching helps")
	}
	for _, want := range []string{"remember_fact", "current state", "at anchor"} {
		if !strings.Contains(stable, want) {
			t.Errorf("the stable prompt lacks the rule %q", want)
		}
	}
}

func TestMemoriesReachTheReviewPrompt(t *testing.T) {
	pc := assistantPromptContext{Memories: []mateMemory{{ID: "m1", Text: "The freezer runs all night.", CreatedAt: time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)}}}
	prompt := buildWatchReviewPrompt(pc, "metric", watchTestTime(29, 6, 0), time.UTC, false)
	if !strings.Contains(prompt, "The freezer runs all night.") || !strings.Contains(prompt, "asked you to keep") {
		t.Errorf("the review prompt lacks the memories:\n%s", prompt)
	}
	none := buildWatchReviewPrompt(assistantPromptContext{}, "metric", watchTestTime(29, 6, 0), time.UTC, false)
	if strings.Contains(none, "asked you to keep") {
		t.Error("no memories, no section")
	}
}

func TestCollectedPromptContextCarriesTheMemories(t *testing.T) {
	env := newProposalEnv(t)
	addMemory(t, env, "The port tank sender reads 10% high.")
	pc := collectAssistantPromptContext(writeAssistantSettingsFixture(t, true, "test/model"), time.Now())
	if len(pc.Memories) != 1 || !strings.Contains(pc.Memories[0].Text, "10% high") {
		t.Errorf("memories %+v", pc.Memories)
	}
}

func TestReviewsReadMemoriesButCannotSaveThem(t *testing.T) {
	if watchReviewReadTools["remember_fact"] || alarmReviewReadTool("remember_fact") {
		t.Fatal("an unattended review must not be able to save a memory")
	}
	tools := &watchReviewTools{assistantToolDeps: assistantToolDeps{}}
	for _, def := range tools.definitions() {
		if def.Function.Name == "remember_fact" {
			t.Error("the save tool is offered to a review")
		}
	}
	if _, _, err := tools.execute(context.Background(), "remember_fact", rememberArgs("x", "")); err == nil {
		t.Error("a review that calls remember_fact is refused")
	}
}

// The outside assistants' context.

func TestBoatContextListsTheMemoriesReadOnly(t *testing.T) {
	d := contextTestDeps(t)
	if err := globalDocumentStore.RunTx("seed", func(tx *sql.Tx, now time.Time) error {
		_, err := createMateMemoryTx(tx, "The fuel polisher has no sampling port.", now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	md, err := buildBoatContext(d, mcpAllOn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "## Facts the skipper asked Mate to keep") || !strings.Contains(md, "The fuel polisher has no sampling port.") {
		t.Errorf("the context lacks the memories:\n%s", md)
	}
	if e, ok := mcpPolicyFor("remember_fact"); !ok || e.Status != mcpNever {
		t.Errorf("no outside tool may change a memory: %+v %v", e, ok)
	}
}

func TestMateMemory_TheDayIsTheBoatsDayEverywhere(t *testing.T) {
	// 21:30 on 2 November at UTC-5 is already 3 November in UTC.
	loc := time.FixedZone("UTC-5", -5*3600)
	at := time.Date(2026, 11, 3, 2, 30, 0, 0, time.UTC)
	m := mateMemory{ID: "m1", Text: "The hatch sticks.", CreatedAt: at, UpdatedAt: at}

	snap := mateMemorySnapshot(m, loc)
	if got := snap.Fields["kept_at_iso"]; got != "2026-11-03T02:30:00Z" {
		t.Errorf("kept_at_iso = %v, want the UTC instant", got)
	}
	if got := snap.Fields["kept_on"]; got != "2 Nov 2026" {
		t.Errorf("kept_on = %v, want the boat's day", got)
	}
	if section := mateMemoriesPromptSection([]mateMemory{m}, loc, true); !strings.Contains(section, "kept 2 Nov 2026") {
		t.Errorf("the prompt and the record must agree on the day:\n%s", section)
	}
}

// Undo is a server action on the card's proposal.

func keptFactCard(t *testing.T, env *proposalEnv, text string) (assistantProposal, mateMemory) {
	t.Helper()
	res, err := runRemember(t, env, "Remember that "+text, rememberArgs(text, ""))
	if err != nil || !res.Saved {
		t.Fatalf("remember: %v %+v", err, res)
	}
	env.saveOnMessage(t, "Kept.", *res.Proposal)
	return *res.Proposal, memoriesOf(t, env)[0]
}

func TestUndoProposal_RemovesTheFactAndRecordsUndone(t *testing.T) {
	env := newProposalEnv(t)
	card, fact := keptFactCard(t, env, "the hatch sticks")
	got, err := undoAssistantProposal(env.asst, env.docs, card.ID)
	if err != nil || got.Status != assistantProposalUndone {
		t.Fatalf("undo: %v %+v", err, got)
	}
	if left := memoriesOf(t, env); len(left) != 0 {
		t.Errorf("the fact %s is still kept: %+v", fact.ID, left)
	}
	stored, _ := env.asst.GetProposal(card.ID)
	if stored.Status != assistantProposalUndone {
		t.Errorf("a reload shows %q, want undone", stored.Status)
	}
	// Undoing twice is the same answer; applying or dismissing it now is refused.
	if again, err := undoAssistantProposal(env.asst, env.docs, card.ID); err != nil || again.Status != assistantProposalUndone {
		t.Errorf("second undo: %v %+v", err, again)
	}
	if _, err := env.apply(card.ID); err == nil {
		t.Error("an undone proposal cannot be applied again")
	}
	if _, err := env.dismiss(card.ID); err == nil {
		t.Error("an undone proposal cannot be dismissed")
	}
	if left := memoriesOf(t, env); len(left) != 0 {
		t.Errorf("applying an undone proposal brought the fact back: %+v", left)
	}
}

func TestUndoProposal_RefusesAFactChangedSinceItWasKept(t *testing.T) {
	env := newProposalEnv(t)
	card, fact := keptFactCard(t, env, "the hatch sticks")
	env.advance()
	if err := env.docs.RunTx("rewrite", func(tx *sql.Tx, now time.Time) error {
		_, err := updateMateMemoryTx(tx, fact.ID, "The forward hatch sticks when wet.", now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	_, err := undoAssistantProposal(env.asst, env.docs, card.ID)
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("undo of a rewritten fact: %v", err)
	}
	if left := memoriesOf(t, env); len(left) != 1 || left[0].Text != "The forward hatch sticks when wet." {
		t.Errorf("the rewritten fact must stay: %+v", left)
	}
	if stored, _ := env.asst.GetProposal(card.ID); stored.Status != assistantProposalApplied {
		t.Errorf("a refused undo leaves the card %q", stored.Status)
	}
}

func TestUndoProposal_AFactAlreadyRemovedIsUndone(t *testing.T) {
	env := newProposalEnv(t)
	card, fact := keptFactCard(t, env, "the hatch sticks")
	if err := env.docs.RunTx("remove", func(tx *sql.Tx, _ time.Time) error { return deleteMateMemoryTx(tx, fact.ID) }); err != nil {
		t.Fatal(err)
	}
	got, err := undoAssistantProposal(env.asst, env.docs, card.ID)
	if err != nil || got.Status != assistantProposalUndone {
		t.Fatalf("undo: %v %+v", err, got)
	}
}

func TestUndoProposal_OnlyAnAppliedFactCanBeUndone(t *testing.T) {
	env := newProposalEnv(t)
	res, _ := runRemember(t, env, "", rememberArgs("The hatch sticks.", ""))
	env.saveOnMessage(t, "Keep it?", *res.Proposal)
	if _, err := undoAssistantProposal(env.asst, env.docs, res.Proposal.ID); err == nil {
		t.Error("a pending proposal has nothing to undo")
	}
	if _, err := undoAssistantProposal(env.asst, env.docs, "no-such-proposal"); err == nil {
		t.Error("an unknown proposal cannot be undone")
	}
	other := env.propose(t, `{"operations":[{"type":"maintenance_rule","action":"create","fields":{"description":"Registration renewal","interval_months":12}}]}`)
	if _, err := env.apply(other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := undoAssistantProposal(env.asst, env.docs, other.ID); err == nil {
		t.Error("only a kept fact can be undone")
	}
}

func TestProposalsTableMadeBeforeUndoIsRebuiltKeepingEveryRow(t *testing.T) {
	env := newProposalEnv(t)
	db := env.asst.db
	card, _ := keptFactCard(t, env, "the hatch sticks")
	// Put the table back as it was before 'undone' existed.
	for _, stmt := range []string{
		`ALTER TABLE assistant_message_proposals RENAME TO proposals_new`,
		strings.Replace(assistantProposalsTableDDL, ", 'undone'", "", 1),
		`INSERT INTO assistant_message_proposals SELECT * FROM proposals_new`,
		`DROP TABLE proposals_new`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if _, err := db.Exec(`UPDATE assistant_message_proposals SET status = 'undone'`); err == nil {
		t.Fatal("the old table should refuse 'undone'")
	}
	if err := createAssistantProposalsSchema(db); err != nil {
		t.Fatal(err)
	}
	got, err := env.asst.GetProposal(card.ID)
	if err != nil || got.Status != assistantProposalApplied {
		t.Fatalf("the row survived the rebuild: %v %+v", err, got)
	}
	if _, err := db.Exec(`UPDATE assistant_message_proposals SET status = 'undone'`); err != nil {
		t.Errorf("the rebuilt table takes 'undone': %v", err)
	}
}

func TestUndoProposalHandler_AnswersTheProposalOrAPlainRefusal(t *testing.T) {
	env := newProposalEnv(t)
	card, fact := keptFactCard(t, env, "the hatch sticks")
	c, rec := watchRequestTo(http.MethodPost, "/api/assistant/proposals/"+card.ID+"/undo", "", "id", card.ID)
	if err := undoAssistantProposalHandler(c); err != nil || rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"undone"`) {
		t.Fatalf("undo: %v %d %s", err, rec.Code, rec.Body)
	}

	card2, fact2 := keptFactCard(t, env, "the bilge pump is on the port side")
	_ = fact
	env.advance()
	_ = env.docs.RunTx("rewrite", func(tx *sql.Tx, now time.Time) error {
		_, err := updateMateMemoryTx(tx, fact2.ID, "The bilge pump is forward.", now)
		return err
	})
	c, rec = watchRequestTo(http.MethodPost, "/api/assistant/proposals/"+card2.ID+"/undo", "", "id", card2.ID)
	if err := undoAssistantProposalHandler(c); err != nil || rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "changed") {
		t.Fatalf("a changed fact is a 409 with a reason: %v %d %s", err, rec.Code, rec.Body)
	}
}

func TestProposalHistoryTellsMateAnUndoneFactIsNotKept(t *testing.T) {
	block := assistantProposalHistoryBlock(assistantProposal{Status: assistantProposalUndone, Ops: []changeOp{{Description: "Keep this fact: The hatch sticks."}}})
	if !strings.Contains(block, "undid") || !strings.Contains(block, "NOT") {
		t.Errorf("history block:\n%s", block)
	}
}

// A failed read of the facts is said in the prompt, never read as an empty list.

func TestAnUnreadableMemoryIsSaidInEveryPrompt(t *testing.T) {
	pc := assistantPromptContext{MemoriesUnreadable: true}
	stable, _ := assistantSystemPromptParts(pc)
	prompts := map[string]string{
		"chat":        stable,
		"watch":       buildWatchReviewPrompt(pc, "metric", watchTestTime(29, 6, 0), time.UTC, false),
		"alarm":       buildAlarmReviewPrompt(pc, "metric", watchTestTime(29, 6, 0), time.UTC, true),
		"watch clear": buildWatchReviewPrompt(assistantPromptContext{}, "metric", watchTestTime(29, 6, 0), time.UTC, false),
	}
	for name, p := range prompts {
		said := strings.Contains(p, "could not be read")
		if name == "watch clear" && said {
			t.Errorf("%s: a readable empty list says nothing", name)
		}
		if name != "watch clear" && !said {
			t.Errorf("%s prompt does not say the kept facts could not be read", name)
		}
	}
}

func TestCollectedPromptContextFlagsAnUnreadableMemory(t *testing.T) {
	env := newProposalEnv(t)
	if _, err := env.docs.db.Exec(`DROP TABLE mate_memories`); err != nil {
		t.Fatal(err)
	}
	pc := collectAssistantPromptContext(writeAssistantSettingsFixture(t, true, "test/model"), time.Now())
	if !pc.MemoriesUnreadable {
		t.Error("a failed read of the facts must be flagged")
	}
}

func TestReviewPromptsCarryNoFactIdsButTheChatPromptDoes(t *testing.T) {
	pc := assistantPromptContext{Memories: []mateMemory{{ID: "0b6d6c1e-id", Text: "The freezer runs all night.", CreatedAt: time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)}}}
	stable, _ := assistantSystemPromptParts(pc)
	if !strings.Contains(stable, "0b6d6c1e-id") {
		t.Error("the chat prompt needs the ids for replaces")
	}
	for name, p := range map[string]string{
		"watch": buildWatchReviewPrompt(pc, "metric", watchTestTime(29, 6, 0), time.UTC, false),
		"alarm": buildAlarmReviewPrompt(pc, "metric", watchTestTime(29, 6, 0), time.UTC, true),
	} {
		if !strings.Contains(p, "The freezer runs all night.") {
			t.Errorf("%s review lost the fact", name)
		}
		if strings.Contains(p, "0b6d6c1e-id") || strings.Contains(p, "each with its id") {
			t.Errorf("%s review carries a fact id it cannot act on:\n%s", name, p)
		}
	}
}

// REST.

func TestMateMemoriesREST_ListAddEditDelete(t *testing.T) {
	env := newProposalEnv(t)

	c, rec := watchRequestTo(http.MethodPost, "/api/assistant/memories", `{"text":"The hatch sticks when wet."}`)
	if err := createMateMemoryHandler(c); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("create: %v %d %s", err, rec.Code, rec.Body)
	}
	var created mateMemory
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("created %s", rec.Body)
	}

	c, rec = watchRequestTo(http.MethodGet, "/api/assistant/memories", "")
	if err := listMateMemoriesHandler(c); err != nil || rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "The hatch sticks when wet.") {
		t.Fatalf("list: %v %d %s", err, rec.Code, rec.Body)
	}

	c, rec = watchRequestTo(http.MethodPatch, "/api/assistant/memories/"+created.ID, `{"text":"The forward hatch sticks when wet."}`, "id", created.ID)
	if err := patchMateMemoryHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("patch: %v %d %s", err, rec.Code, rec.Body)
	}
	if got := memoriesOf(t, env); len(got) != 1 || got[0].Text != "The forward hatch sticks when wet." {
		t.Fatalf("after edit %+v", got)
	}

	c, rec = watchRequestTo(http.MethodPost, "/api/assistant/memories", `{"text":"`+strings.Repeat("x", maxMateMemoryChars+1)+`"}`)
	if err := createMateMemoryHandler(c); err != nil || rec.Code != http.StatusBadRequest {
		t.Fatalf("a long fact is a 400: %d %s", rec.Code, rec.Body)
	}

	c, rec = watchRequestTo(http.MethodDelete, "/api/assistant/memories/"+created.ID, "", "id", created.ID)
	if err := deleteMateMemoryHandler(c); err != nil || rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %v %d", err, rec.Code)
	}
	c, rec = watchRequestTo(http.MethodDelete, "/api/assistant/memories/"+created.ID, "", "id", created.ID)
	if err := deleteMateMemoryHandler(c); err != nil || rec.Code != http.StatusNotFound {
		t.Fatalf("a second delete is a 404: %d", rec.Code)
	}
}

func TestMateMemoriesREST_FullListIsAConflict(t *testing.T) {
	env := newProposalEnv(t)
	for i := 0; i < maxMateMemories; i++ {
		addMemory(t, env, "Fact "+strings.Repeat("q", i+1))
	}
	c, rec := watchRequestTo(http.MethodPost, "/api/assistant/memories", `{"text":"One more."}`)
	if err := createMateMemoryHandler(c); err != nil || rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "remove one") {
		t.Fatalf("full: %d %s", rec.Code, rec.Body)
	}
}

func TestMateMemoryRoutes_ReadSessionsCanOnlyRead(t *testing.T) {
	want := map[string]apiTier{
		http.MethodGet + " /api/assistant/memories":        tierRead,
		http.MethodPost + " /api/assistant/memories":       tierWrite,
		http.MethodPatch + " /api/assistant/memories/:id":  tierWrite,
		http.MethodDelete + " /api/assistant/memories/:id": tierWrite,
	}
	for _, r := range mateMemoryRoutes() {
		key := r.Method + " " + r.Path
		tier, ok := want[key]
		if !ok || r.Tier != tier {
			t.Errorf("%s: tier %v", key, r.Tier)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Errorf("missing routes: %v", want)
	}
}

// A conversation made from a run, over HTTP.

func TestCreateConversationHandler_StoresTheRunLink(t *testing.T) {
	env := newProposalEnv(t)
	c, rec := watchRequestTo(http.MethodPost, "/api/assistant/conversations", `{"watch_run_id":"run-7"}`)
	if err := createAssistantConversationHandler(c); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("create: %v %d %s", err, rec.Code, rec.Body)
	}
	var conv assistantConversation
	if err := json.Unmarshal(rec.Body.Bytes(), &conv); err != nil {
		t.Fatal(err)
	}
	got, _, _ := env.asst.GetConversation(conv.ID)
	if got.WatchRunID == nil || *got.WatchRunID != "run-7" {
		t.Errorf("link %v", got.WatchRunID)
	}
}
