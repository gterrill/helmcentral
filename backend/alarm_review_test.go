package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// Mate reviews an alarm when it fires (ADR 0237).

type alarmReviewHarness struct {
	r        *alarmReviewer
	chats    *assistantStore
	d        *alarmDispatcher
	ntfy     *recordingTransport
	webhook  *recordingTransport
	signalk  *recordingTransport
	rule     alarmRule
	now      time.Time
	stillOn  bool
	contexts int
}

func newAlarmReviewHarness(t *testing.T, mateReviews bool) *alarmReviewHarness {
	t.Helper()
	withTempAlarmRules(t)
	h := &alarmReviewHarness{
		chats:   newTestAssistantStore(t),
		ntfy:    &recordingTransport{id: transportNtfy},
		webhook: &recordingTransport{id: transportWebhook},
		signalk: &recordingTransport{id: transportSignalK},
		now:     watchTestTime(29, 2, 0),
		stillOn: true,
	}
	h.d, _ = watchDispatcher(t, h.ntfy, h.webhook, h.signalk)
	rule := validRule()
	rule.MateReviews = mateReviews
	rule.Notify = []string{transportNtfy, transportSignalK}
	created, err := createAlarmRule(rule)
	if err != nil {
		t.Fatal(err)
	}
	h.rule = created
	boat := watchBoatLocation
	watchBoatLocation = func() (*time.Location, error) { return time.UTC, nil }
	t.Cleanup(func() { watchBoatLocation = boat })
	h.r = newAlarmReviewer("")
	t.Cleanup(func() { h.r.stop(time.Second) })
	h.r.config = func() (watchConfig, error) { return reviewConfig(t), nil }
	h.r.now = func() time.Time { return h.now }
	h.r.assistant = func() *assistantStore { return h.chats }
	h.r.dispatcher = func() *alarmDispatcher { return h.d }
	h.r.linkBase = func() string { return "https://boat.example.org" }
	h.r.stillRaised = func(string, time.Time) bool { return h.stillOn }
	h.r.contextFor = func(_ context.Context, _ assistantToolDeps, ruleID string) (string, error) {
		h.contexts++
		return `{"alarm":{"title":"House bank low","active":true}}`, nil
	}
	return h
}

func (h *alarmReviewHarness) raise() alarmEvent {
	return alarmEvent{
		Kind: alarmEventRaised, Rule: h.rule,
		Status: alarmStatus{RuleID: h.rule.ID, Label: h.rule.Label, State: alarmStateAlarm, RaisedAt: h.now, Message: "House bank low"},
	}
}

func submitAlarmReview(text string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"review": text})
	return raw
}

func withAlarmReviewScript(t *testing.T, text string) *fakeReviewRunner {
	return withReviewRunner(t, func(ctx context.Context, tools assistantToolExecutor) (assistantReply, error) {
		if _, err := tools.execute(ctx, alarmReviewToolName, submitAlarmReview(text)); err != nil {
			return assistantReply{}, err
		}
		assistantCostMeterFrom(ctx).add(0.012)
		return assistantReply{Content: "done", Model: "test/model"}, nil
	})
}

func (h *alarmReviewHarness) latest(t *testing.T) alarmReview {
	t.Helper()
	got, ok, err := h.r.latestFor(h.rule.ID)
	if err != nil || !ok {
		t.Fatalf("latestFor: %v ok %v", err, ok)
	}
	return got
}

func TestAlarmRule_MateReviewsSwitchPersistsAndIsOffByDefault(t *testing.T) {
	withTempAlarmRules(t)
	off, err := createAlarmRule(validRule())
	if err != nil || off.MateReviews {
		t.Fatalf("a new rule is not reviewed by default: %+v %v", off, err)
	}
	on := validRule()
	on.MateReviews = true
	created, err := createAlarmRule(on)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := newDocumentStore(globalDocumentStore.path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var got alarmRule
	if err := reopened.Read(func(q sqlQueryer) error {
		var err error
		got, err = alarmRuleByIDQ(q, created.ID)
		return err
	}); err != nil || !got.MateReviews {
		t.Fatalf("the switch must survive a reopen: %+v %v", got, err)
	}
	created.MateReviews = false
	updated, err := updateAlarmRule(created.ID, created)
	if err != nil || updated.MateReviews {
		t.Fatalf("update turns it off: %+v %v", updated, err)
	}
	if again, _ := getAlarmRule(created.ID); again.MateReviews {
		t.Fatal("the running copy must follow the update")
	}
}

func TestAlarmRule_MateProposesTheSwitchAsAField(t *testing.T) {
	r, err := applyAlarmFields(validRule(), fieldSet{"mate_reviews": true})
	if err != nil || !r.MateReviews {
		t.Fatalf("a proposal sets the switch: %+v %v", r, err)
	}
	r, err = applyAlarmFields(r, fieldSet{"mate_reviews": false})
	if err != nil || r.MateReviews {
		t.Fatalf("and clears it: %+v %v", r, err)
	}
	snap := alarmRuleSnapshot(r)
	if v, ok := snap.Fields["mate_reviews"]; !ok || v != false {
		t.Fatalf("the record shows the switch: %v", snap.Fields)
	}
}

func TestAlarmReview_ARaiseStartsOneReviewAndARepeatWithinAnHourDoesNot(t *testing.T) {
	h := newAlarmReviewHarness(t, true)
	runs := 0
	withReviewRunner(t, func(ctx context.Context, tools assistantToolExecutor) (assistantReply, error) {
		runs++
		if _, err := tools.execute(ctx, alarmReviewToolName, submitAlarmReview("The house bank is at 11.6 V and still falling. It needs you now.")); err != nil {
			return assistantReply{}, err
		}
		return assistantReply{Content: "done", Model: "test/model"}, nil
	})
	h.r.onRaise(h.raise(), h.now)
	h.r.wait()
	if runs != 1 {
		t.Fatalf("a raise starts one review, got %d", runs)
	}
	h.now = h.now.Add(59 * time.Minute)
	h.r.onRaise(h.raise(), h.now)
	h.r.wait()
	if runs != 1 {
		t.Fatalf("a repeat within the hour is not reviewed again, got %d", runs)
	}
	h.now = h.now.Add(2 * time.Minute)
	h.r.onRaise(h.raise(), h.now)
	h.r.wait()
	if runs != 2 {
		t.Fatalf("a raise an hour after the last review is reviewed, got %d", runs)
	}
}

func TestAlarmReview_OnlyARuleWithTheSwitchOnIsReviewed(t *testing.T) {
	h := newAlarmReviewHarness(t, false)
	runs := 0
	withReviewRunner(t, func(context.Context, assistantToolExecutor) (assistantReply, error) {
		runs++
		return assistantReply{}, nil
	})
	h.r.onRaise(h.raise(), h.now)
	bus := h.raise()
	bus.Source = alarmSourceSignalK
	bus.Rule.MateReviews = true
	h.r.onRaise(bus, h.now)
	cleared := h.raise()
	cleared.Kind = alarmEventCleared
	h.rule.MateReviews = true
	cleared.Rule = h.rule
	h.r.onRaise(cleared, h.now)
	h.r.wait()
	if runs != 0 {
		t.Fatalf("switch off, a network alarm and a clear start nothing, got %d", runs)
	}
	if _, ok, _ := h.r.latestFor(h.rule.ID); ok {
		t.Fatal("nothing is recorded for them")
	}
}

func TestAlarmReview_BecomesAConversationTitledAfterTheAlarmAndShowsOnTheCard(t *testing.T) {
	h := newAlarmReviewHarness(t, true)
	withAlarmReviewScript(t, "The house bank is at 11.6 V and still falling. Check what is drawing power. A bigger alternator would not help.")
	h.r.onRaise(h.raise(), h.now)
	h.r.wait()

	rev := h.latest(t)
	if rev.State != alarmReviewDone || rev.ConversationID == "" || rev.Problem != "" {
		t.Fatalf("review %+v", rev)
	}
	conv, ok, err := h.chats.GetConversation(rev.ConversationID)
	if err != nil || !ok || conv.Title != "House bank low" {
		t.Fatalf("conversation %+v ok %v err %v", conv, ok, err)
	}
	msgs, _ := h.chats.ListMessages(rev.ConversationID)
	if len(msgs) != 2 || msgs[0].Role != "user" || msgs[1].Role != "assistant" ||
		!strings.Contains(msgs[1].Content, "11.6 V and still falling") || msgs[1].CostUSD != 0.012 {
		t.Fatalf("messages %+v", msgs)
	}
	if rev.Summary != "The house bank is at 11.6 V and still falling." || rev.CostUSD != 0.012 || !rev.CostKnown {
		t.Fatalf("summary %q cost %v/%v", rev.Summary, rev.CostUSD, rev.CostKnown)
	}

	// The card carries the link and the first sentence.
	statuses := h.r.attach([]alarmStatus{{RuleID: h.rule.ID, RaisedAt: h.now}, {RuleID: "other", RaisedAt: h.now}})
	card := statuses[0].MateReview
	if card == nil || card.ConversationID != rev.ConversationID || card.Summary != rev.Summary || card.State != alarmReviewDone {
		t.Fatalf("card %+v", card)
	}
	if statuses[1].MateReview != nil {
		t.Fatal("another alarm gets no card")
	}
	raw, _ := json.Marshal(statuses[0])
	if !strings.Contains(string(raw), `"mate_review":{`) || !strings.Contains(string(raw), `"conversation_id":"`+rev.ConversationID+`"`) {
		t.Fatalf("wire form %s", raw)
	}
	// A later raise of the same rule is a different alarm: it does not wear the old review.
	later := h.r.attach([]alarmStatus{{RuleID: h.rule.ID, RaisedAt: h.now.Add(3 * time.Hour)}})
	if later[0].MateReview != nil {
		t.Fatal("the old review belongs to the old raise")
	}
}

func TestAlarmReview_TheTurnIsGivenTheAlarmContextAndOnlyReadTools(t *testing.T) {
	h := newAlarmReviewHarness(t, true)
	var offered []string
	fake := withReviewRunner(t, func(ctx context.Context, tools assistantToolExecutor) (assistantReply, error) {
		for _, def := range tools.definitions() {
			offered = append(offered, def.Function.Name)
		}
		for _, name := range []string{"propose_changes", "start_watch", "fill_form", "inspect_form", "run_watch_now", "show_telemetry_chart", "search_web", watchReviewToolName} {
			if _, err := tools.execute(ctx, name, json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "not available in an alarm review") {
				t.Errorf("%s must be refused in an alarm review, got %v", name, err)
			}
		}
		_, err := tools.execute(ctx, alarmReviewToolName, submitAlarmReview("Fine."))
		return assistantReply{}, err
	})
	h.r.onRaise(h.raise(), h.now)
	h.r.wait()

	for _, name := range offered {
		if !alarmReviewReadTools[name] && name != alarmReviewToolName {
			t.Errorf("%s must not be offered", name)
		}
	}
	has := map[string]bool{}
	for _, n := range offered {
		has[n] = true
	}
	for _, want := range []string{"get_alarm_context", "get_path_history", alarmReviewToolName} {
		if !has[want] {
			t.Errorf("%s should be offered, got %v", want, offered)
		}
	}
	if h.contexts != 1 || len(fake.history) != 1 || !strings.Contains(string(fake.history[0].Content), `"title":"House bank low"`) {
		t.Fatalf("the alarm context is the message: contexts %d history %+v", h.contexts, fake.history)
	}
}

func TestAlarmReview_PromptHoldsTheReviewRules(t *testing.T) {
	p := buildAlarmReviewPrompt(assistantPromptContext{VesselName: "Pikorua"}, "metric", watchTestTime(29, 2, 0), time.UTC, true)
	for _, want := range []string{
		"unattended", "what the reading is doing", "needs the watchkeeper now", "named source", "never guess a cause",
		"no paths, rule ids or device keys", "at most one", "in words only", "cannot propose", alarmReviewToolName,
	} {
		if !strings.Contains(strings.ToLower(p), strings.ToLower(want)) {
			t.Errorf("the prompt lacks %q:\n%s", want, p)
		}
	}
}

func TestAlarmReview_SubmissionIsHeldToTheAlarmWording(t *testing.T) {
	tools := &alarmReviewTools{rule: validRule()}
	tools.rule.ID = "rule-9"
	for name, text := range map[string]string{
		"empty":    "  ",
		"bang":     "The bank is low!",
		"question": "Is the bank low?",
		"path":     "electrical.batteries.house.voltage is low.",
		"rule id":  "Rule rule-9 fired.",
		"long":     strings.Repeat("a", maxAlarmReviewChars+1),
	} {
		if _, err := tools.execute(context.Background(), alarmReviewToolName, submitAlarmReview(text)); err == nil {
			t.Errorf("%s should be refused", name)
		}
	}
	raw, _ := json.Marshal(map[string]any{"review": "The bank is low.", "suggested_rule_change": "Raise the clear point to 12.4 V."})
	if _, err := tools.execute(context.Background(), alarmReviewToolName, raw); err != nil {
		t.Fatal(err)
	}
	if s := tools.submission(); s == nil || s.RuleChange != "Raise the clear point to 12.4 V." {
		t.Fatalf("submission %+v", s)
	}
}

func TestAlarmReview_AFailureIsRecordedAndShownNotRetriedOrReplaced(t *testing.T) {
	h := newAlarmReviewHarness(t, true)
	calls := 0
	withReviewRunner(t, func(context.Context, assistantToolExecutor) (assistantReply, error) {
		calls++
		return assistantReply{}, errors.New("openrouter: 502 bad gateway")
	})
	h.r.onRaise(h.raise(), h.now)
	h.r.wait()
	rev := h.latest(t)
	if rev.State != alarmReviewFailed || !strings.Contains(rev.Problem, "502 bad gateway") || rev.ConversationID != "" || rev.Summary != "" {
		t.Fatalf("review %+v", rev)
	}
	if calls != 1 {
		t.Fatalf("never retried silently, got %d calls", calls)
	}
	if h.ntfy.count() != 0 || h.d.queueDepth() != 0 {
		t.Fatal("a failed review sends no follow-up")
	}
	card := h.r.attach([]alarmStatus{{RuleID: h.rule.ID, RaisedAt: h.now}})[0].MateReview
	if card == nil || card.State != alarmReviewFailed || !strings.Contains(card.Problem, "502") || card.ConversationID != "" {
		t.Fatalf("the card says it failed: %+v", card)
	}
}

func TestAlarmReview_FinishingWithoutSubmittingIsAFailure(t *testing.T) {
	h := newAlarmReviewHarness(t, true)
	withReviewRunner(t, func(context.Context, assistantToolExecutor) (assistantReply, error) {
		return assistantReply{Content: "the bank is low"}, nil
	})
	h.r.onRaise(h.raise(), h.now)
	h.r.wait()
	if rev := h.latest(t); rev.State != alarmReviewFailed || rev.Problem != "Mate finished without submitting a review" {
		t.Fatalf("review %+v", rev)
	}
}

func TestAlarmReview_MateNotReadyIsRecordedAsAFailure(t *testing.T) {
	h := newAlarmReviewHarness(t, true)
	h.r.config = func() (watchConfig, error) {
		return watchConfig{NotReady: "No OpenRouter API key is configured.", Readiness: assistantReadiness{Enabled: true}}, nil
	}
	built := false
	withReviewRunner(t, func(context.Context, assistantToolExecutor) (assistantReply, error) {
		built = true
		return assistantReply{}, nil
	})
	h.r.onRaise(h.raise(), h.now)
	h.r.wait()
	if rev := h.latest(t); built || rev.State != alarmReviewFailed || !strings.Contains(rev.Problem, "Mate is not ready") {
		t.Fatalf("built %v review %+v", built, rev)
	}
}

func TestAlarmReview_TheDeadlineEndsTheTurnAndIsRecorded(t *testing.T) {
	h := newAlarmReviewHarness(t, true)
	h.r.deadline = 20 * time.Millisecond
	withReviewRunner(t, func(ctx context.Context, _ assistantToolExecutor) (assistantReply, error) {
		<-ctx.Done()
		return assistantReply{}, ctx.Err()
	})
	h.r.onRaise(h.raise(), h.now)
	h.r.wait()
	if rev := h.latest(t); rev.State != alarmReviewFailed || !strings.Contains(rev.Problem, "deadline") {
		t.Fatalf("review %+v", rev)
	}
}

func TestAlarmReview_TheFollowUpGoesAfterTheAlarmOnTheRulesOwnChannels(t *testing.T) {
	h := newAlarmReviewHarness(t, true)
	withAlarmReviewScript(t, "The house bank is at 11.6 V and still falling. More follows.")
	// The alarm's own notification is already queued when the review starts.
	h.d.enqueue(h.raise(), "Pikorua")
	h.r.onRaise(h.raise(), h.now)
	h.r.wait()

	rev := h.latest(t)
	if rev.Followup != alarmFollowupQueued {
		t.Fatalf("followup %q", rev.Followup)
	}
	first, _ := h.d.popQueued()
	second, ok := h.d.popQueued()
	if first.message != nil || first.event.Kind != alarmEventRaised || !ok || second.message == nil {
		t.Fatalf("the alarm goes first, then the follow-up: %+v %+v", first, second)
	}
	h.d.dispatchAlarmReview(*second.message, second.channels)

	if h.ntfy.count() != 1 || h.webhook.count() != 0 || h.signalk.count() != 0 {
		t.Fatalf("only the rule's own channels, never the bus: ntfy %d webhook %d signalk %d", h.ntfy.count(), h.webhook.count(), h.signalk.count())
	}
	got := h.ntfy.got[0]
	wantLink := "https://boat.example.org/mate/" + rev.ConversationID
	if got.Kind != notificationKindAlarmReview || got.title() != "Mate on House bank low" || got.Link != wantLink ||
		!strings.HasPrefix(got.Message, "The house bank is at 11.6 V and still falling.") || !strings.Contains(got.Message, wantLink) {
		t.Fatalf("message %+v", got)
	}
	if strings.Contains(got.Message, "More follows") {
		t.Fatalf("only the first sentence goes out: %q", got.Message)
	}
}

func TestAlarmReview_NoLinkAddressMeansTheFollowUpHasNoLink(t *testing.T) {
	h := newAlarmReviewHarness(t, true)
	h.r.linkBase = func() string { return "" }
	withAlarmReviewScript(t, "The house bank is low.")
	h.r.onRaise(h.raise(), h.now)
	h.r.wait()
	item, ok := h.d.popQueued()
	if !ok || item.message == nil || item.message.Link != "" || strings.Contains(item.message.Message, "http") {
		t.Fatalf("item %+v", item)
	}
}

func TestAlarmReview_NothingIsSentIfTheAlarmClearedFirst(t *testing.T) {
	h := newAlarmReviewHarness(t, true)
	h.stillOn = false
	withAlarmReviewScript(t, "The house bank is low.")
	h.r.onRaise(h.raise(), h.now)
	h.r.wait()
	rev := h.latest(t)
	if rev.State != alarmReviewDone || rev.ConversationID == "" || rev.Followup != alarmFollowupSkippedCleared {
		t.Fatalf("the review stands and says why nothing went out: %+v", rev)
	}
	if h.d.queueDepth() != 0 {
		t.Fatal("no follow-up for a cleared alarm")
	}
}

func TestAlarmReview_ARestartKeepsTheReviewOnTheCard(t *testing.T) {
	h := newAlarmReviewHarness(t, true)
	withAlarmReviewScript(t, "The house bank is low.")
	h.r.onRaise(h.raise(), h.now)
	h.r.wait()
	// A reviewer started after a restart sees the stored review.
	again := newAlarmReviewer("")
	if err := again.start(); err != nil {
		t.Fatal(err)
	}
	cards := again.attach([]alarmStatus{{RuleID: h.rule.ID, RaisedAt: h.now}})
	if cards[0].MateReview == nil || cards[0].MateReview.State != alarmReviewDone {
		t.Fatalf("after a restart %+v", cards[0].MateReview)
	}
}

func TestAlarmReview_RecordingARaiseStartsTheReviewAfterQueueingTheAlarm(t *testing.T) {
	h := newAlarmReviewHarness(t, true)
	withAlarmReviewScript(t, "The house bank is low.")
	prevLog, prevD, prevR := globalAlarmLogStore, globalAlarmDispatcher, globalAlarmReviewer
	globalAlarmLogStore, globalAlarmDispatcher, globalAlarmReviewer = newTestAlarmLog(t), h.d, h.r
	t.Cleanup(func() { globalAlarmLogStore, globalAlarmDispatcher, globalAlarmReviewer = prevLog, prevD, prevR })

	recordAlarmEvent(h.raise(), h.now)
	h.r.wait()

	first, _ := h.d.popQueued()
	second, ok := h.d.popQueued()
	if first.message != nil || first.event.Kind != alarmEventRaised || !ok || second.message == nil {
		t.Fatalf("the alarm is queued before its review: %+v %+v", first, second)
	}
}

// A follow-up goes where its alarm went, bar the bus: a rule that names no
// channel sends its alarm on every switched-on transport, so the follow-up does
// too, and a rule that names some sends the follow-up to those.
func TestAlarmReview_TheFollowUpUsesTheChannelsItsAlarmUsed(t *testing.T) {
	for name, notify := range map[string][]string{
		"no channels named": nil,
		"some named":        {transportNtfy, transportSignalK},
	} {
		t.Run(name, func(t *testing.T) {
			h := newAlarmReviewHarness(t, true)
			h.rule.Notify = notify
			event := h.raise()
			h.d.dispatch(event, "Pikorua", time.Now().Add(time.Second))
			alarmOn := map[string]bool{transportNtfy: h.ntfy.count() == 1, transportWebhook: h.webhook.count() == 1, transportSignalK: h.signalk.count() == 1}

			ntfy, webhook, signalk := h.ntfy.count(), h.webhook.count(), h.signalk.count()
			h.d.dispatchAlarmReview(notificationMessage{Kind: notificationKindAlarmReview, Label: "Mate on House bank low", Message: "x"}, notify)
			reviewOn := map[string]bool{transportNtfy: h.ntfy.count() == ntfy+1, transportWebhook: h.webhook.count() == webhook+1, transportSignalK: h.signalk.count() == signalk+1}

			if reviewOn[transportSignalK] {
				t.Fatal("never the bus")
			}
			for _, id := range []string{transportNtfy, transportWebhook} {
				if alarmOn[id] != reviewOn[id] {
					t.Errorf("%s: alarm %v review %v", id, alarmOn[id], reviewOn[id])
				}
			}
			if notify == nil && !(alarmOn[transportNtfy] && alarmOn[transportWebhook] && alarmOn[transportSignalK]) {
				t.Fatalf("an empty list sends the alarm everywhere today: %v", alarmOn)
			}
		})
	}
}

func TestAlarmReview_OnRaiseDoesNotWaitForTheDatabase(t *testing.T) {
	h := newAlarmReviewHarness(t, true)
	withAlarmReviewScript(t, "The house bank is low.")
	// The record store is busy: anything on the caller's path would wait here.
	globalDocumentStore.mu.Lock()
	returned := make(chan struct{})
	go func() { h.r.onRaise(h.raise(), h.now); close(returned) }()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		globalDocumentStore.mu.Unlock()
		t.Fatal("onRaise waited for the store, which would hold up the alarm evaluator")
	}
	globalDocumentStore.mu.Unlock()
	h.r.wait()
	if rev := h.latest(t); rev.State != alarmReviewDone {
		t.Fatalf("review %+v", rev)
	}
}

func TestAlarmReview_TwoRaisesTogetherAreReviewedOnce(t *testing.T) {
	h := newAlarmReviewHarness(t, true)
	runs := 0
	var mu sync.Mutex
	withReviewRunner(t, func(ctx context.Context, tools assistantToolExecutor) (assistantReply, error) {
		mu.Lock()
		runs++
		mu.Unlock()
		_, err := tools.execute(ctx, alarmReviewToolName, submitAlarmReview("The house bank is low."))
		return assistantReply{}, err
	})
	for i := 0; i < 5; i++ {
		h.r.onRaise(h.raise(), h.now)
	}
	h.r.wait()
	if runs != 1 {
		t.Fatalf("one review for raises that arrive together, got %d", runs)
	}
}

func TestAlarmReview_TodayIsTheBoatsDate(t *testing.T) {
	h := newAlarmReviewHarness(t, true)
	watchBoatLocation = func() (*time.Location, error) { return time.FixedZone("NZDT", 13*3600), nil }
	h.now = time.Date(2026, 10, 29, 20, 0, 0, 0, time.UTC) // 09:00 on the 30th at UTC+13
	h.r.now = func() time.Time { return h.now }
	fake := withAlarmReviewScript(t, "The house bank is low.")
	h.r.onRaise(h.raise(), h.now)
	h.r.wait()
	tools, ok := fake.tools.(*alarmReviewTools)
	if !ok || tools.today.Format("2006-01-02") != "2026-10-30" {
		t.Fatalf("today %v", tools.today)
	}
	if !strings.Contains(fake.system, "Boat time: Fri 30 Oct 2026 09:00 NZDT") {
		t.Fatalf("the prompt gives the boat's time:\n%s", fake.system)
	}
}

func TestAlarmReview_WithNoFixThePromptSaysTimesAreUTC(t *testing.T) {
	h := newAlarmReviewHarness(t, true)
	watchBoatLocation = func() (*time.Location, error) { return nil, errors.New("no position fix has ever been seen") }
	fake := withAlarmReviewScript(t, "The house bank is low.")
	h.r.onRaise(h.raise(), h.now)
	h.r.wait()
	if !strings.Contains(fake.system, "UTC") || strings.Contains(fake.system, "Boat time:") {
		t.Fatalf("with no fix the times are UTC and are not called boat time:\n%s", fake.system)
	}
	with := buildAlarmReviewPrompt(assistantPromptContext{}, "metric", h.now, time.UTC, true)
	if !strings.Contains(with, "Boat time:") {
		t.Fatalf("with a fix the prompt gives boat time:\n%s", with)
	}
}

func TestAlarmReview_ARaiseWithNoTimeFailsTheReview(t *testing.T) {
	h := newAlarmReviewHarness(t, true)
	built := false
	withReviewRunner(t, func(context.Context, assistantToolExecutor) (assistantReply, error) {
		built = true
		return assistantReply{}, nil
	})
	event := h.raise()
	event.Status.RaisedAt = time.Time{}
	h.r.onRaise(event, h.now)
	h.r.wait()
	if rev := h.latest(t); built || rev.State != alarmReviewFailed || !strings.Contains(rev.Problem, "raise time") {
		t.Fatalf("built %v review %+v", built, rev)
	}
}

func TestAlarmReview_StartupFailsReviewsLeftRunningAndTheyDoNotHoldBackTheNext(t *testing.T) {
	h := newAlarmReviewHarness(t, true)
	orphan := alarmReview{ID: "orphan", RuleID: h.rule.ID, Title: h.rule.Label, RaisedAt: h.now.Add(-5 * time.Minute), StartedAt: h.now.Add(-5 * time.Minute), State: alarmReviewActive}
	if err := h.r.insertReview(orphan); err != nil {
		t.Fatal(err)
	}
	again := newAlarmReviewer("")
	again.config, again.now, again.assistant, again.dispatcher = h.r.config, h.r.now, h.r.assistant, h.r.dispatcher
	again.linkBase, again.stillRaised, again.contextFor = h.r.linkBase, h.r.stillRaised, h.r.contextFor
	t.Cleanup(func() { again.stop(time.Second) })
	if err := again.start(); err != nil {
		t.Fatal(err)
	}
	got, ok, err := again.latestFor(h.rule.ID)
	if err != nil || !ok || got.State != alarmReviewFailed || got.Problem != "Helmcentral restarted during the review" {
		t.Fatalf("orphan %+v ok %v err %v", got, ok, err)
	}
	runs := 0
	withReviewRunner(t, func(ctx context.Context, tools assistantToolExecutor) (assistantReply, error) {
		runs++
		_, err := tools.execute(ctx, alarmReviewToolName, submitAlarmReview("The house bank is low."))
		return assistantReply{}, err
	})
	again.onRaise(h.raise(), h.now)
	again.wait()
	if runs != 1 {
		t.Fatalf("a review that died with the restart must not count toward the hour, got %d runs", runs)
	}
}

func TestAlarmReview_ShuttingDownRecordsTheReviewAsStopped(t *testing.T) {
	h := newAlarmReviewHarness(t, true)
	started := make(chan struct{})
	withReviewRunner(t, func(ctx context.Context, _ assistantToolExecutor) (assistantReply, error) {
		close(started)
		<-ctx.Done()
		return assistantReply{}, ctx.Err()
	})
	h.r.onRaise(h.raise(), h.now)
	<-started
	h.r.stop(5 * time.Second)
	if rev := h.latest(t); rev.State != alarmReviewFailed || rev.Problem != "Helmcentral stopped during the review" {
		t.Fatalf("review %+v", rev)
	}
	// A stopped review does not hold back the next one either.
	if !rev0(h).interrupted() {
		t.Fatal("a stopped review is interrupted, not a finished one")
	}
}

func rev0(h *alarmReviewHarness) alarmReview {
	got, _, _ := h.r.latestFor(h.rule.ID)
	return got
}

func TestAlarmReview_WithMateOffNothingStartsAndNothingIsWritten(t *testing.T) {
	h := newAlarmReviewHarness(t, true)
	h.r.config = func() (watchConfig, error) {
		return watchConfig{NotReady: "The assistant is switched off.", Readiness: assistantReadiness{Enabled: false}}, nil
	}
	built := false
	withReviewRunner(t, func(context.Context, assistantToolExecutor) (assistantReply, error) {
		built = true
		return assistantReply{}, nil
	})
	h.r.onRaise(h.raise(), h.now)
	h.r.wait()
	if _, ok, _ := h.r.latestFor(h.rule.ID); ok || built {
		t.Fatalf("Mate off: row %v built %v", ok, built)
	}
	var n int
	if err := globalDocumentStore.Read(func(q sqlQueryer) error {
		return q.QueryRow(`SELECT COUNT(*) FROM alarm_reviews`).Scan(&n)
	}); err != nil || n != 0 {
		t.Fatalf("rows %d err %v", n, err)
	}
}
