package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// Final review fixes for Mate on the Inbox (ADR 0283).

// withSwitchableMate wires a Mate whose settings the test can change, the way a
// settings save does: change the value, then drop the remembered reading.
func withSwitchableMate(t *testing.T, doer openRouterDoer) (on *atomic.Bool, model *atomic.Value) {
	t.Helper()
	on, model = new(atomic.Bool), new(atomic.Value)
	on.Store(true)
	model.Store(testDocumentModel)
	prev := globalInboxMate
	globalInboxMate = &inboxMate{
		readiness: func() (assistantReadiness, string, error) {
			if !on.Load() {
				return assistantReadiness{Problem: "The assistant is switched off."}, "", nil
			}
			return assistantReadiness{Enabled: true, Configured: true, Model: "chat/model", DocumentModel: model.Load().(string)}, "test-key", nil
		},
		doer: doer,
	}
	t.Cleanup(func() { globalInboxMate = prev })
	return on, model
}

// ── 1: turning Mate off or changing it stops a pass in progress ──────────

// BEHAVIOUR: switching Mate off part-way through a pass makes no further calls.
func TestInboxClassifierStopsWhenMateIsSwitchedOffMidPass(t *testing.T) {
	w := newInboxWorld(t)
	var on *atomic.Bool
	doer := &fakeMateDoer{reply: func(call int, _ string) (string, error) {
		if call == 1 {
			on.Store(false)
			globalInboxMate.forget() // what a settings save does
		}
		return `{"kind":"scenery"}`, nil
	}}
	on, _ = withSwitchableMate(t, doer)
	for i := 0; i < 4; i++ {
		uploadSortable(t, uint8(120+i))
	}
	c := classifierFor(w)
	c.workers = 1
	if err := c.RunPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if doer.calls() != 1 {
		t.Errorf("%d calls: Mate was switched off after the first, so no more were wanted", doer.calls())
	}
}

// BEHAVIOUR: a model changed part-way through a pass is used for the rest.
func TestInboxClassifierUsesTheNewModelMidPass(t *testing.T) {
	w := newInboxWorld(t)
	var model *atomic.Value
	doer := &fakeMateDoer{reply: func(call int, _ string) (string, error) {
		if call == 1 {
			model.Store("other/model")
			globalInboxMate.forget()
		}
		return `{"kind":"scenery"}`, nil
	}}
	_, model = withSwitchableMate(t, doer)
	for i := 0; i < 3; i++ {
		uploadSortable(t, uint8(125+i))
	}
	c := classifierFor(w)
	c.workers = 1
	if err := c.RunPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if doer.calls() != 3 {
		t.Fatalf("%d calls, want 3", doer.calls())
	}
	if !strings.Contains(doer.body(0), testDocumentModel) {
		t.Errorf("first call: %s", doer.body(0))
	}
	for i := 1; i < 3; i++ {
		if !strings.Contains(doer.body(i), "other/model") {
			t.Errorf("call %d still used the old model: %s", i+1, doer.body(i))
		}
	}
}

// ── 2: only a change Mate depends on requeues failed sorts ───────────────

// failedSortWorld has one item whose sort failed, a counter of classifier wakes
// and a settings file to save to.
func failedSortWorld(t *testing.T) (w inboxWorld, id, settingsPath string, woken *atomic.Int32) {
	t.Helper()
	w = newInboxWorld(t)
	withInboxMate(t, statusDoer(401, new(atomic.Int32)))
	id = uploadSortable(t, 140)
	if err := classifierFor(w).RunPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if it, _ := w.inbox.Get(id); it.ClassifyError == "" {
		t.Fatal("setup: the item should have failed")
	}
	woken = new(atomic.Int32)
	prev := inboxClassifierWake
	inboxClassifierWake = func() { woken.Add(1) }
	t.Cleanup(func() { inboxClassifierWake = prev })
	return w, id, writeTestSettings(t, "203.0.113.1", 3000), woken
}

func waitForRequeue(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func postSecrets(t *testing.T, body string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/settings/secrets", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	if err := updateSecretsSettingsHandler(echo.New().NewContext(req, rec)); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("secrets save: %v %d %s", err, rec.Code, rec.Body)
	}
}

// BEHAVIOUR: a settings save that leaves Mate's inputs alone does not requeue.
func TestUnrelatedSettingsSaveDoesNotRequeueFailedSorts(t *testing.T) {
	w, id, settingsPath, woken := failedSortWorld(t)
	code, body := postSettings(t, settingsPath, func(p *settingsPayload) { p.Anchor.ChainOnboardM = 120 })
	if code != http.StatusOK {
		t.Fatalf("%d %v", code, body)
	}
	time.Sleep(100 * time.Millisecond) // a wrongly started requeue would have run by now
	if it, _ := w.inbox.Get(id); it.ClassifyError == "" || it.ClassifyAttempts != 1 {
		t.Errorf("the failure was cleared by an unrelated save: %+v", it)
	}
	if woken.Load() != 0 {
		t.Errorf("woken %d times", woken.Load())
	}
}

// BEHAVIOUR: changing the document model requeues and wakes.
func TestDocumentModelChangeRequeuesFailedSorts(t *testing.T) {
	w, id, settingsPath, woken := failedSortWorld(t)
	code, body := postSettings(t, settingsPath, func(p *settingsPayload) { p.Assistant.DocumentModel = "openai/gpt-4o-mini" })
	if code != http.StatusOK {
		t.Fatalf("%d %v", code, body)
	}
	waitForRequeue(t, "the failed sort to be requeued", func() bool {
		it, _ := w.inbox.Get(id)
		return it.ClassifyError == "" && woken.Load() == 1
	})
}

// BEHAVIOUR: switching the assistant on requeues and wakes.
func TestAssistantEnableChangeRequeuesFailedSorts(t *testing.T) {
	w, id, settingsPath, woken := failedSortWorld(t)
	code, body := postSettings(t, settingsPath, func(p *settingsPayload) { p.Assistant.Enabled = true })
	if code != http.StatusOK {
		t.Fatalf("%d %v", code, body)
	}
	waitForRequeue(t, "the failed sort to be requeued", func() bool {
		it, _ := w.inbox.Get(id)
		return it.ClassifyError == "" && woken.Load() == 1
	})
}

// BEHAVIOUR: a secrets save requeues only when the OpenRouter key changed.
func TestOnlyANewOpenRouterKeyRequeuesFailedSorts(t *testing.T) {
	w, id, _, woken := failedSortWorld(t)
	withTestSecretsStore(t)

	postSecrets(t, `{"SIGNALK_USERNAME":"admin"}`)
	time.Sleep(100 * time.Millisecond)
	if it, _ := w.inbox.Get(id); it.ClassifyError == "" || woken.Load() != 0 {
		t.Fatalf("an unrelated secret requeued: %+v, woken %d", it, woken.Load())
	}

	postSecrets(t, `{"OPENROUTER_API_KEY":"sk-new"}`)
	waitForRequeue(t, "the failed sort to be requeued", func() bool {
		it, _ := w.inbox.Get(id)
		return it.ClassifyError == "" && woken.Load() == 1
	})
}

// ── 3: a timed-out call is not asked again at once ───────────────────────

// BEHAVIOUR: a call that ran its full time goes to the back-off, with no second
// ask 2 seconds later.
func TestInboxClassifierDoesNotAskAgainAfterATimeout(t *testing.T) {
	w := newInboxWorld(t)
	doer := &fakeMateDoer{delay: time.Minute}
	withInboxMate(t, doer)
	prev := inboxMateCallTimeout
	inboxMateCallTimeout = 30 * time.Millisecond
	t.Cleanup(func() { inboxMateCallTimeout = prev })
	id := uploadSortable(t, 141)
	if err := classifierFor(w).RunPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if doer.calls() != 1 {
		t.Errorf("%d calls: a timed-out call was asked again at once", doer.calls())
	}
	if it, _ := w.inbox.Get(id); it.ClassifyError == "" || it.ClassifyAttempts != 1 || it.NextTryMs == 0 {
		t.Errorf("not on the back-off: %+v", it)
	}
}

// ── 4: a place-name hang is not blamed on Mate ───────────────────────────

// BEHAVIOUR: a Mate title whose feature fetch hangs is reported as the
// place-name server not answering, within the place-name timeout.
func TestInboxMateTitleBlamesThePlaceNamesForAHungFeatureFetch(t *testing.T) {
	_, at := titleWorld(t, 5)
	withInboxMate(t, &fakeMateDoer{reply: func(int, string) (string, error) { return `{"title":"T"}`, nil }})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	withFeatures(t, func(float64, float64) ([]poiFeatureRaw, error) {
		<-release
		return nil, nil
	})
	prevPlace, prevDeadline := inboxPlaceTitleTimeout, inboxTitleDeadline
	inboxPlaceTitleTimeout, inboxTitleDeadline = 50*time.Millisecond, 600*time.Millisecond
	t.Cleanup(func() { inboxPlaceTitleTimeout, inboxTitleDeadline = prevPlace, prevDeadline })
	id := uploadForFiling(t, 90, map[string]string{"capture_ms": ms(at)})
	start := time.Now()
	titles, errs := titlesFor(t, id)
	if time.Since(start) > 400*time.Millisecond {
		t.Errorf("waited %v: the fetch was not bounded by the place-name timeout", time.Since(start))
	}
	if titles[id] != nil && titles[id] != "" || errs[id] != "The place-name server didn't answer, so no title was suggested." {
		t.Errorf("title %v, error %v", titles[id], errs[id])
	}
}

// ── 5: the tile line is not stale ────────────────────────────────────────

// BEHAVIOUR: the line promises a try only when one is still ahead and Mate is on.
func TestInboxSortFailedLineOnlyPromisesATryThatIsStillAhead(t *testing.T) {
	useFreshBoatZone(t)
	w := newInboxWorld(t)
	withInboxMate(t, statusDoer(401, new(atomic.Int32)))
	id := uploadSortable(t, 142)
	if err := classifierFor(w).RunPending(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Due time already passed, Mate on.
	if err := w.inbox.SetClassifyRetry(id, inboxSortFailedMessage, 1, time.Now().Add(-time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if got := itemJSON(t, id)["classify_error"]; got != "Mate couldn't sort this yet. Trying again shortly" {
		t.Errorf("past due: %q", got)
	}

	// Still ahead, Mate on.
	if err := w.inbox.SetClassifyRetry(id, inboxSortFailedMessage, 1, time.Now().Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if got, _ := itemJSON(t, id)["classify_error"].(string); !strings.HasPrefix(got, "Mate couldn't sort this yet. Trying again at ") {
		t.Errorf("ahead: %q", got)
	}

	// Mate off: no try is coming.
	withMateOff(t)
	if got := itemJSON(t, id)["classify_error"]; got != "Mate couldn't sort this. Mate is off" {
		t.Errorf("Mate off: %q", got)
	}
}
