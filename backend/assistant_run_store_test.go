package main

import (
	"strings"
	"testing"
	"time"
)

func sampleRunTrace(convID string, afterSeq int, answerID *string, stop string) assistantRunTrace {
	return assistantRunTrace{
		ConversationID:  convID,
		AfterSeq:        afterSeq,
		AnswerMessageID: answerID,
		Model:           "anthropic/claude-sonnet-4.5",
		StartedAt:       time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC),
		DurationMs:      133000,
		StopReason:      stop,
		Error:           "",
		Budget:          currentAssistantTraceBudget(),
		Rounds: []assistantTraceRound{
			{Index: 0, Kind: assistantRoundLookups, StartOffsetMs: 0, LatencyMs: 4100, PromptTokens: 900, CompletionTokens: 40, CachedTokens: 600, CostUSD: 0.004, Progress: true,
				Calls: []assistantToolCall{
					{Seq: 0, Name: "find_places", Args: `{"query":"Cairns"}`, ElapsedMs: 2300, Result: strings.Repeat("r", 812), Cache: "miss"},
					{Seq: 1, Name: "get_tides", Args: `{"lat":1}`, ElapsedMs: 90, Error: "tide station offline"},
				}},
			{Index: 1, Kind: assistantRoundFinal, StartOffsetMs: 6600, LatencyMs: 9000, ForcedFinal: true, Error: "context deadline exceeded"},
		},
	}
}

func TestAssistantRunStore_SaveAndGetKeepsEveryRoundAndCall(t *testing.T) {
	store := newTestAssistantStore(t)
	conv, _ := store.CreateConversation("Passage")
	in := sampleRunTrace(conv.ID, 1, nil, assistantStopTimeCeiling)
	in.Error = "read openrouter stream: context deadline exceeded"

	saved, err := store.SaveRun(in)
	if err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if saved.ID == "" {
		t.Fatal("SaveRun should assign an id")
	}
	got, ok, err := store.GetRun(saved.ID)
	if err != nil || !ok {
		t.Fatalf("GetRun: ok=%v err=%v", ok, err)
	}
	if got.AnswerMessageID != nil || got.StopReason != assistantStopTimeCeiling || got.Error != in.Error || got.Model != in.Model || got.DurationMs != 133000 || got.AfterSeq != 1 {
		t.Fatalf("run = %+v", got)
	}
	if got.Budget != in.Budget {
		t.Fatalf("budget = %+v, want %+v", got.Budget, in.Budget)
	}
	if len(got.Rounds) != 2 || len(got.Rounds[0].Calls) != 2 {
		t.Fatalf("rounds = %+v", got.Rounds)
	}
	r0 := got.Rounds[0]
	if r0.PromptTokens != 900 || r0.CachedTokens != 600 || r0.CostUSD != 0.004 || !r0.Progress || r0.LatencyMs != 4100 {
		t.Fatalf("round 0 = %+v", r0)
	}
	if c := r0.Calls[0]; c.Name != "find_places" || c.Cache != "miss" || c.ResultBytes != 812 || c.ElapsedMs != 2300 || c.Result != "" {
		t.Fatalf("call 0 = %+v", c)
	}
	if c := r0.Calls[1]; c.Error != "tide station offline" || c.Cache != "" {
		t.Fatalf("call 1 = %+v", c)
	}
	if r1 := got.Rounds[1]; !r1.ForcedFinal || r1.Error != "context deadline exceeded" || len(r1.Calls) != 0 {
		t.Fatalf("round 1 = %+v", r1)
	}
}

func TestAssistantRunStore_ListRunsGivesSummariesInTheOrderTheyStarted(t *testing.T) {
	store := newTestAssistantStore(t)
	conv, _ := store.CreateConversation("Passage")
	answer, err := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "ok"})
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	first := sampleRunTrace(conv.ID, 1, &answer.ID, assistantStopAnswered)
	second := sampleRunTrace(conv.ID, 3, nil, assistantStopCancelled)
	second.StartedAt = first.StartedAt.Add(time.Hour)
	if _, err := store.SaveRun(second); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if _, err := store.SaveRun(first); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	other, _ := store.CreateConversation("Other")
	if _, err := store.SaveRun(sampleRunTrace(other.ID, 1, nil, assistantStopError)); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	runs, err := store.ListRuns(conv.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 2 || runs[0].StopReason != assistantStopAnswered || runs[1].StopReason != assistantStopCancelled {
		t.Fatalf("runs = %+v", runs)
	}
	if runs[0].AnswerMessageID == nil || *runs[0].AnswerMessageID != answer.ID {
		t.Fatalf("answer id = %v", runs[0].AnswerMessageID)
	}
	if runs[0].RoundCount != 1 || len(runs[0].Rounds) != 0 {
		t.Fatalf("a summary should carry the round count and no rounds: %+v", runs[0])
	}
}

func TestAssistantRunStore_DeletingAConversationDeletesItsRuns(t *testing.T) {
	store := newTestAssistantStore(t)
	conv, _ := store.CreateConversation("Passage")
	saved, err := store.SaveRun(sampleRunTrace(conv.ID, 1, nil, assistantStopError))
	if err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if err := store.DeleteConversation(conv.ID); err != nil {
		t.Fatalf("DeleteConversation: %v", err)
	}
	if _, ok, _ := store.GetRun(saved.ID); ok {
		t.Fatal("the run outlived its conversation")
	}
	var rounds int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM assistant_run_rounds`).Scan(&rounds); err != nil || rounds != 0 {
		t.Fatalf("rounds left behind: %d, %v", rounds, err)
	}
}

func TestAssistantRunStore_PruneKeepsFailedRunsLongerThanUnratedAnswers(t *testing.T) {
	store := newTestAssistantStore(t)
	now := time.Date(2026, 11, 30, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	conv, _ := store.CreateConversation("Passage")
	// An answered run is as old as its answer.
	store.now = func() time.Time { return now.Add(-assistantRunRetention - time.Hour) }
	rated, _ := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "rated"})
	unrated, _ := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "unrated"})
	store.now = func() time.Time { return now.Add(-time.Hour) }
	young, _ := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "young"})
	store.now = func() time.Time { return now }
	if _, err := store.SetMessageFeedback(rated.ID, assistantFeedbackUp, ""); err != nil {
		t.Fatalf("SetMessageFeedback: %v", err)
	}

	save := func(answer *string, age time.Duration) string {
		tr := sampleRunTrace(conv.ID, 1, answer, assistantStopAnswered)
		tr.StartedAt = now.Add(-age)
		saved, err := store.SaveRun(tr)
		if err != nil {
			t.Fatalf("SaveRun: %v", err)
		}
		return saved.ID
	}
	oldUnrated := save(&unrated.ID, assistantRunRetention+time.Hour)
	oldRated := save(&rated.ID, assistantRunRetention+time.Hour)
	youngUnrated := save(&young.ID, time.Hour)
	failedYoungEnough := save(nil, assistantRunRetention+time.Hour)
	failedTooOld := save(nil, assistantFailedRunRetention+time.Hour)

	n, err := store.PruneRuns()
	if err != nil {
		t.Fatalf("PruneRuns: %v", err)
	}
	if n != 2 {
		t.Fatalf("pruned %d runs, want 2", n)
	}
	for id, want := range map[string]bool{oldUnrated: false, oldRated: true, youngUnrated: true, failedYoungEnough: true, failedTooOld: false} {
		if _, ok, _ := store.GetRun(id); ok != want {
			t.Fatalf("run %s present = %v, want %v", id, ok, want)
		}
	}
}
