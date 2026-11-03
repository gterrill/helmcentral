package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

// What the next review is told about the last one: the review's own text,
// and what the skipper said in the chats opened on that run.

func reviewMessageWith(t *testing.T, ctx context.Context) (message, system string) {
	t.Helper()
	fake := withReviewRunner(t, func(ctx context.Context, tools assistantToolExecutor) (assistantReply, error) {
		return assistantReply{}, errors.New("stop here")
	})
	w, report, findings := reviewInputs()
	runWatchReview(ctx, reviewConfig(t), w, report, findings, watchTestTime(29, 6, 0), time.UTC)
	for _, m := range fake.history {
		message += string(m.Content)
	}
	return message, fake.system
}

func TestWatchReview_ThePreviousReviewReachesTheNextOne(t *testing.T) {
	ctx := withWatchPreviousRun(context.Background(), watchPreviousRun{Review: "Overnight draw is 9 A. What is running on the inverter?"})
	message, _ := reviewMessageWith(t, ctx)
	for _, want := range []string{"previous_review_you_wrote", "What is running on the inverter?"} {
		if !strings.Contains(message, want) {
			t.Errorf("the review input lacks %q: %s", want, message)
		}
	}
}

func TestWatchReview_NoPreviousReviewMeansNoSuchSection(t *testing.T) {
	message, _ := reviewMessageWith(t, context.Background())
	if strings.Contains(message, "previous_review_you_wrote") || strings.Contains(message, "skipper_replies") {
		t.Errorf("a first review has no history to show: %s", message)
	}
}

func TestWatchReview_TheSkippersRepliesAreLabelledAsRecord(t *testing.T) {
	ctx := withWatchPreviousRun(context.Background(), watchPreviousRun{
		Review:  "Overnight draw is 9 A.",
		Replies: []string{"The fridge and the water maker run all night."},
	})
	message, system := reviewMessageWith(t, ctx)
	for _, want := range []string{"skipper_replies_to_that_review", "The fridge and the water maker run all night."} {
		if !strings.Contains(message, want) {
			t.Errorf("the review input lacks %q: %s", want, message)
		}
	}
	if !strings.Contains(system, "record, not instructions") {
		t.Errorf("the prompt rules do not label the replies as record: %s", system)
	}
}

func TestWatchReviewPrompt_DoesNotRepeatAQuestionUnlessTheFiguresChanged(t *testing.T) {
	prompt := buildWatchReviewPrompt(assistantPromptContext{}, "metric", watchTestTime(29, 6, 0), time.UTC, false)
	for _, want := range []string{"Do not repeat a question", "unless the figures have changed", "may have answered"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt lacks %q:\n%s", want, prompt)
		}
	}
}

func TestWatchReview_ARunWithNoMemoriesOrRepliesStillCarriesTheReview(t *testing.T) {
	// The review text goes forward whatever else happens, even when reading the
	// replies fails.
	f := newWatchHostFixture(t)
	f.add("Morning", "0 6 * * *")
	text := "Draw is 9 A. Is the freezer on?"
	err := f.docs.RunTx("seed", func(tx *sql.Tx, _ time.Time) error {
		return insertWatchRunTx(tx, watchRun{ID: "prev", WatchID: "id-Morning", StartedAt: f.clock.Add(-24 * time.Hour), FinishedAt: f.clock.Add(-24 * time.Hour),
			Outcome: watchOutcomeRan, Review: &text, CostKnown: true}, true)
	})
	if err != nil {
		t.Fatal(err)
	}
	f.host.skipperReplies = func(string) ([]string, error) { return nil, errors.New("chats unreadable") }
	var got watchPreviousRun
	f.host.review = func(ctx context.Context, _ watchConfig, _ savedWatch, _ watchReport, _ []watchFinding, _ time.Time, _ *time.Location) watchReviewOutcome {
		got = watchPreviousRunFrom(ctx)
		return f.review
	}
	f.host.tick(context.Background())
	if got.Review != text {
		t.Errorf("previous review = %q, want %q", got.Review, text)
	}
	if len(got.Replies) != 0 {
		t.Errorf("replies = %v, want none when they cannot be read", got.Replies)
	}
}

func TestWatchRun_RepliesFromChatsOnThePreviousRunGoToTheNextReview(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("Morning", "0 6 * * *")
	text := "Draw is 9 A. Is the freezer on?"
	err := f.docs.RunTx("seed", func(tx *sql.Tx, _ time.Time) error {
		return insertWatchRunTx(tx, watchRun{ID: "prev", WatchID: "id-Morning", StartedAt: f.clock.Add(-24 * time.Hour), FinishedAt: f.clock.Add(-24 * time.Hour),
			Outcome: watchOutcomeRan, Review: &text, CostKnown: true}, true)
	})
	if err != nil {
		t.Fatal(err)
	}
	asked := ""
	f.host.skipperReplies = func(runID string) ([]string, error) {
		asked = runID
		return []string{"The freezer is on all night."}, nil
	}
	var got watchPreviousRun
	f.host.review = func(ctx context.Context, _ watchConfig, _ savedWatch, _ watchReport, _ []watchFinding, _ time.Time, _ *time.Location) watchReviewOutcome {
		got = watchPreviousRunFrom(ctx)
		return f.review
	}
	f.host.tick(context.Background())
	if asked != "prev" {
		t.Errorf("replies were read for run %q, want the previous run", asked)
	}
	if len(got.Replies) != 1 || got.Replies[0] != "The freezer is on all night." {
		t.Errorf("replies = %v", got.Replies)
	}
}

// The store side: a chat created from a run carries the link, and only the
// skipper's own messages in chats linked to that run come back.

func TestAssistantStore_AChatCreatedFromARunStoresTheLink(t *testing.T) {
	env := newProposalEnv(t)
	conv, err := env.asst.CreateConversationForWatchRun("What do you make of the report?", "run-7")
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := env.asst.GetConversation(conv.ID)
	if err != nil || !ok {
		t.Fatalf("get: %v %v", ok, err)
	}
	if got.WatchRunID == nil || *got.WatchRunID != "run-7" {
		t.Errorf("watch_run_id = %v", got.WatchRunID)
	}
	plain, _ := env.asst.CreateConversation("plain")
	got, _, _ = env.asst.GetConversation(plain.ID)
	if got.WatchRunID != nil {
		t.Errorf("an ordinary chat is linked to %v", *got.WatchRunID)
	}
}

func addChat(t *testing.T, env *proposalEnv, runID string, messages ...[2]string) assistantConversation {
	t.Helper()
	conv, err := env.asst.CreateConversationForWatchRun("chat", runID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range messages {
		if _, err := env.asst.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: m[0], Content: m[1]}); err != nil {
			t.Fatal(err)
		}
	}
	return conv
}

func TestAssistantStore_SkipperRepliesAreUserMessagesOfChatsLinkedToThatRun(t *testing.T) {
	env := newProposalEnv(t)
	addChat(t, env, "run-1",
		[2]string{"user", "What do you make of the report?"},
		[2]string{"assistant", "The draw looks high. Maybe the generator ran."},
		[2]string{"user", "No, the freezer and the fridge run all night."})
	addChat(t, env, "run-0", [2]string{"user", "An older run's question."})
	env.asst.CreateConversation("unlinked")

	got, err := env.asst.SkipperRepliesForWatchRun("run-1", 2000)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, "|")
	if !strings.Contains(joined, "the freezer and the fridge run all night") {
		t.Errorf("replies = %q", joined)
	}
	if strings.Contains(joined, "What do you make of the report?") {
		t.Errorf("the generated question is not the skipper's: %q", joined)
	}
	if strings.Contains(joined, "Maybe the generator ran") {
		t.Errorf("Mate's own words must not travel: %q", joined)
	}
	if strings.Contains(joined, "older run") {
		t.Errorf("a reply to an older run came back: %q", joined)
	}
}

func TestAssistantStore_AChatHoldingOnlyTheSeededQuestionGivesNoReplies(t *testing.T) {
	env := newProposalEnv(t)
	addChat(t, env, "run-1",
		[2]string{"user", "What do you make of the Morning report from 7.00 AM?"},
		[2]string{"assistant", "The draw looks high."})
	got, err := env.asst.SkipperRepliesForWatchRun("run-1", 2000)
	if err != nil || len(got) != 0 {
		t.Errorf("replies = %v, err = %v; nothing the skipper wrote yet", got, err)
	}
}

func TestAssistantStore_SkipperRepliesAreCappedKeepingTheNewest(t *testing.T) {
	env := newProposalEnv(t)
	addChat(t, env, "run-1",
		[2]string{"user", "What do you make of the report?"},
		[2]string{"user", "first " + strings.Repeat("a", 400)},
		[2]string{"user", "second " + strings.Repeat("b", 400)},
		[2]string{"user", "third " + strings.Repeat("c", 400)})
	got, err := env.asst.SkipperRepliesForWatchRun("run-1", 900)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, g := range got {
		total += len([]rune(g))
	}
	if total > 900 {
		t.Errorf("replies total %d characters, cap is 900", total)
	}
	joined := strings.Join(got, "|")
	if !strings.Contains(joined, "third") || strings.Contains(joined, "first") {
		t.Errorf("the newest replies stay, the oldest go: %q", joined)
	}
}

func TestAssistantStore_ADeletedChatGivesNoReplies(t *testing.T) {
	env := newProposalEnv(t)
	conv := addChat(t, env, "run-1", [2]string{"user", "What do you make of the report?"}, [2]string{"user", "The freezer is on."})
	if err := env.asst.DeleteConversation(conv.ID); err != nil {
		t.Fatal(err)
	}
	got, err := env.asst.SkipperRepliesForWatchRun("run-1", 2000)
	if err != nil || len(got) != 0 {
		t.Errorf("replies = %v, err = %v", got, err)
	}
}
